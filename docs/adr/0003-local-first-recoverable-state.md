# ADR-0003 — Local-first, recoverable filesystem state

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 4 and 5, [MVP](../mvp.md)

## Context

Awake keeps state under `~/.awake`. That directory belongs to the user, who
may delete it, edit it, sync it, back it up, or restore an old copy of it at
any moment — reasonably, and without telling us. A tool that breaks when its
dotfiles vanish fails principle 4.

There is also a subtler trap: if the app trusts a file for something
critical, a stale or hand-edited file becomes a lie the app repeats. The
motivating example is version reporting — an app that reads its own version
from a user-editable file can be made to misreport itself.

## Decision

**All state is local, on the filesystem, in human-readable formats.** No
database, no remote store, no hidden caches elsewhere on the system.

**Every file under `~/.awake` is expendable.** Each is classified, and each
classification has defined behaviour when the file is missing or unreadable:

| File | Class | If missing or invalid |
| --- | --- | --- |
| `config.toml` | recoverable | fall back to built-in defaults, log `config.defaulted` |
| `session.json` | recoverable | treat as "no known session" and continue |
| `update.json` | cache | discard, re-check on the next interval |
| `logs/` | derived | recreate the directory |

**No file holds authoritative truth about the program itself.** The version
is compiled into the binary. Configuration expresses preference, never fact.
If a file and the binary disagree about what the binary is, the binary wins.

**Corrupt files are moved aside, not deleted.** A file that fails to parse is
renamed with a suffix and left in place for inspection, then regenerated.
Recovery should not destroy evidence, and a user who wants to know what went
wrong deserves the artifact.

**Writes are atomic.** State is written to a temporary file in the same
directory and renamed over the target, so an interrupted write can never
leave a half-written session record. Partially written state is the one
recovery case that is genuinely hard to reason about; the write strategy
eliminates it rather than handling it.

**Mutual exclusion is not a state-file concern.** This ADR originally
proposed using `session.json` plus a PID liveness check as the exclusivity
mechanism. That position is **superseded by
[ADR-0008](0008-session-exclusivity.md)**: a session holds an OS advisory
lock, held outside `~/.awake`, and the session record carries PID information
for diagnostics only. The reasoning is in that ADR; the short version is that
exclusivity is a resource to be held, not a fact to be derived from a file
that users are invited to delete.

**Every recovery is logged.** Silent self-repair is indistinguishable from a
bug. If Awake recreated, defaulted, or set something aside, there is an
event saying so.

## Consequences

- `awake doctor` and `awake repair` have a concrete specification: they walk
  this table.
- Deleting `~/.awake` costs the user their history and preferences, and
  nothing else. That is the intended blast radius.
- No dependency is needed for storage itself, satisfying principle 7. The
  config file's format does imply a parser decision; it is resolved
  separately in [ADR-0007](0007-configuration-format.md) rather than
  smuggled in here.
- The recovery paths are numerous enough that they need real tests: fault
  injection per file class, not incidental coverage.
- Two Awake processes racing on the same state file is possible in principle.
  Atomic writes make the outcome well-defined rather than corrupt; genuine
  concurrency control waits for detached mode.

## Alternatives considered

**SQLite for state.** Transactional, solves concurrency properly. Rejected
for the MVP: it adds a dependency, makes state opaque to inspection, and
trades a problem we have (a handful of small files) for machinery we don't
need. Being able to `cat` the state is itself a feature under principle 3.

**One combined state file.** Fewer files to manage. Rejected: it couples
unrelated lifetimes — a corrupt update cache should never be able to take
the session record with it.

**Nothing on disk; reconstruct everything at runtime.** Maximally robust to
deletion. Rejected: it makes per-session history impossible, which is a core
feature, and `status` could not report on a previous session.

**XDG base directories** (`~/.config/awake`, `~/.local/state/awake`).
Arguably more correct on Linux, and worth revisiting when Linux support
lands. `~/.awake` is chosen now for macOS-first simplicity and because a
single directory makes "delete this and nothing breaks" trivially explainable.
