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
| 5 | [Update checking](updates.md) — manifest, cache, check flow | Ratified |
| 6 | [Platform abstraction](platform.md) — interface, macOS mechanism, lifetime guarantee | Ratified |
| 7 | [CLI contract](cli-contract.md) — commands, flags, output, exit codes | Ratified |
| 8 | [Testing strategy](testing.md) — seams, fakes, fault injection, required tests | Ratified |

Documents 1 and 2 are prerequisites for the rest; the remaining six are
largely independent of each other.

## Status

All eight documents are ratified. Document 5 was blocked until
[ADR-0009](../adr/0009-update-manifest.md) settled where the update manifest
lives and how its integrity is established.
