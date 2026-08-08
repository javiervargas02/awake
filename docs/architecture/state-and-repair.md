# Architecture — State and repair

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0003](../adr/0003-local-first-recoverable-state.md) and
> [ADR-0007](../adr/0007-configuration-format.md).
> Depends on [Overview](overview.md) and
> [Session lifecycle](session-lifecycle.md).

This document turns ADR-0003's file table into a specification: who writes
what, how, what happens when each thing is missing or broken, and what
`doctor` and `repair` actually do.

## The governing split

One idea organises everything below:

> **State is current truth. Logs are history.**

`session.json` holds the current or most recent session — one record,
overwritten. It is not an archive; the archive is `logs/sessions/`. This is
why state files stay small and why losing them costs preference and
convenience, never history.

## Layout, ownership, and classes

```text
~/.awake/                        0700
├── config.toml                  0600   recoverable   written by: user, or Awake at creation
├── session.json                 0600   recoverable   written by: the session owner only
├── update.json                  0600   cache         written by: whichever process checks
└── logs/                        0700   derived
    ├── awake.jsonl              0600                 written by: every process
    └── sessions/                0700
        └── <session-id>.jsonl   0600                 written by: the session owner only
```

One file lives **outside** this tree, deliberately:

```text
$TMPDIR/awake/session.lock       0600   runtime       held by: the session owner
```

This is the exclusivity lock (ADR-0008). It holds no content and no history,
is recreated per session, and is cleared by the OS on reboot. It sits outside
`~/.awake` for exactly one reason: so that deleting `~/.awake` — which this
project actively tells users is safe — cannot permit a second concurrent
session. Because it is the only thing Awake writes elsewhere, it must be named
in the README rather than discovered.

**Permissions are deliberate.** `0700` / `0600` throughout. Even with no
content logged about the user (principle 5), the *timing* of sessions reveals
when someone was away from their machine. That is private, and file modes are
the cheapest place to respect it.

**Single-writer wherever possible.** `session.json` and a session's trace have
exactly one writer — the process that owns the session. `awake stop` signals;
it does not write (see session lifecycle). Only the global log and
`update.json` have multiple potential writers.

## Writing state atomically

Every state write follows the same protocol:

1. Write the full new content to a temporary file **in the same directory**
   (same filesystem, so the rename is atomic).
2. `fsync` the temporary file.
3. `rename` it over the target.

There is no partial-write recovery path because partial writes cannot occur:
a reader sees either the old file or the new one. This is the one place worth
paying for `fsync` — state files are small, written rarely, and a torn
session record is the single hardest thing to reason about after a crash.
Logs make the opposite trade for the opposite reasons (see logging).

**Reads never repair silently.** A read that fails is reported to the caller,
which decides: fall back to defaults, treat as absent, or surface a problem.
Repair is an explicit action, always logged.

## Handling a corrupt file

A file that exists but cannot be parsed is **quarantined, not deleted**
(ADR-0003) — recovery must not destroy evidence:

1. Rename it to `<name>.corrupt-<UTC timestamp>` in place.
2. Log `repair.performed` naming the action and the target.
3. Regenerate or treat as absent, per the file's class.

Quarantined files are never cleaned up automatically. If they accumulate,
`doctor` says so and lets the user decide — deleting a user's file is their
call, not ours.

## Record contents

### `session.json`

`version`, `id`, `app_version`, `mode`, `requested_duration`, `indefinite`,
`deadline`, `started_at`, `ended_at`, `status`, `end_reason`, `owner_pid`,
`owner_started_at`, `mechanism_pid`.

`mechanism_pid` identifies the platform process, so startup reclamation
(ADR-0006, layer 3) knows what to look for. It is never acted on without
verifying the process is the expected executable — see
[Platform abstraction](platform.md).

**`owner_pid` and `owner_started_at` are diagnostics, not the liveness
mechanism.** ADR-0008 makes the advisory lock authoritative for "is a session
running?", which is a fact rather than an inference. The PID is still needed —
`awake stop` sends its signal there — and the start time still distinguishes a
reused PID from the original process when explaining a trace. But neither
decides exclusivity, so a platform that cannot supply `owner_started_at`
loses diagnostic precision and nothing more.

### `update.json`

`version`, `channel`, `checked_at`, `result`, `latest_version`, `severity`.

Purely a cache. Any doubt about its contents is resolved by discarding it.

### State files are not public API

The log schema is a contract (principle 8). **These files are not.** They are
an implementation detail, and their `version` field exists for our own
forward-compatibility, not as a consumer promise.

