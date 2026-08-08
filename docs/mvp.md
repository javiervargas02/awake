# Awake — MVP Definition (v0.1.0)

> Status: Ratified 2026-08-07 (v0.1).
>
> This document defines exactly what ships in the first release and, just as
> importantly, what does not. It is the scope contract for Stage 0 → v0.1.0.
> Changes to this document are roadmap changes and must be called out
> explicitly.

## Goal of v0.1.0

> Start a bounded, observable stay-awake session on macOS from the terminal,
> and be able to explain afterwards exactly what happened.

v0.1.0 is deliberately narrow. It exists to prove the architecture — core
engine, session model, structured logging, state recovery — not to cover
every use case. If a feature does not serve that goal, it waits.

## Definition of done

v0.1.0 ships when all of the following are true:

1. `awake start 30m` keeps a macOS machine awake for 30 minutes and then
   ends on its own, restoring normal power behaviour.
2. `Ctrl-C` ends the session cleanly, with the same guarantees as a natural
   end — no orphaned system processes, state written, logs closed.
3. Every session produces a readable per-session trace under
   `~/.awake/logs/sessions/<session-id>.jsonl`.
4. Deleting all of `~/.awake` mid-session or between sessions never prevents
   the next command from working.
5. `awake doctor` reports a clean bill of health on a healthy install, and
   correctly identifies each fault we deliberately inject in tests.
6. The CLI contract (commands, flags, exit codes) and the log event schema
   are documented and frozen for the 0.x line under the stated stability
   policy.
7. A user can learn everything above from `README.md` and `--help` without
   reading the source.

## Process model (decided)

**v0.1.0 sessions run in the foreground.** `awake start` holds the terminal
until the session ends. This removes daemon supervision, orphan detection,
and stale-lock handling from the first milestone.

The session model, state file, and log schema must nevertheless be designed
so that a `--detach` flag can be added in v0.2 **without** breaking the CLI
contract or log schema. Concretely, that means v0.1.0 already records the
owning process ID and treats "is that process still alive?" as a first-class
question, even though the answer is trivially "yes" today.

## In scope

### 1. Sessions

- Exactly one active session at a time, enforced by an atomically-acquired OS
  advisory lock held for the session's lifetime.
- Duration parsing in Go's standard duration form (`30m`, `1h30m`, `90s`).
- A default duration when none is given, so `awake start` alone is bounded.
- Explicit indefinite sessions via a flag; never the default.
- Session identity: a unique, sortable, non-guessable ID used for both the
  state record and the log filename.

**Session statuses**

```text
running     — active now
completed   — ended because its duration elapsed
stopped     — ended because the user asked it to
failed      — ended because something went wrong
```

**End reasons** (recorded separately from status, so "why" survives):

```text
duration_elapsed
user_stopped        — explicit `awake stop`
interrupted         — SIGINT / SIGTERM
mode_failure        — the platform mechanism died or refused to start
crashed             — discovered after the fact; process vanished
input_detected      — RESERVED, not emitted in v0.1.0 (see v0.2)
```

Reserving `input_detected` now means adding end-on-input in v0.2 is an
additive, non-breaking change to the log schema.

### 2. System mode only

The single MVP mode asks macOS to stay awake through the platform
abstraction. No synthetic mouse or keyboard input ships in v0.1.0.

The mode abstraction must exist as an interface even with one implementation
behind it — that is the point of the milestone.

### 3. Platform abstraction (macOS)

All macOS-specific behaviour sits behind the platform interface. Core logic
contains no OS conditionals. Windows and Linux implementations are out of
scope but must not require touching core code to add.

**Hard requirement:** when Awake exits by any path — normal end, Ctrl-C,
crash — no system process it started may survive it. An orphaned
keep-awake process outliving its session is the single worst trust bug this
project can ship.

### 4. State and filesystem

```text
~/.awake/
├── config.toml          minimal, optional, recreatable
├── session.json         current/last session record
├── update.json          update-check cache
└── logs/
    ├── awake.jsonl      global application log
    └── sessions/
        └── <id>.jsonl   per-session trace
```

Every one of these is recreatable. None holds authoritative truth: the
binary knows its own version, and a missing file means "unknown," never
"broken."

**Exclusivity** is enforced by an OS advisory lock held for the session's
lifetime ([ADR-0008](adr/0008-session-exclusivity.md)). It is the one file
Awake writes outside `~/.awake`, which is what allows deleting that directory
mid-session without permitting a second session. The session record carries
PID information for diagnostics only.

Minimal config only — enough to prove config loading and recovery:

| Key | Purpose |
| --- | --- |
| `session.default_duration` | duration used when `start` is given none |
| `updates.enabled` | master switch for the update check |
| `updates.check_interval` | minimum time between checks |

Presets, profiles, and a full config surface are M11, not MVP. A missing or
partially invalid config falls back to built-in defaults, logs that it did
so, and never blocks the command.

### 5. Logging

JSON Lines, two destinations: the global log and the per-session trace.
Session-scoped events go to both, so `awake.jsonl` stays a complete
timeline while each session remains independently readable.

MVP event set:

