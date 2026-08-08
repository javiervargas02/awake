# Architecture

These documents describe how Awake is built. They sit between the ADRs
(*why* a decision was made) and the implementation (*what* the code does):
an architecture doc explains the shape that follows from the decisions.

Where an ADR and an architecture document disagree, the ADR wins — or the
ADR is superseded deliberately.

## The series

| # | Document | Status |
| --- | --- | --- |
| 1 | [Overview](overview.md) — layers, packages, dependency rules, wiring | Ratified |
| 2 | [Session lifecycle](session-lifecycle.md) — states, transitions, timing, recovery | Ratified |
| 3 | [Logging](logging.md) — sinks, event schema, privacy, failure behaviour | Ratified |
| 4 | [State and repair](state-and-repair.md) — file store, atomicity, doctor/repair specification | Ratified |
| 5 | Update architecture — manifest, cache, check flow | Blocked |
| 6 | [Platform abstraction](platform.md) — interface, macOS mechanism, lifetime guarantee | Ratified |
| 7 | [CLI contract](cli-contract.md) — commands, flags, output, exit codes | Ratified |
| 8 | [Testing strategy](testing.md) — seams, fakes, fault injection, required tests | Ratified |

Documents 1 and 2 are prerequisites for the rest; the remaining six are
largely independent of each other.

## Status

Seven of eight documents are drafted. **Document 5 is blocked**, not skipped:
it is gated on an unresolved decision — where the update manifest is hosted
and how its integrity is verified (see the [ADR index](../adr/README.md)
known gaps). Writing it before that decision would produce fiction.

It gates M9 alone, the last milestone before v0.1.0, so M1–M8 can proceed
without it.
