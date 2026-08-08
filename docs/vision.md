# Awake — Vision

> Status: Ratified 2026-08-07 (v0.1).

## One-line definition

Awake is a CLI-first utility that keeps your computer awake for a bounded,
observable, user-controlled session — and always tells you exactly what it did.

## The problem

Operating systems and workplace tools treat idleness as a signal: screens lock,
machines sleep, statuses flip to "away." Sometimes that signal is wrong — you
stepped away for 30 minutes, you're running a long build, you're presenting,
you're downloading something large. Existing fixes are unsatisfying:

- OS settings are global and easy to forget to undo.
- Ad-hoc scripts and "jiggler" apps are opaque, unbounded, and untrustworthy —
  you can't tell what they're doing to your machine or when they'll stop.
- Most tools have no concept of "done": they run until you remember them.

## What Awake is

- **A session tool, not a daemon.** A session ends deliberately: its duration
  elapses, you stop it, or — if you opt in — you return to the machine and
  real input ends it for you. Indefinite sessions exist, but only when asked
  for explicitly; the default posture is "temporary and bounded."
- **CLI-first.** The terminal is the primary interface. Every capability is
  scriptable and inspectable from the command line.
- **Local-first.** All state lives on your machine under `~/.awake`. No
  accounts, no server-side state, no telemetry.
- **Transparent.** Every meaningful action the app takes is logged in a
  structured, per-session trace you can read. If Awake did it, you can see it.
- **Self-explaining and self-repairing.** `awake doctor` tells you what state
  the app is in and why; `awake repair` fixes what can safely be fixed.
  Deleting `~/.awake` is inconvenient, never catastrophic.
- **User-controlled.** Awake never updates itself silently, never runs remote
  code, and never takes an action you didn't ask for. Updates are offered,
  not imposed (with an explicit, documented exception for security-critical
  releases).

## What Awake is not

- **Not a stealth tool.** Awake does not try to hide from IT, evade monitoring
  software, or disguise its activity. It is an honest system utility with an
  honest name and an audit trail.
- **Not a monitoring or automation platform.** It does not watch what you do,
  record input, take screenshots, or automate your applications.
- **Not a background service you forget about.** Sessions are explicit, and
  running indefinitely is an opt-in choice, never the default.
- **Not a cloud product.** No sign-in, no sync, no remote dashboard.

## Target users

1. **Developers and technical users** who live in a terminal and want a
   trustworthy, scriptable alternative to opaque "mouse jiggler" apps.
2. **Anyone running long unattended tasks** — builds, renders, downloads,
   presentations — who needs the machine awake for a known window of time.
3. **Contributors** — people who want to read, audit, and extend a small,
   well-documented Go codebase.

## Primary use cases

- "Keep my machine awake for the next 30 minutes while I step away."
- "Keep the system from sleeping while this long-running job finishes."
- "Keep it awake until I'm back — end the session the moment I actually
  touch the mouse or keyboard again."
- "Show me what Awake did during that session yesterday." (per-session trace)
- "Something looks off — diagnose and repair Awake's own state."

## Non-goals

These are deliberate exclusions, not missing features:

- Bypassing or deceiving employer monitoring, MDM, or compliance tooling.
- Telemetry, analytics, or any phone-home behavior beyond an explicit,
  cache-backed update check.
- Silent self-modification: no auto-install of updates without consent
  (security-critical releases may block *new sessions*, but never install
  themselves).
- Being a general system-tweaking or automation toolkit.
- Monetization as a design driver. Awake is open source; its currency is trust.

## Success criteria

Awake succeeds if:

- A stranger can read the docs and predict exactly what the binary will do.
- A session always ends when it says it will — or the logs explain why not.
- A user who deletes `~/.awake` loses nothing they can't regenerate.
- The codebase is small and boring enough that a first-time contributor can
  find their way around in an afternoon.

## Related documents

- [Principles](principles.md) — the rules that constrain every design decision.
- Roadmap, MVP definition, and ADRs — to follow in Stage 0.
