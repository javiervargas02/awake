# ADR-0006 — Platform abstraction and the process-lifetime guarantee

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principle 10, [MVP](../mvp.md)

## Context

Keeping a machine awake is inherently OS-specific: macOS, Windows, and Linux
each expose a different mechanism. The MVP targets macOS only, but the
project intends to support the others without restructuring.

The sharper concern is process lifetime. On macOS the natural mechanism is a
system utility that holds an assertion for as long as it runs. That means
Awake will supervise a child process whose job is to alter the machine's
power behaviour. If Awake dies badly — `SIGKILL`, a panic, a terminal window
closed — and that child survives, the user is left with a machine that will
not sleep and no visible reason why, and no obvious way to stop it. That is
the worst failure this project can ship: invisible, persistent, and directly
contrary to principles 1 and 3.

## Decision

**A narrow platform interface** sits between the core and the OS. It exposes
the minimum: begin keeping the machine awake, stop doing so, and describe
what this platform supports. Core logic contains no OS conditionals; build
tags and platform-specific imports are confined to the platform package.

**The MVP implementation drives a macOS system utility as a child process**
rather than calling system frameworks directly. This is auditable — a reader
can see exactly what Awake asks the OS for — and avoids cgo in the first
milestone.

**Process-lifetime guarantee: no process Awake starts may outlive Awake.**
This is a hard requirement, not best-effort, and it is defended in three
layers:

1. **Tie the child's lifetime to Awake's own**, using the mechanism the
   platform provides for exactly this, so that the child terminates when the
   parent process exits — including when the parent is killed and no cleanup
   code runs.
2. **Explicit cleanup on every ordinary exit path**, including signals.
3. **Defensive reclamation at startup**: a new session checks for a process
   recorded by a previous session that should no longer exist, and stops it,
   logging a `session.recovered` event.

Layer 1 is what makes the guarantee hold under `SIGKILL`; layers 2 and 3 are
the belt and braces.

**Capability reporting** lets `doctor` state what the current platform can
actually do, so an unsupported or degraded environment is a clear diagnosis
rather than a confusing runtime failure.

**Platform failures are session failures.** If the mechanism cannot start or
dies unexpectedly, the session ends as `failed` / `mode_failure` and says so.
Awake never reports a session as running while silently failing to keep the
machine awake — a false claim here is worse than an error.

## Consequences

- Adding Windows or Linux is one new file implementing one interface, plus
  tests. No core changes.
- A fake platform implementation makes the entire core testable without
  touching real power management, which is otherwise nearly untestable in CI.
- The orphan guarantee needs a real test: kill Awake ungracefully and assert
  that nothing survives. This is a required test for v0.1.0, not a nicety.
- A narrow interface gives up platform-specific richness. Anything a future
  mode needs — display-only versus system-wide sleep prevention, for
  instance — will require widening the interface deliberately, across all
  platforms, rather than leaking macOS concepts into the core.
- Depending on a system utility means depending on its presence and
  behaviour. `doctor` should verify it is available rather than discovering
  its absence mid-session.

## Alternatives considered

**Call macOS power-management frameworks directly via cgo.** More control,
no child process, and the orphan problem disappears with it. Rejected for
the MVP: cgo complicates builds and cross-compilation, and framework calls
are harder for a contributor to audit than a visible subprocess. Worth
revisiting if the subprocess model proves limiting — this ADR would be
superseded, not amended.

**No abstraction; write macOS code directly and generalise later.** Cheaper
now. Rejected: this is the exact shortcut principle 10 exists to prevent, and
retrofitting an abstraction after the log schema and session model depend on
macOS specifics is far more expensive than paying for it up front.

**Separate binaries per platform.** Rejected: it multiplies the release
process and fragments the CLI contract for no architectural gain.

**Accept orphaned processes and document the cleanup command.** Rejected.
Documentation does not fix an invisible failure the user does not know to
look for.
