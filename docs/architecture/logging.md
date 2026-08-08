# Architecture — Logging

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0004](../adr/0004-structured-session-scoped-logging.md).
> Depends on [Overview](overview.md) and
> [Session lifecycle](session-lifecycle.md).
>
> **This document defines public API.** Under principle 8, the event
> vocabulary and envelope below become a semver-governed contract at v0.1.0.

## The bar

Principle 2 sets a testable standard: *if a behaviour cannot be explained
from the logs, the logging is incomplete.* The practical version — the one to
review against — is this:

> Given only a session's trace file, a reader must be able to reconstruct
> what Awake did, in order, and why the session ended.

Not "roughly what happened." The actual sequence.

## Sinks

Two destinations, both append-only JSON Lines:

```text
~/.awake/logs/awake.jsonl                    global — every event
~/.awake/logs/sessions/<session-id>.jsonl    one session's trace
```

**Session-scoped events are written to both.** The global log stays a
complete timeline; the session trace stays independently readable and
shareable. Non-session events (`app.started`, update checks, health checks)
go to the global log only.

The trace file is created when the session record is created, appended to for
the session's life, and closed on its terminal event. Recovery
(ADR-0002) reopens it to append `session.recovered` — the only case where a
closed trace is written to again.

**Logs are never written to stdout or stderr in normal operation.** Terminal
output is the CLI's job and is a separate contract (document 7). The single
exception is one degradation notice on stderr if logging itself fails.

## The event envelope

Every line is one JSON object with a reserved envelope and an event-specific
payload:

| Field | Type | Presence | Meaning |
| --- | --- | --- | --- |
| `ts` | string | always | UTC, RFC 3339 with **microseconds** |
| `schema_version` | integer | always | envelope/vocabulary version; starts at `1` |
| `level` | string | always | `info` \| `warn` \| `error` |
| `event` | string | always | dotted name from the catalogue below |
| `session_id` | string | session-scoped events only | correlation key |
| `data` | object | optional | event-specific fields |

Illustrative line:

```text
{"ts":"2026-08-07T14:03:11.482913Z","schema_version":1,"level":"info",
 "event":"session.started","session_id":"...","data":{}}
```

Microsecond precision costs nothing and makes ordering unambiguous in the
global log, which has concurrent writers from independent processes.

Three deliberate choices here:

**Event-specific fields live under `data`, never at the top level.** The
envelope namespace stays reserved, so adding an envelope field later can never
collide with an event's payload. Flat is prettier; nested is safe to evolve.

**There is no free-text `message` field.** Every event name is
self-describing and every specific lives in a typed field. A message field
would be a second copy of facts already in the payload, and — worse — it
invites putting information *only* in prose, where no consumer can reach it.
Human phrasing is a rendering concern, derived from the data by whoever
displays it. (Error *text* is different: it appears as a typed `error` field
where an operation genuinely failed.)

**Three levels only.** `info` for normal operation, `warn` for handled
degradation, `error` for something the user asked for that failed. No `debug`
in v0.1.0 — adding a level later is additive; removing one is breaking.

## Event catalogue (v0.1.0)

`data` fields are listed per event. All are required unless marked optional.

### Application

| Event | Level | `data` |
| --- | --- | --- |
| `app.started` | info | `app_version`, `command` |

`command` is the resolved subcommand name only. **Raw command-line arguments
are never logged** — see Privacy.

### Configuration

| Event | Level | `data` |
| --- | --- | --- |
| `config.loaded` | info | `source` (`file` \| `defaults`) |
| `config.defaulted` | warn | `key`, `reason` (`missing_file` \| `invalid_value` \| `unreadable`) |
| `config.unknown_key` | warn | `key` |

### Session

| Event | Level | `data` |
| --- | --- | --- |
| `session.start_refused` | warn | `reason` (`already_running` \| `lock_unavailable`) |
| `session.created` | info | `app_version`, `mode`, `requested_duration`, `indefinite`, `deadline` (null if indefinite), `owner_pid` |
| `session.started` | info | — |
| `session.completed` | info | `end_reason` (`duration_elapsed`), `elapsed`, `overrun` |
| `session.stopped` | info | `end_reason` (`user_stopped` \| `interrupted`), `elapsed`, `remaining` |
| `session.failed` | error | `end_reason` (`mode_failure` \| `crashed`), `elapsed`, `error` (optional) |
| `session.recovered` | warn | `owner_pid`, `discovered_at`, `platform_reclaimed` |

`overrun` is how far past the deadline the ending was *noticed* — non-zero
when the machine slept through it (see session lifecycle). It is the field
that makes a late ending explainable instead of suspicious.

`session.created` carries `app_version` so a trace file shared on its own
identifies the binary that produced it.

### Mode and platform

| Event | Level | `data` |
| --- | --- | --- |
| `mode.started` | info / warn | `mode`, `mechanism`, `mechanism_pid`, `assertion_verified` (`verified` \| `unverifiable`) |
| `mode.stopped` | info | `mode` |
| `mode.failed` | error | `mode`, `error` |

`mechanism` names the platform facility in use, so a reader can see exactly
what Awake asked the OS for rather than inferring it.

### Updates

| Event | Level | `data` |
| --- | --- | --- |
| `update.check.started` | info | `channel` |
| `update.check.completed` | info / warn | `result` (`up_to_date` \| `update_available` \| `failed`), `latest_version` (optional), `error` (optional) |
| `update.available` | info | `current_version`, `latest_version`, `severity` |

