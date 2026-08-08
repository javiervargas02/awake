# ADR-0001 — Core-first architecture, CLI as a thin client

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principle 10, [vision](../vision.md), [MVP](../mvp.md)

## Context

Awake ships as a CLI, but the roadmap anticipates other frontends: a tray
app, a desktop UI, possibly a local HTTP API. The common failure mode for
tools that grow this way is that business logic accumulates in the command
layer — argument parsing, validation, file writes, and process control all
tangled into one another. When the second frontend arrives, it either
duplicates that logic or the project stalls.

We are also in a position to decide this cheaply: there is no code yet.

## Decision

Business logic lives in core packages under `internal/`. The CLI is a thin
client over an application service that exposes intent-level operations —
start a session, stop the running session, report status, diagnose, repair,
check for updates.

The CLI layer is responsible for exactly three things:

1. parsing and validating user input into a request the core understands,
2. calling one core operation,
3. rendering the result as human or machine-readable output.

The CLI does not touch the filesystem, does not talk to the platform layer,
does not decide session state, and does not format log events. Any future
frontend is a peer of the CLI, not a wrapper around it.

Rendering is explicitly a frontend concern: the core returns structured
results, never pre-formatted strings intended for a terminal.

## Consequences

- A second frontend costs a new presentation layer and nothing else.
- Core operations are testable without spawning a process or parsing text.
- Error handling gets a boundary: the core returns typed, meaningful errors;
  the CLI maps them to exit codes and human phrasing. Exit-code mapping thus
  lives in exactly one place, which matters because exit codes are a public
  contract (principle 8).
- There is more indirection than a single-frontend tool strictly needs. We
  accept that cost knowingly, in exchange for the option value.
- It becomes possible to violate this quietly — a small validation here, a
  file check there. Code review must treat CLI-layer logic as a defect.

## Alternatives considered

**Logic in the CLI, refactor later.** Fastest path to v0.1.0. Rejected: the
refactor never happens on schedule, and by then the log schema and CLI
contract have calcified around the wrong shape.

**Library-first, CLI in a separate repository.** Maximum separation.
Rejected as premature: it imposes cross-repo versioning overhead on a
project with one implementation and no external consumers.

**Daemon plus thin client from day one.** The eventual shape if detached
sessions and a tray app both land. Rejected for v0.1.0 because the MVP
deliberately runs sessions in the foreground; a daemon would add supervision
and IPC to the first milestone. This ADR does not preclude it — the core
would sit behind the daemon, unchanged.
