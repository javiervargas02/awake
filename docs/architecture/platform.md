# Architecture — Platform abstraction

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0006](../adr/0006-platform-abstraction-and-process-lifetime.md).
> Depends on [Overview](overview.md) and
> [Session lifecycle](session-lifecycle.md).

This document specifies the boundary between Awake and the operating system:
what crosses it, what the macOS implementation does, and how the
process-lifetime guarantee is actually enforced.

## Mechanism and policy

Two layers are easy to confuse, so the split is worth stating plainly:

- **A mode is policy.** It decides *what approach* to take to keep the
  machine awake. `system` mode is the only one in v0.1.0.
- **The platform is mechanism.** It knows *how this OS does that*, and
  nothing else.

The platform layer has no idea what a session is. It does not know about
deadlines, end reasons, config, or logging policy. It starts something, stops
something, and reports what it can do. Everything else is above it.

## The interface

Deliberately narrow — three capabilities, no more:

**Describe.** Report what this platform can do: whether the mechanism is
available, what it is called, and which kinds of keep-awake it supports. This
feeds `doctor` (check 9) and is the only way the core learns anything about
the OS.

**Start.** Begin keeping the machine awake, given a request describing which
kind. Returns a handle representing the running mechanism, or an error. The
handle also exposes a way for the caller to learn that the mechanism **died
unexpectedly**, which the session layer treats as a failure rather than a
silent degradation.

**Stop.** Release the mechanism through the handle. Idempotent: stopping an
already-stopped handle is not an error, because shutdown paths can overlap.

That is the entire surface. Anything a caller wants beyond it — retries,
timing, logging, deciding whether a failure ends the session — belongs above
the boundary.

### What the platform layer must never do

- read config, or know that config exists,
- decide *when* a session ends, or how long anything lasts,
- write log events (it returns facts; the caller logs them),
- produce user-facing strings,
- know a session ID.

## The macOS implementation

macOS exposes power management through **assertions**: a running process
declares "do not sleep for this reason," and the assertion holds while that
process lives. Awake drives the system utility `caffeinate`, which registers
assertions on behalf of a process, rather than calling the frameworks
directly (ADR-0006 — auditable, and no cgo in the first milestone).

**v0.1.0 requests one thing: prevent idle system sleep.**

Notably, it does **not** prevent display sleep. Keeping the screen on has real
costs — power, and a screen left visible in a room the user has walked away
from — and it is a different user intent. A `--keep-display-awake` option is a
v0.2 question, and it maps to a different assertion, not a different mode.

**The mechanism is resolved by absolute path**, never through `PATH` lookup.
A tool that spawns a system utility while the user is away must not be
redirectable by an environment variable; this is cheap to do and expensive to
retrofit.

## The process-lifetime guarantee

ADR-0006's hard requirement: **no process Awake starts may outlive Awake.**
An orphaned assertion means a machine that will not sleep, with no visible
cause and no obvious way to stop it — invisible, persistent, and directly
contrary to principles 1 and 3.

Three layers defend it. They are not redundant; each covers what the others
cannot.

### Layer 1 — the kernel-independent tie

The mechanism is asked to **watch Awake's own process and exit when it
exits**. `caffeinate` supports precisely this: it can be told to hold its
assertions only for as long as a given process is alive.

This is the layer that matters, because it holds when *no code of ours runs
at all* — `SIGKILL`, a panic, a force-quit. The child's termination is not
something Awake performs; it is a consequence of Awake ceasing to exist.

There is no portable way to express this, which is itself an argument for the
abstraction: macOS solves it by having the utility watch a PID, Linux offers
a parent-death signal, Windows has job objects. Each platform implementation
owns its own answer, and the interface never mentions the problem.

### Layer 2 — explicit shutdown

Every ordinary exit path — deadline reached, `awake stop`, Ctrl-C, mode
failure — stops the handle explicitly and confirms the mechanism is gone
before writing the session's terminal record. This is the fast, clean path;
layer 1 is what catches the rest.

### Layer 3 — reclamation at startup

Before starting a session, Awake checks whether a mechanism recorded by a
previous run is somehow still alive, and terminates it if so, emitting
`session.recovered`.

This requires knowing which process to look at, so the session record carries
`mechanism_pid` alongside the owner's PID.

**Hard rule: never terminate a PID that cannot be verified.** PIDs are
reused, so before acting, the recorded PID must be confirmed to be the
expected executable. If it cannot be verified, Awake reports the anomaly
through `doctor` and terminates nothing. Killing an innocent process to
protect a guarantee would be a far worse bug than the one being prevented.

With layer 1 working, layer 3 should essentially never fire. It exists
because "should never" is not a guarantee, and because it turns an invisible
failure into a logged one.

