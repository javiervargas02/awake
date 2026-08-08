# Awake — Engineering Principles

> Status: Ratified 2026-08-07 (v0.1).
>
> These principles are constraints, not aspirations. When a design or feature
> conflicts with one of them, the design changes — or we amend the principle
> deliberately, in writing (via an ADR), never by quiet erosion.

## 1. The user is always in control

Awake never takes an action the user didn't ask for. Sessions start because
the user started them; updates install because the user chose to install them.
The only sanctioned exception — blocking *new* sessions on a security-critical
update — is documented, visible, and still never installs anything by itself.

## 2. Everything important is observable

Every meaningful action leaves a structured log entry: session lifecycle,
mode activity, repairs, update checks. Each session has its own trace. If a
behavior can't be explained from the logs, that's a bug in the logging.

## 3. Explain, don't surprise

Output, errors, and docs should let a user predict what the tool will do
before running it. If something feels magical, document it until it doesn't.
Predictable beats clever; transparent beats magical.

## 4. Recover, don't fail

Missing or corrupt files under `~/.awake` are an inconvenience Awake handles,
not a crash the user debugs. Critical truth (like the binary's own version)
lives in the binary, never in user-editable files. `doctor` diagnoses,
`repair` fixes, and repairs are themselves logged.

## 5. Local-first, privacy-first

All state is on the user's machine. No telemetry, no analytics, no accounts.
The only network activity is an explicit, cache-backed update check the user
can disable. Logs describe what *Awake* did — never keystrokes, clipboard,
screenshots, or unrelated user activity.

## 6. Sessions end deliberately

The default session is time-bounded. Indefinite sessions are allowed, but only
when explicitly requested — never as the default. Users may also opt in to
ending a session when external input is detected (i.e., they have returned to
the machine). Consistent with principle 5, that detection may only register
*that* activity occurred — never what the activity was.

## 7. Dependencies are intentional

Prefer the Go standard library. A dependency must earn its place with a
compelling reason, and each one is a trust decision made on the user's behalf.

## 8. Interfaces are promises

CLI commands, flags, exit codes, config keys, and log schemas are contracts.
They are versioned semantically, broken only in major releases, and every
breaking change is announced and explained. Machine-readable output stays
stable enough to script against.

## 9. Every release must be worth trusting

Meaningful changelogs, semantic versions, a documented release process, and
honest communication about security issues. A release that erodes trust is
worse than no release.

## 10. The core is the product; the CLI is a client

Business logic lives in a reusable core. The CLI is a thin frontend, and any
future GUI, tray app, or local API is just another client of the same core —
never a reimplementation of it.
