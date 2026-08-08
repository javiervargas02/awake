# ADR-0002 — The session is the core domain object

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 2 and 6, [MVP](../mvp.md)

## Context

Awake needs a unit that is bounded, observable, and explainable after the
fact. A tool that simply "turns keep-awake on and off" has no such unit:
there is nothing to name, nothing to log against, and nothing to report on
when a user asks what happened yesterday.

We also need to answer, precisely, what it means for a session to end when
the machine may sleep, the clock may shift, and the process may be killed.

## Decision

**The session is the central domain entity.** Everything else — modes, the
platform layer, logging, state — is organised around it.

A session record carries: a unique sortable identifier, the mode, the
requested duration, an absolute deadline (absent for indefinite sessions),
start and end timestamps, a status, an end reason, and the process ID that
owns it.

**Status and end reason are separate fields.** Status answers "what state is
this in"; end reason answers "why did it end."

```text
status:      running | completed | stopped | failed
end reason:  duration_elapsed | user_stopped | interrupted
             | mode_failure | crashed | input_detected (reserved)
```

**Exactly one session may be active at a time.** Keep-awake is a global
property of the machine; concurrent sessions would have no coherent meaning
when one ends and another continues.

**The deadline is an absolute wall-clock instant in UTC, and it is
authoritative.** When a user says `awake start 30m`, the promise is "this
ends at a specific moment," not "this ends after 30 minutes of accumulated
awake time." Internal timing may use a monotonic clock, but the deadline
decides. On resume from sleep, Awake compares the current instant against
the stored deadline; if it has passed, the session ends as
`duration_elapsed` and the logs record that it was discovered late.

**Indefinite sessions have no deadline** and require an explicit request.

**A session whose owning process vanished is `failed` / `crashed`,**
discovered by the next command via a PID liveness check and recorded with a
`session.recovered` event rather than silently overwritten.

## Consequences

- The session ID becomes the correlation key for the entire system: the
  state record, the log filename, and every session-scoped log event.
- Adding detached sessions later is mostly a question of who owns the
  process, because ownership is already modelled.
- Adding end-on-input later is additive: a new end reason, already reserved.
- Wall-clock authority means a machine that sleeps through its session wakes
  to find it already over. This is the honest behaviour — the keep-awake
  promise was already broken by the sleep — but it must be documented, and
  the logs must make the sequence legible.
- Wall-clock authority also means a large clock correction (an NTP step, not
  a timezone change — deadlines are UTC instants) can move a deadline. This
  is rare and bounded; monotonic authority trades it for a worse failure.
- Single-session exclusivity needs enforcement at the state layer, not the
  CLI (see ADR-0003).

## Alternatives considered

**Status only, no end reason.** Fewer fields. Rejected: it cannot
distinguish a user stopping a session from a signal interrupting it, and it
cannot represent a crash discovered after the fact. That is precisely the
information a per-session trace exists to preserve.

**Monotonic time as authoritative.** Immune to clock adjustment. Rejected:
a machine that suspends for two hours during a 30-minute session would keep
that session alive long past the moment the user expected it to end,
producing a tool that quietly outlives its promise — the exact behaviour
principle 6 exists to prevent.

**Recomputed countdown on each tick.** Rejected: accumulates drift and makes
the end time unknowable in advance, so `status` cannot honestly answer "when
does this end?"

**Multiple concurrent sessions.** Rejected: keep-awake is not a resource
that can be held twice, and the semantics of overlapping deadlines would be
impossible to explain — failing the "predict what the binary will do" test.