```text
app.started
config.loaded
config.defaulted
session.created
session.started
mode.started
mode.stopped
session.completed
session.stopped
session.failed
session.recovered
update.check.started
update.check.completed
update.available
health.check.completed
repair.performed
```

Deliberately **not** in the MVP set: `session.tick`. A periodic heartbeat is
tempting for observability but writes unbounded log volume for a
low-information event. If we later find we can't explain a session's
behaviour without it, that is evidence to add it — with a documented
interval and rotation story.

Every event carries a timestamp, level, event name, and — where applicable —
the session ID. Privacy principle 5 is absolute here: the logs record what
*Awake* did, never what the user did.

### 6. Health: doctor and repair

`doctor` diagnoses and never mutates. It checks: directory existence and
permissions, config readability and validity, session-record integrity,
stale/crashed session detection, log directory writability, and update-cache
readability. It reports each check as ok / warning / problem with a plain
explanation.

`repair` performs only safe, non-destructive fixes: recreating missing
directories, restoring a default config, resolving a stale session record,
and recreating a corrupt update cache. It never deletes logs and never
touches a genuinely running session. Every repair emits a log event.

### 7. Update checking (notification only)

Fetch a signed-over-HTTPS version manifest, compare against the version
compiled into the binary, cache the result, and tell the user when something
newer exists. Checks respect the cached interval — a CLI command is never
gated on a network round trip. Failure to reach the network is a logged
non-event, never an error the user has to care about.

**No self-installation in v0.1.0.** Notification tells the user what to run.
Severity levels (`optional`, `recommended`, `required`, `security`) are
carried in the manifest schema so policy can be added later without a
breaking change, but v0.1.0 enforces no policy and blocks nothing.

## CLI surface (frozen for 0.x)

```text
awake start [duration]     start a session
awake stop                 end the running session
awake status               show current/last session
awake doctor               diagnose installation health
awake repair               apply safe fixes
awake version              print version and build info
awake update check         check for a newer release
```

Global flags: `--json`, `--verbose`, `--help`. Command flags: `--indefinite`
on `start`, `--clean-quarantine` on `repair`, `--force` on `update check`.
The full specification is the [CLI contract](architecture/cli-contract.md).

**Exit codes** — part of the public contract from day one:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | unexpected internal error |
| 2 | usage error (bad flags or arguments) |
| 3 | precondition not met (session already running; no session to stop) |
| 4 | RESERVED — blocked by a required security update (v0.2+) |
| 5 | diagnostics found problems (`doctor`); warnings alone still exit 0 |

## Out of scope for v0.1.0

Each of these is deferred deliberately, with a home:

| Excluded | Goes to |
| --- | --- |
| End session on user input returning | v0.2 (design slot already reserved) |
| Detached / background sessions | v0.2 |
| Mouse and keyboard modes | M10 |
| Windows and Linux support | post-1.0 |
| Self-installing updates, rollback, channels | M9+ / v0.3 |
| Update *policy* enforcement and session blocking | v0.2 |
| Presets, profiles, full config surface | M11 |
| Desktop UI, tray app, local HTTP API | M12+ |
| Log rotation and retention policy | v0.2 (see risks) |
| `awake logs` viewer command | post-MVP; the files are readable |
| Shell completions, man pages, Homebrew tap | release polish, v0.1.x |

## Known limitations to document honestly

These are not bugs and must be stated plainly in the README rather than
discovered by users:

1. **Closing the laptop lid still sleeps the machine.** The macOS mechanism
   Awake uses does not override lid-close sleep. Claiming otherwise would be
   a trust failure.
2. **Foreground only.** Closing the terminal ends the session. That is the
   documented v0.1.0 behaviour, not a defect.
3. **Sessions are wall-clock bounded but sleep-aware.** If the machine
   sleeps anyway, the session's accounting of elapsed time must be
   explainable from its logs.

## Risks and open questions

- **Orphaned platform process.** The highest-severity risk in the MVP.
  Needs an explicit design answer in the platform architecture doc and a
  test that kills Awake ungracefully and asserts nothing survives.
- **Unbounded log growth.** With no rotation in v0.1.0, session traces
  accumulate forever. Acceptable at MVP volume; must not stay unaddressed
  past v0.2. Open question: cap by age, by count, or leave it to the user?
- **Duration accounting across sleep.** Monotonic and wall-clock time
  diverge when a machine suspends. Which one bounds a session is a real
  decision, not an implementation detail, and belongs in the session
  lifecycle ADR.
- **Manifest hosting and signing.** Where the update manifest lives and how
  its integrity is verified is unresolved. It gates M9 but nothing earlier.

## Milestone mapping

```text
M1  Go project skeleton
M2  Session model
M3  File store
M4  Logging
M5  macOS system mode
M6  App service
M7  CLI integration
M8  Health: doctor / repair
M9  Update checking
    ↓
v0.1.0
```

M10 (mouse mode), M11 (config/presets), and M12 (UI/API preparation) sit
beyond this document.

## Ratification

**Ratified 2026-08-07.** Changes from here require an explicit roadmap
callout — the point of writing it down is that scope creep has to be a
decision, not a drift.
