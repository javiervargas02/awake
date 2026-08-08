# Architecture — Session lifecycle

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0002](../adr/0002-session-as-core-domain-object.md).
> Depends on [Overview](overview.md).

This document specifies exactly how a session begins, how it ends, how each
ending is classified, and what happens when it ends in a way nobody chose.

## The state machine

```text
                         (no session)
                              │
                              │ start
                              ▼
                          running
                              │
      ┌───────────────┬───────┴────────┬──────────────────┐
      │ deadline      │ stop requested │ mechanism failed │ process vanished
      ▼               ▼                ▼                  ▼
  completed        stopped           failed             failed
  duration_        user_stopped      mode_failure       crashed
  elapsed          / interrupted                        (discovered later)
```

Four persisted statuses, as fixed by ADR-0002: `running`, `completed`,
`stopped`, `failed`. Three of them are terminal. A session never leaves a
terminal state — a new session is a new record with a new ID.

### Why there is no `starting` state

There is a real window between persisting the session record and the platform
mechanism actually running. It is tempting to model it as a state.

We don't, because the session **trace** already answers it with more
precision than a status field could. `session.created`, `session.started`,
and `mode.started` are distinct events; where the trace stops tells you
exactly how far the session got. Adding a status would duplicate that
information in a coarser form, and every duplicated fact is a fact that can
disagree with itself.

This is the general pattern: **the state file carries coarse, current truth;
the log carries fine-grained, historical truth.** When the two could overlap,
the log wins and the state file stays small.

## Starting a session

The order of operations is deliberate:

1. **Load config**, falling back to defaults per ADR-0007.
2. **Acquire the session lock** (below). Refuse with exit code 3 if another
   session already holds it.
3. **Construct the session**: new ID, mode, requested duration, absolute UTC
   deadline (absent if indefinite), start timestamp, owning PID. Emit
   `session.created`.
4. **Persist the record** — before touching the platform.
5. **Start the mode**, which asks the platform to begin keeping the machine
   awake. Emit `mode.started`. On failure: terminal state `failed` /
   `mode_failure`, and exit non-zero.
6. **Emit `session.started`** and begin waiting.

Step 4 precedes step 5 on purpose. If Awake dies between them, a record
exists and recovery can attribute and clean up whatever the platform layer
may have started. Reverse the order and a leaked child process would be
unattributable — precisely the failure ADR-0006 treats as the worst outcome
in the project.

### Exclusivity

Exclusivity is held as a resource, not derived from a file: the session takes
an **OS advisory lock** for its whole lifetime, per
[ADR-0008](../adr/0008-session-exclusivity.md). Acquisition is atomic, so
there is no window in which two starts can both conclude the coast is clear,
and the kernel releases the lock when the holder dies — so a stale lock cannot
exist.

The lock is taken **before** the session record is written, and it decides
liveness:

| Lock | Record | Meaning | Result |
| --- | --- | --- | --- |
| acquired | absent or terminal | nothing was running | proceed |
| acquired | non-terminal | previous run died without writing an ending | recover, then proceed |
| held by another | — | a session is genuinely active | **conflict**, exit 3, emit `session.start_refused` |

Because the lock lives outside `~/.awake`, deleting that directory mid-session
cannot produce a second session.

The record's `owner_pid` and `owner_started_at` remain, but only as
diagnostics — `awake stop` uses the PID to send its signal, and the trace uses
both to explain what happened. Neither decides whether a session is live.

If the lock cannot be created at all (an unwritable runtime directory), Awake
logs a warning, falls back to record-based checking, and `doctor` reports it
as a problem. A working tool with a stated weakness beats a tool that refuses
to start because of a temp directory.

## Timing

The deadline is an absolute UTC instant, fixed at creation and never
recomputed (ADR-0002). Waiting is a timer, but the timer is not the
authority — **the deadline is**, and the clock is consulted on every wake.