## When the mechanism fails

Two distinct failures, both surfaced rather than absorbed:

**Fails to start** → `mode.failed`, then the session ends `failed` /
`mode_failure` and the command exits non-zero. No session record is left
claiming to be running.

**Dies mid-session** → the handle reports it, and the session ends the same
way. Awake does not silently restart it.

Not restarting is deliberate. A mechanism that died once will likely die
again, and a session that quietly flaps between working and not is
unexplainable from its logs — which principle 2 treats as a defect. Ending
honestly is better than continuing dishonestly.

The rule underneath both: **Awake never reports a session as running while
failing to keep the machine awake.** A false claim here is worse than an
error, because the user acts on it by walking away.

## Verifying the assertion

A live child process is *evidence* that the machine is being kept awake, not
*proof*. Principle 2 asks for proof, so after starting the mechanism Awake
queries the OS to confirm an assertion attributable to it actually exists.

Verification distinguishes two outcomes that must not be collapsed:

| Outcome | Meaning | Response |
| --- | --- | --- |
| **Verified** | an assertion is registered | proceed; `mode.started` records `assertion_verified` |
| **Verified absent** | the query worked, and there is no assertion | `mode.failed` → session `failed` / `mode_failure` |
| **Unverifiable** | the query itself failed or could not be parsed | proceed, log at `warn`, record the session as unverified |

The third row is the important one. If Awake cannot *ask* the question, it
must not answer it — failing a session that is probably working, because a
diagnostic query broke, would be its own trust failure. Saying "running, but
I could not confirm it" is honest; saying "failed" would not be.

The query is retried briefly before concluding an assertion is absent, since
registration is not instantaneous.

`doctor` additionally checks that the query facility itself is available, so
a machine that cannot verify says so before a session is started rather than
mid-way through one.

## Capability reporting

`Describe` gives `doctor` something concrete to check before a session is
attempted rather than during one:

- is the mechanism present at its expected path,
- is it executable,
- which keep-awake kinds does this platform support.

An unavailable mechanism is a `doctor` **problem**, not a runtime surprise.

## Modes and the platform, revisited

`system` mode maps directly onto the assertion above. Future modes will not:
mouse and keyboard modes (M10) need a *synthetic input* capability, which is
a different platform facility with different permissions — on macOS,
accessibility permissions the user must grant explicitly.

One constraint belongs on the record now, because it is easy to miss later:
**when end-on-input lands (v0.2), Awake's own synthetic input must never
count as the user returning.** A mouse mode that ends its own session the
moment it moves the cursor would be an absurd, self-defeating bug. Whatever
mechanism reports "the user is back" must be able to exclude input Awake
generated — and if it cannot, the two features are mutually exclusive and
must be documented as such.

## Honest limitations

These are properties of macOS, not defects, and the README must state them
plainly rather than let users discover them:

- **Closing the lid still sleeps the machine.** No assertion overrides it.
- **Explicit user-initiated sleep still works.** Choosing Sleep from the menu
  is not blocked, and should not be — the user is in control (principle 1).
- **Critically low battery still sleeps the machine.** Correctly.
- **Display sleep is not prevented in v0.1.0**, by design.

A tool that claims to keep a machine awake and then silently fails on lid
close has broken its promise. Saying so up front costs nothing.

## Testing

| Test | How |
| --- | --- |
| Core logic, all paths | Fake platform implementation; no real power management in CI |
| Mechanism unavailable | Fake reports unavailable; assert clean failure and a `doctor` problem |
| Dies mid-session | Fake signals unexpected exit; assert `failed` / `mode_failure` |
| **Orphan guarantee** | **Real macOS.** `SIGKILL` Awake mid-session, then assert no mechanism process survives |

The orphan test is the one that cannot be faked, and ADR-0006 makes it
required for v0.1.0. It is also the test most likely to be skipped for
inconvenience, which is exactly why it is named here.

## Adding a platform later

1. Implement the three-capability interface for the OS.
2. Answer the lifetime question with that platform's own mechanism.
3. Report capabilities honestly, including what it cannot do.
4. Add the platform to the composition root.
5. Port the orphan test.

No core changes. If a new platform requires one, the interface is wrong.

## Open questions

1. **Re-verification during long sessions.** Verification happens at start.
   An assertion could in principle be lost mid-session without the child
   dying. Re-checking periodically would catch it — but it reintroduces a
   polling loop, which is exactly what `session.tick` was cut for. Leaning
   toward start-only for v0.1.0 and revisiting if it ever happens in
   practice.
2. **Behaviour on macOS permission changes mid-session** (relevant for M10's
   input modes, not v0.1.0).
