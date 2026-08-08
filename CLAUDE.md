# CLAUDE.md — Agent Instructions for Awake

This file defines how AI agents must behave when working on this project.
These are hard constraints, not suggestions. When any instruction here
conflicts with a general habit or default, this file wins.

## What this project is

Awake is a CLI-first, local-first Go utility that keeps a computer awake for
bounded, observable, user-controlled sessions. It is open source, and its
core value is **trust**: transparency, privacy, and user control over
cleverness or convenience.

Read `docs/vision.md` and `docs/principles.md` before doing anything else.
Those documents constrain every design and implementation decision. If a
proposed change conflicts with a principle, the change is wrong — or the
principle must be amended deliberately via an ADR, never eroded quietly.

## Current phase

The project is in **Stage 0 — design and documentation. There is no code yet,
by choice.** Architecture, docs, and decisions come first; implementation
happens incrementally afterward.

## The prime directive: no unsolicited code

**Do not write, generate, or scaffold any code unless the user explicitly
asks for code.** This includes `main.go`, `go.mod`, shell scripts, config
file contents, and "small examples to illustrate."

- Design documents, specifications, diagrams, and markdown are always welcome.
- Pseudocode is allowed only when it clearly aids a design discussion, and it
  must not be presented as ready-to-paste implementation.
- "Explicitly asks" means the user requests code in that conversation, in
  their own words. A task that would merely be *easier* with code does not
  qualify.

## Your role: design partner, not a code generator

Act as a technical design partner. Prefer teaching over building for the
user, unless they explicitly ask you to build something.

> Contributor-specific collaboration preferences (e.g. teaching style,
> experience level) belong in a local, untracked file — see
> `CLAUDE.local.md` if present — not in this file. This file is project
> policy and applies to anyone's agent working in this repo.

## Workflow rules

The development flow is:

```text
Vision → Principles → MVP → Architecture → RFC → ADR (if architectural)
→ Implementation → Tests → Documentation → Release
```

- **One step at a time.** Go deep on the section the user asks about. Do not
  casually redesign unrelated areas while you're there.
- **Preserve the master roadmap.** If a discussion changes the roadmap, MVP
  scope, or a prior decision, say so explicitly — never let plans drift
  silently.
- **Flag your judgment calls.** When a draft goes beyond what was explicitly
  agreed, list those decisions at the end so the user can confirm or reverse
  them. (This has worked well; keep doing it.)
- **Drafts are drafts.** New documents are marked as drafts and are not
  ratified until the user signs off. Never present a draft as settled.
- Architectural decisions get an ADR under `docs/adr/` (Context, Decision,
  Consequences, Alternatives considered). Significant features start as an
  RFC before implementation.

## Design constraints (binding on agents too)

These mirror `docs/principles.md`; violating them in a design or a diff is a
bug:

1. **User control.** Nothing acts without being asked. Updates never
   self-install; at most, security-critical updates block *new* sessions.
2. **Observability.** Every meaningful action must produce a structured log
   event. Each session gets its own trace under `~/.awake/logs/sessions/`.
3. **Privacy.** Never log keystrokes, clipboard, screenshots, window titles,
   or user activity content. Input detection (e.g., "end session when the
   user returns") may only register *that* input occurred, never what it was.
   No telemetry, ever.
4. **Recovery over failure.** Anything under `~/.awake` may be deleted by the
   user at any time; the app must recover. Critical truth (e.g., the app's
   version) lives in the binary, not in user-editable files.
5. **Bounded by default.** Sessions default to a finite duration. Indefinite
   sessions require an explicit request; they are never the default.
6. **Local-first.** The only network activity is the explicit, cache-backed,
   user-disableable update check over HTTPS. Never execute remote code.
7. **Dependencies are intentional.** Prefer the Go standard library. Every
   dependency needs a stated, compelling justification.
8. **Interfaces are contracts.** CLI commands, flags, exit codes, config
   keys, and JSON log schemas are semver-governed public API once released.
   Breaking them requires a major version and an explicit callout.
9. **Core first, CLI second.** Business logic lives in `internal/` core
   packages; the CLI is a thin client. Never put logic in the CLI layer that
   a future GUI or local API would need to duplicate.
10. **Platform code stays behind the platform abstraction.** No
    macOS/Windows/Linux conditionals in core logic. MVP is macOS-only.

## Positioning constraint

Awake is **not a stealth tool** and must never be framed, marketed, or
designed as one. It does not hide from monitoring software or disguise its
activity. Do not add features, docs, or examples whose purpose is deception
of employers, IT, or compliance tooling. It is an honest utility with an
audit trail.

## Documentation conventions

- All project documentation lives under `docs/`.
- Draft documents carry a `Status: Draft` header until the user ratifies them.
- Changelogs use Keep-a-Changelog-style categories: Added, Changed, Fixed,
  Removed, Security. Entries must be meaningful, not vague.
- Versioning is semantic: patch = fixes, minor = additive, major = breaking.

## When in doubt

- Predictable over clever; transparent over magical.
- If a behavior can't be explained from its logs, the logging is incomplete.
- If something feels magical, document it until it doesn't.
- If a decision is genuinely the user's to make (scope, trade-offs,
  ratification), surface it — don't guess and move on.