This matters because system sleep suspends timers. A machine that sleeps
through its deadline wakes with a timer that fires late; Awake compares the
current instant against the stored deadline, ends the session as
`duration_elapsed`, and logs that the ending was discovered late rather than
observed on time. The session is not extended to compensate. The user asked
for a moment, not for an allowance of awake-time.

Indefinite sessions have no deadline and wait only for cancellation.

## Ending a session

### The four paths

| Path | Status | End reason |
| --- | --- | --- |
| Deadline reached | `completed` | `duration_elapsed` |
| `awake stop`, or Ctrl-C | `stopped` | `user_stopped` / `interrupted` |
| Platform mechanism failed | `failed` | `mode_failure` |
| Process killed outright | `failed` | `crashed` (discovered later) |

Every path except the last runs the same shutdown sequence: stop the mode,
emit `mode.stopped`, write the terminal status and end reason, emit the
closing session event, close the trace. Shutdown is one code path with
different reasons — not four different endings — so an ending can never be
half-performed depending on how it was triggered.

### How `awake stop` reaches a running session

Sessions run in the foreground (MVP decision), so `awake stop` is a
*different process* from the one holding the session. It needs no IPC
channel:

1. Read the session record; verify a session is running and its PID is alive.
   If not, exit 3 — there is nothing to stop.
2. Send `SIGTERM` to the recorded PID.
3. Wait a bounded grace period for the record to reach a terminal state.
4. Report the outcome.

If the grace period expires, `awake stop` reports plainly that the session did
not end and exits non-zero. **It does not escalate to `SIGKILL`.** Escalation
is a destructive act the user should choose, and per ADR-0006 the platform
mechanism dies with its parent regardless, so a hung Awake cannot leave the
machine permanently awake. A `--force` flag is a v0.2 question.

The stopping process never writes to the session record. **The owner is the
sole writer** — which is what makes atomic writes sufficient and a lock
unnecessary.

### Distinguishing `user_stopped` from `interrupted`

The running process sees a signal, not an intention. The mapping is:

- `SIGTERM` → `user_stopped` (what `awake stop` sends)
- `SIGINT` → `interrupted` (what Ctrl-C sends)

This is imprecise at the edges: a `SIGTERM` from a system shutdown will be
recorded as `user_stopped`. We accept that, because the raw signal is
recorded in the trace regardless — the end reason is an interpretation, the
log is the fact. The alternative, a stop-request marker file written before
signalling, buys real precision at the cost of a new file, a new stale-state
failure mode, and a second writer. Not worth it for this distinction.

Mechanically, signal handling stays at the process boundary as the overview
requires: `cmd/awake` catches the signal and cancels the session's context
**with a domain-level cause** — Go's context package supports attaching a
cause to a cancellation, which the core then reads. The core learns "stopped
by the user" or "interrupted"; it never learns that signals exist.

## Recovery

A session whose owning process vanished can never write its own ending. It is
resolved by the *next* Awake command that reads state:

1. Detect a non-terminal record whose PID is not alive.
2. Attempt platform reclamation (ADR-0006, layer 3).
3. Write terminal status `failed`, end reason `crashed`, with the end
   timestamp marked as the moment of *discovery*, not of death — Awake does
   not know when the process died and must not invent it.
4. Emit `session.recovered` to the global log and to that session's trace,
   which is reopened for the purpose.
5. Proceed with whatever the user actually asked for.

Recovery is never a command the user has to run first. `awake start` after a
crash just works, and says what it cleaned up.

## What `status` reports

- A running session: its ID, mode, start time, deadline, and remaining time.
- No running session: the most recent terminal session and how it ended.
- No history at all: that, plainly — not an error.

`status` performs recovery detection like any other command, so it can never
report a crashed session as running.

## Open questions

1. **Grace period for `awake stop`** — a fixed value, or configurable? Leaning
   fixed; a knob here invites tuning a thing that should just work.
2. **Indefinite sessions and `status` output** — "remaining time" has no
   meaning; confirm the wording when the CLI contract is written (document 7).
