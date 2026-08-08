# ADR-0004 — Structured, session-scoped logging

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 2, 3, 5 and 8, [MVP](../mvp.md)

## Context

Observability is not a nice-to-have here; it is the feature that makes Awake
trustworthy. A tool that manipulates system power behaviour while the user
is away has to be able to prove what it did. Principle 2 states that if a
behaviour cannot be explained from the logs, the logging is incomplete.

Two audiences need the logs: a human asking "what happened during that
session?" and a machine — a script, a future UI, a support request — needing
to parse the same answer.

## Decision

**Logs are JSON Lines**: one JSON object per line, append-only. Readable
enough to skim, structured enough to parse without inventing a format.

**Two destinations.** A global application log (`logs/awake.jsonl`) and a
per-session trace (`logs/sessions/<session-id>.jsonl`). Session-scoped events
are written to both: the global log stays a complete timeline, and each
session remains independently readable and shareable without leaking
unrelated activity.

**Event names are a stable, closed vocabulary** (`session.started`,
`mode.stopped`, and so on), namespaced by subsystem. New events may be added
in minor releases; renaming or removing one is breaking.

**Every event carries a schema version.** Consumers can then detect a format
they don't understand instead of misreading it. This is deliberate
verbosity bought in exchange for being able to evolve the schema at all.

**Every event carries** a UTC timestamp, a level, an event name, and — for
anything session-scoped — the session ID as the correlation key.

**The log schema is public API** under principle 8, from the first release.
Adding a field is additive; changing a field's meaning or type is a breaking
change requiring a major version.

**Logs record what Awake did, never what the user did.** No keystrokes, no
clipboard, no screenshots, no window titles, no application names, no file
paths beyond Awake's own. When input detection lands (v0.2), it may record
only *that* input occurred — never what it was. This constraint outranks any
debugging convenience, and there is no verbosity level that relaxes it.

**No `session.tick` in v0.1.0.** A periodic heartbeat writes unbounded
volume for low information, and there is no rotation yet. If a real
situation proves inexplicable without it, that is the evidence to add it.

## Consequences

- Any session can be handed to a maintainer as a single file, and it will
  contain nothing personal by construction — which is what makes bug reports
  safe to share.
- Log volume grows without bound in v0.1.0. Accepted at MVP scale; rotation
  is a named v0.2 obligation, not an open-ended deferral.
- Freezing the schema early constrains us. That is the intent — but it means
  the initial event set deserves scrutiny before ratification, because it is
  cheap to change now and expensive later.
- Writing each session event twice costs a second write. Negligible at this
  volume, and cheaper than reconstructing a session's trace by filtering the
  global log after the fact.
- Log writes must never take the app down: a failing log write degrades, it
  does not abort a running session.

## Alternatives considered

**Plain-text human logs.** Friendlier to read with no tooling. Rejected: it
forces every consumer to write a parser, and the stability guarantee of
principle 8 would be unenforceable.

**One global log only, filtered by session ID on demand.** Fewer files.
Rejected: per-session traces are a stated product feature, and filtering
requires tooling the user may not have — plus a shared log cannot be handed
over without exposing unrelated sessions.

**A structured-logging dependency.** Go's standard library now includes a
structured logger (`log/slog`), which makes an external dependency hard to
justify under principle 7. Revisit only if a concrete need appears.

**System logging facilities** (syslog, macOS unified logging). Native
integration, free rotation. Rejected: state moves outside `~/.awake`,
becomes platform-coupled, and is far less inspectable — the user can no
longer simply read the file.

**Rotation in v0.1.0.** Deferred rather than rejected. Rotation policy
(by age, by count, by size) is a real user-facing decision and belongs in
its own ADR once there is usage to reason about.