The supported way to read Awake's state from a script is `awake status
--json`. Committing to the on-disk format as well would freeze two
representations of the same thing, and the CLI is the one we can evolve
deliberately. This should be stated in the README so nobody discovers it the
hard way.

## Bootstrap and first run

The directory tree is created **lazily, by the first command that needs to
write.** Read-only commands — `version`, and `status` on a machine that has
never run a session — do not create anything. Installing Awake and asking it
its version should leave no trace.

Creation is itself a logged event, and a first run is indistinguishable from
a run after `rm -rf ~/.awake`. That is the point: there is no "installed"
state to be in.

## `doctor` — the check catalogue

`doctor` **diagnoses and never mutates.** It is also the dry run for
`repair`: every problem it reports names the action that would fix it.

| # | Check | Warning | Problem |
| --- | --- | --- | --- |
| 1 | Root directory exists and is a directory | absent (normal before first use) | exists but is not a directory, or not creatable |
| 2 | Root directory permissions | broader than `0700` | not readable/writable by the user |
| 3 | Config file | absent (defaults in use); unknown keys; invalid values | present but unparseable |
| 4 | Session record | absent | unparseable; or non-terminal while the lock is free (stale) |
| 5 | Orphaned platform process | — | a mechanism from a previous run is still running |
| 5b | Session lock | runtime directory unavailable (degraded exclusivity) | lock held while the record is terminal or absent (anomaly) |
| 6 | Log directories exist and are writable | absent | not writable |
| 7 | Write probe (temp file create/rename/delete) | — | fails |
| 8 | Update cache | absent or stale | unparseable |
| 9 | Platform capability | mechanism present but degraded | mechanism unavailable |
| 9b | Assertion verification facility | query available but unparseable | query facility unavailable |
| 10 | Quarantined files | any present | — |

Every finding carries a status, the check name, a plain-language explanation,
and the remedy. `--json` renders the same findings structurally.

**Exit codes.** `0` when everything is `ok` or `warning`; **`5` when any
problem is found.** Warnings are informational — a machine with no config
file is healthy, not broken — while problems mean something needs attention,
and a script should be able to tell without parsing output.

> This adds exit code `5` to the MVP contract, which reserved `0`–`4`. It is
> an addition to a document that is not yet ratified, and it is flagged as
> such below.

## `repair` — the action catalogue

`repair` performs only safe, non-destructive fixes. Each maps to a `doctor`
finding; each emits `repair.performed`.

| Finding | Action |
| --- | --- |
| Root or log directories missing | create with correct permissions |
| Permissions too broad | tighten (never loosen) |
| Config missing | generate defaults, with explanatory comments (ADR-0007) |
| Config unparseable | quarantine, then generate defaults |
| Session record unparseable | quarantine, treat as no session |
| Session record stale | write terminal `failed` / `crashed`, emit `session.recovered` |
| Orphaned platform process | terminate it, emit `session.recovered` |
| Update cache unparseable or stale | discard; it re-checks on the next interval |
| Quarantined files present | **only with `--clean-quarantine`**: delete them (see below) |

### Quarantine cleanup

Quarantined files accumulate forever otherwise, so `repair` offers a way out —
but not by default, because deleting a user's files is their decision.

`awake repair --clean-quarantine` deletes quarantined files, and only those:
files matching the quarantine naming pattern, nothing else. Plain `repair`
never removes them; it reports how many exist and names the flag. Each
deletion emits `repair.performed`.

Two guards make this safe to offer: the flag is explicit, and the action is
narrow enough to describe in one sentence. If a user cannot predict exactly
what a destructive command will delete, the command is wrong.

**What `repair` will never do:**

- delete or truncate logs,
- delete quarantined files *unless* `--clean-quarantine` is passed,
- modify a config file that parses — even one it disagrees with,
- touch a genuinely running session,
- loosen permissions,
- act on anything `doctor` would not have reported.

That last rule is what keeps the pair honest: **`repair` has no powers
`doctor` cannot predict.** A user can always see the whole blast radius
before authorising it.

`repair` is idempotent, and on a healthy installation it does nothing and
says so.

## Concurrency and races

Multiple Awake processes can run at once — `status` during a session is
normal, and two `start` attempts are possible.

- **Reads during a write** are safe: rename atomicity means a reader sees one
  whole version or the other.
- **Two `start` attempts racing** are resolved by the advisory lock
  (ADR-0008), which is acquired atomically. Exactly one wins; the other exits
  3. There is no check-then-act window to lose.
- **`update.json`** has multiple writers and no coordination. Last write wins;
  it is a cache, so this is harmless by construction.

### Deleting `~/.awake` mid-session

The running session continues — it holds its deadline, its lock, and its
platform mechanism, none of which depend on that directory. Log writes fail
and degrade per the logging ladder. When the session ends, it recreates what
it needs to write its terminal record.

**Exclusivity survives**, because the lock lives outside `~/.awake`. A
concurrent `awake start` still finds the lock held and refuses. This is the
specific reason ADR-0008 places the lock where it does: the deletion the
project invites must not be able to produce two sessions.

What is genuinely lost is continuity of the session's *trace* — the events
written while the directory was missing are gone, and the file resumes when
writes succeed again. That is a history gap, not a correctness problem, and
the recreated trace records that the gap happened.

## Config validation depth

`doctor` validates more than syntax. A value can be perfectly well-formed and
still be wrong for its purpose — an update `check_interval` of one second
parses fine, but it would mean contacting the network on essentially every
command, which no user wants and which ADR-0005's caching exists to prevent.

So each key gets a **plausible range** as well as a type, and a value outside
it is a `warning`: the tool honours it (it is the user's machine) while saying
plainly that it looks unintended. Rejecting it outright would be worse — that
turns a preference into an error — and staying silent would let a typo
quietly change Awake's behaviour.

The ranges themselves belong with each key's definition in the config
specification, not here.

## Open questions

1. **`repair --dry-run`.** Arguably redundant, since `doctor` is the dry run.
   Worth confirming that framing holds once both are implemented.
