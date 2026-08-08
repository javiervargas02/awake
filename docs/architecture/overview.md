# Architecture — Overview

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0001](../adr/0001-cli-first-architecture.md) (core-first),
> [ADR-0003](../adr/0003-local-first-recoverable-state.md) (local state),
> and [ADR-0006](../adr/0006-platform-abstraction-and-process-lifetime.md)
> (platform abstraction).

## The shape

```text
        ┌─────────────────────────────────────────────┐
        │  Frontends                                  │
        │  CLI (v0.1)   ·   GUI / tray / API (later)  │
        └──────────────────────┬──────────────────────┘
                               │  intent in, structured results out
        ┌──────────────────────▼──────────────────────┐
        │  Application service                        │
        │  orchestrates use cases; owns no mechanism  │
        └──────────────────────┬──────────────────────┘
                               │
   ┌───────────┬───────────┬───┴───────┬───────────┬───────────┐
   │  session  │   mode    │   store   │  update   │  health   │
   │  (domain) │           │           │           │           │
   └───────────┴─────┬─────┴───────────┴───────────┴───────────┘
                     │
        ┌────────────▼────────────┐     ┌──────────────────────┐
        │  Platform abstraction   │     │  config   ·  logging │
        │  macOS (v0.1)           │     │  (cross-cutting)     │
        └────────────┬────────────┘     └──────────────────────┘
                     │
                    OS
```

Four layers, one direction. Frontends depend on the application service; the
service depends on the domain and subsystems; those depend on the platform
abstraction; the platform depends on the OS. Nothing points back up.

## Packages

```text
cmd/awake/            composition root: wiring and process entry
internal/cli/         command parsing, output rendering, exit-code mapping
internal/app/         application service — the use cases
internal/session/     the session domain type, statuses, end reasons
internal/mode/        mode interface; system mode (v0.1)
internal/platform/    platform interface; macOS implementation
internal/store/       persistence of session record, config, update cache
internal/logging/     structured logging, global and session-scoped
internal/config/      defaults, loading, validation, generation
internal/update/      manifest fetch, cache, version comparison
internal/health/      doctor checks and repair actions
```

`internal/` is a Go convention, not a naming preference: packages under it
cannot be imported from outside this module. That makes "this is not a public
library yet" a compiler-enforced fact rather than a promise in a README.

## The rules

These are the invariants that keep the shape from eroding. A violation is a
defect, reviewable as such.

**1. Dependencies point one way.** No package imports a frontend. `internal/app`
never imports `internal/cli`. If the core seems to need something from the
CLI, the CLI should have passed it in.

**2. The core returns data, not prose.** Application operations return
structured results and typed errors. They never return a string formatted for
a terminal, never print, and never decide colour, alignment, or verbosity.
Rendering belongs to whichever frontend asked.

**3. Exit codes are mapped in exactly one place.** The CLI translates typed
core errors into the exit codes in the MVP contract. Nothing else knows an
exit code exists.

**4. No globals, no singletons, no hidden state.** No package-level logger,
no ambient config, no `init()` side effects. Everything a component needs
arrives through its constructor. This is what makes the core testable without
a filesystem, a clock, or a real machine.

**5. The composition root does the wiring.** `cmd/awake` is the only place
that knows which concrete implementations are in play. It builds the store,
the logger, the platform controller, and the application service, then hands
control to the CLI. Swapping macOS for a fake is a change in one file.

**6. Time and the filesystem are injected.** The application service does not
read the wall clock directly; it asks a clock it was given. ADR-0002 makes
deadlines wall-clock authoritative, which means deadline behaviour — including
the wake-from-sleep case — is only testable if time is a seam. The same holds
for the root directory: tests point it at a temporary path.

**7. Platform specifics stay behind the platform package.** No OS
conditionals, no build tags, and no platform vocabulary anywhere else.

**8. Logging is injected, never imported ambiently.** Components receive a
logger. Session-scoped components receive one already bound to the session ID,
so no caller has to remember to attach it.

## A note on interfaces (Go)

Go's interfaces are satisfied structurally: a type implements an interface by
having the right methods, without declaring that it does. That has a
consequence worth designing around.

The interfaces here are **defined by the consumer, not the implementer**. The
application service declares what it needs from a store, a clock, or a
platform controller — and `internal/store` and `internal/platform` satisfy
those without importing the service. The dependency arrow points inward while
the code stays decoupled, and interfaces stay small because they describe one
caller's needs rather than one implementation's capabilities.

The practical test: if an interface has a dozen methods, it is describing an
implementation and probably belongs somewhere else.

## Walking one command through the system

`awake start 30m`, end to end:

1. **`cmd/awake`** builds the concrete pieces — store rooted at `~/.awake`,
   logger, macOS platform controller, application service — and hands off to
   the CLI.
2. **`internal/cli`** parses `start` and `30m`, validates the duration into a
   typed request, and calls one operation on the service. It does nothing
   else.
3. **`internal/app`** loads config (defaults if absent), checks that no
   session is active, constructs a session with an ID and an absolute UTC
   deadline, and persists the record via the store *before* touching the
   platform — so a crash in the next step is always attributable.
4. **`internal/mode`** (system mode) asks the **platform controller** to begin
   keeping the machine awake. The child process is tied to Awake's lifetime
   per ADR-0006.
5. **`internal/logging`** has been recording throughout: `session.created`,
   `session.started`, `mode.started` — to both the global log and the
   session's own trace.
6. **`internal/app`** waits on whichever comes first: the deadline, a stop
   signal, or a platform failure.
7. On any of those, it stops the mode, writes the terminal status and end
   reason, logs the closing events, and returns a structured result.
8. **`internal/cli`** renders that result and exits `0`.

Note what the CLI did: parse, call, render. Everything else is a frontend a
GUI would reimplement — which is exactly what ADR-0001 forbids.

## Concurrency and cancellation

A running session is a wait on three possible outcomes: a timer reaching the
deadline, a cancellation arriving, or the platform mechanism failing.

Cancellation is carried by Go's `context`, threaded from the composition root
down through the service. Signal handling lives at the top — `cmd/awake`
translates SIGINT and SIGTERM into cancellation of that context — so the core
knows only "you were asked to stop," never which signal caused it. That
keeps signal handling, a process-level concern, out of the domain.

The session's *end reason* is a separate question from cancellation, and it is
resolved in the session lifecycle document.

## Error handling across the boundary

The core returns errors that describe *what went wrong in domain terms* — a
session is already running, no session exists, the platform mechanism failed
to start. It does not return errors phrased for a user.

The CLI is the only translator: domain error → exit code → human sentence (or
a JSON object under `--json`). This is why rule 3 matters. The exit-code table
is a public contract under principle 8, and a contract enforced in one
function is a contract that can actually be tested.

## Testing seams

The rules above exist largely to create these, and the required v0.1.0 tests
depend on them:

| Seam | Enables |
| --- | --- |
| Injected clock | Deadline expiry and wake-from-sleep without waiting |
| Injected root directory | Full state and recovery tests in a temp dir |
| Platform interface | Core tests with a fake; no real power management in CI |
| Store interface | Fault injection per file class (ADR-0003) |
| Structured results | Asserting on data instead of parsing terminal output |

The one test that cannot be faked is ADR-0006's orphan guarantee: kill Awake
ungracefully and assert that nothing survives. That requires the real
platform and is a required test regardless.

## Out of scope for this document

The session state machine (document 2), the log event schema (3), the store
and repair specification (4), the update flow (5), the platform interface in
detail (6), the CLI contract (7), and the testing strategy (8).