A failed check logs at `warn` and is never an error — being offline is not a
fault (ADR-0005).

### Health

| Event | Level | `data` |
| --- | --- | --- |
| `health.check.completed` | info / warn | `total`, `ok`, `warnings`, `problems`, `findings[]` |
| `repair.performed` | warn | `action`, `target`, `result` |

One `repair.performed` per action taken. Repairs log at `warn` because a
repair means something was wrong — silent self-repair is indistinguishable
from a bug (ADR-0003).

`session.start_refused` has no `session_id` — no session was created. It is
logged because a refusal is a meaningful action: a user who wonders why
nothing happened deserves a record of it.

### Additions to the MVP list

`config.unknown_key`, `mode.failed`, and `session.start_refused` are not in
the MVP document's event set. The first satisfies ADR-0007's "unknown keys
warn rather than fail"; the second distinguishes *the mechanism died* from
*the session ended*, which the trace otherwise could not show; the third
records a refused start, which principle 2 requires. All three are additive.

## Privacy

Principle 5 is absolute here and outranks any debugging convenience. There is
no verbosity level that relaxes it.

**Never logged, under any circumstances:** keystrokes, key codes, or input
content; clipboard contents; screenshots or screen contents; window titles;
names of other running applications; user file paths; environment variables;
raw command-line arguments; hostnames, usernames, or network identifiers.

**Paths are logged only when they are Awake's own** — files under
`~/.awake` — and even then, home-relative rather than absolute where
practical.

**Arguments are logged only after validation, as typed values.** A parsed
duration is logged; the raw string the user typed is not. This closes the
route by which arbitrary user text reaches a log file.

**When end-on-input lands (v0.2)**, it may record only *that* input occurred
and when. Never the device, never the key, never the coordinates. The
reserved `input_detected` end reason carries no payload describing the input.

The design consequence is a useful one: **a session trace contains nothing
personal by construction**, which is what makes it safe to attach to a bug
report without review.

## Behaviour when logging fails

Logging must never be the reason a session ends. The ladder:

1. **Session trace write fails** → record the failure in the global log,
   continue the session.
2. **Global log write fails** → continue, and emit a single notice on stderr.
   Not one per event; once.
3. **Neither sink is writable** → the session still runs. `doctor` reports the
   log directory as a problem, and `repair` recreates it.

A session that keeps its promise with degraded logging is better than one
that abandons the user's machine to sleep because a file was unwritable. This
is the one place where observability yields to the primary function, and it
yields explicitly rather than by accident.

## Durability and concurrent writers

**One event is one write syscall, appended, with no user-space buffering.**
Two consequences, both wanted:

- **Crash survivability.** Everything logged before a `SIGKILL` is on disk.
  A buffered logger would lose exactly the events preceding a crash — the ones
  that explain it.
- **Safe interleaving.** The global log has multiple writers: `awake status`
  or `awake stop` may run while a session holds a trace open. Append-mode
  writes of whole lines cannot interleave into a corrupt line.

Because writers are independent processes, **the global log is not strictly
ordered by timestamp** under concurrency. Consumers should sort by `ts` rather
than assuming file order. Per-session traces have a single writer and are
ordered.

No `fsync` per event. Losing the final line to a hard power cut is acceptable;
paying a disk sync per event is not, and a session that crashed mid-write is
resolved by recovery regardless.

## Schema evolution

The contract for consumers, and the rules for us:

**Consumers must ignore unknown events and unknown `data` fields.** A
consumer that breaks on an unrecognised event is not conforming, and we are
not obliged to preserve its behaviour.

| Change | Classification |
| --- | --- |
| New event name | additive — minor |
| New field in `data` | additive — minor |
| New level | additive — minor |
| Renaming or removing an event | breaking — major |
| Renaming or removing a `data` field | breaking — major |
| Changing a field's type or meaning | breaking — major |
| Changing the envelope | breaking — major |

`schema_version` increments only on a breaking change, and every increment is
called out in the changelog under Changed with a migration note.

## Deferred: rotation

There is no rotation in v0.1.0 (ADR-0004). At roughly a dozen events per
session, growth is negligible at MVP scale, and the global log is the only
file that grows without bound in normal use.

The open question is policy, not mechanism: cap session traces by age, by
count, or leave them to the user? Deleting a user's history automatically is
itself a trust decision, which is why this is a v0.2 ADR rather than an
implementation detail to settle quietly.

## `--verbose`

A global `--verbose` flag mirrors log events to **stderr** as they are
written, for users diagnosing a problem live and for development.

Three rules keep it from becoming a second, weaker log:

- **It changes where events are echoed, never what is logged.** The files are
  identical with and without it, so a trace never depends on how the command
  was invoked.
- **stderr, not stdout.** Stdout carries the command's result and must stay
  clean enough to pipe — especially under `--json`.
- **It grants no privilege.** The privacy rules above are absolute; there is
  no verbosity setting that reveals more about the user, because nothing more
  is ever collected.

Echoed output is rendered for a human reading a terminal, not emitted as raw
JSON lines. The files are the machine-readable artifact; this is a window
onto them.

## Open questions

1. **Rotation and retention policy** — required before v0.2 closes.
2. **Health finding detail in `findings[]`** — full per-check results, or
   counts with detail left to CLI output? Settle when the CLI contract is
   written (document 7).
