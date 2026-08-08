# Architecture Decision Records

An ADR captures a decision that shaped this project, along with the reasoning
and the alternatives that were rejected. The goal is to preserve the **why**,
so that a future contributor — including a future version of ourselves — can
tell the difference between a deliberate constraint and an accident.

## Conventions

- Filename: `NNNN-short-kebab-title.md`, numbered sequentially, never reused.
- Every ADR has: Context, Decision, Consequences, Alternatives considered.
- **Status** is one of:
  - `Proposed (draft — not ratified)` — written, awaiting sign-off.
  - `Accepted` — ratified, with the date it was accepted.
  - `Superseded by ADR-NNNN` — replaced. The original stays; the record of a
    decision includes the record of changing it.
- ADRs are immutable once accepted. Revisiting a decision means writing a new
  ADR that supersedes the old one — never editing history.
- An ADR records an *architectural* decision. Feature design goes in an RFC;
  an RFC may produce an ADR if it changes architecture.

## Index

| # | Title | Status |
| --- | --- | --- |
| [0001](0001-cli-first-architecture.md) | Core-first architecture, CLI as a thin client | Accepted |
| [0002](0002-session-as-core-domain-object.md) | The session is the core domain object | Accepted |
| [0003](0003-local-first-recoverable-state.md) | Local-first, recoverable filesystem state | Accepted |
| [0004](0004-structured-session-scoped-logging.md) | Structured, session-scoped logging | Accepted |
| [0005](0005-user-controlled-updates.md) | User-controlled updates: notify, never install | Accepted |
| [0006](0006-platform-abstraction-and-process-lifetime.md) | Platform abstraction and the process-lifetime guarantee | Accepted |
| [0007](0007-configuration-format.md) | Configuration format: TOML, and the first dependency | Accepted |
| [0008](0008-session-exclusivity.md) | Session exclusivity via an OS advisory lock | Accepted |
| [0009](0009-update-manifest.md) | Update manifest: a static document, hosted from the repository | Accepted |

## Known gaps

Decisions that are identified but not yet written, and what they block:

| Topic | Blocks |
| --- | --- |
| Log rotation and retention policy | v0.2 |
| Detached sessions: process ownership and locking | v0.2 |
| End-on-input: idle detection within the privacy constraint | v0.2 |
