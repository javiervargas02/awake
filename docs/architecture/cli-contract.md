# Architecture — CLI contract

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0001](../adr/0001-cli-first-architecture.md).
> Depends on [Overview](overview.md),
> [Session lifecycle](session-lifecycle.md), and
> [State and repair](state-and-repair.md).
>
> **This document defines public API.** Under principle 8, every command,
> flag, exit code, and `--json` shape below is semver-governed from v0.1.0.

## What the CLI is

A thin client (ADR-0001): parse input into a typed request, call one core
operation, render the result. It holds no logic a future GUI would need to
reimplement.

Two audiences, equally weighted: a person at a terminal, and a script. The
design rule that follows is **stdout is a data channel**. Anything that is not
the command's result — progress, warnings, echoed log events — goes to stderr,
so that piping a command never yields something a program has to clean up.

## Global conventions

### Flags available everywhere

Global flags are accepted by every command, and are written **after** the
command name (`awake version --json`, not `awake --json version`). One
position is easier to document, to complete in a shell, and to predict.

| Flag | Effect |
| --- | --- |
| `--json` | machine-readable output on stdout instead of human text |
| `--verbose` | mirror log events to stderr as they are written (see logging) |
| `--help`, `-h` | usage for the command; exit 0 |

`--verbose` never changes what is written to the log files, and never changes
stdout. It is a window, not a mode.

### Colour and formatting

Colour is used only when stdout is a terminal, and is disabled when the
`NO_COLOR` environment variable is set. Human output carries no emoji and no
spinners: a tool that reports on system power behaviour should read like a
system tool.

### Times and durations

- **Human output** uses the local timezone and relative phrasing where it
  helps ("ends at 15:42, in 28 minutes").
- **JSON output** uses UTC, RFC 3339 with microseconds — identical to the log
  envelope, so the two can be correlated without conversion.
- **Duration input** uses Go's standard form: `30m`, `1h30m`, `90s`. It must
  be positive; `0` and negative values are usage errors.

### `--json` shape

Every `--json` response is a single JSON object carrying `schema_version`,
so consumers can detect a format they do not understand. The rules that apply
to log events (ADR-0004) apply here too: **consumers must ignore unknown
fields**, adding a field is additive, and renaming or removing one is
breaking.

Errors under `--json` are also objects — never bare text — carrying at least
an error identifier and a human-readable description. A script should never
have to parse prose to learn what went wrong.

## Commands

### `awake start [duration]`

Starts a session in the foreground and holds the terminal until it ends
(MVP decision).

| | |
| --- | --- |
| Argument | duration; optional, defaults to `session.default_duration` |
| Flags | `--indefinite` — run with no deadline; mutually exclusive with a duration |
| Exit | `0` ended normally · `2` bad duration or conflicting flags · `3` a session is already running · `1` the mechanism failed |

**Human output.** One line on start naming the mode, the end time, and how to
stop it. When stdout is a terminal, a single updating line shows the time
remaining; when piped, that line is omitted entirely rather than repeated into
a file. One line at the end stating how the session ended.

**`--json` output** is JSON Lines, not a single object: one object when the
session starts, one when it ends. A long-running command that says nothing
until it finishes would be unusable in a script — this way a caller can act on
the start event immediately.

Ctrl-C ends the session cleanly, with the same guarantees as a natural end.

**Deliberately absent: `--mode`.** Only `system` exists in v0.1.0, and a flag
with one legal value teaches users nothing. Adding it later is additive and
non-breaking.

### `awake stop`

Ends the running session by signalling its owner (see session lifecycle).

| | |
| --- | --- |
| Exit | `0` stopped · `3` no session is running · `1` the session did not end within the grace period |

It never escalates to `SIGKILL`; if the grace period expires it says so
plainly. A `--force` flag is a v0.2 question.

### `awake status`

Reports the current session, or the most recent one if none is running.

| | |
| --- | --- |
| Exit | `0` always, including when no session has ever run |

"No session" is information, not an error — a status command that fails when
there is nothing to report is hostile to scripts, which would have to treat a
normal state as an exception.

**This is the supported way to read Awake's state programmatically.** The
files under `~/.awake` are an implementation detail (state and repair); this
output is the contract. The JSON object carries the session's ID, mode,
status, end reason, timestamps, deadline, and remaining time.

For an indefinite session, remaining time is `null` rather than a sentinel
number, and human output says "no scheduled end" rather than inventing a
duration.

`status` performs stale-session detection like every other command, so it can
never report a crashed session as running.

### `awake doctor`

Diagnoses installation health; never mutates anything.

| | |
| --- | --- |
| Exit | `0` all checks `ok` or `warning` · `5` any `problem` found |

Warnings are informational: a machine with no config file and no history is
healthy, not broken. Problems mean something needs attention, and a script
can tell the difference without parsing output.

Every finding names its check, its status, a plain explanation, and the
remedy — and for anything `repair` can fix, the remedy is that command. This
is what makes `doctor` the dry run for `repair`.

### `awake repair`

Applies the safe fixes catalogued in state and repair.

| | |
| --- | --- |
| Flags | `--clean-quarantine` — additionally delete quarantined files, and only those |
| Exit | `0` repairs applied, or nothing needed · `1` a repair failed |

On a healthy installation it does nothing and says so. It reports each action
taken; with nothing to do, that report is empty rather than silent.

### `awake version`

Prints the version compiled into the binary, plus build metadata.

| | |
| --- | --- |
| Exit | `0` |

Reads no state and creates nothing — asking a freshly installed binary its
version leaves no trace on disk (state and repair, bootstrap).

### `awake update check`

Checks for a newer release, subject to the cached interval (ADR-0005).

| | |
| --- | --- |
| Flags | `--force` — ignore the cache and check now |
| Exit | `0` whether or not an update exists, **including when the network is unreachable** |

Being offline is not a failure (ADR-0005). The result — up to date, update
available, or check failed — is reported in the output, not the exit code,
because "I could not reach the network" is not a defect in Awake.

This command **never installs anything.** When an update exists it says what
is available and how to get it.

### `awake` with no command

Prints help and exits `0`. An unknown command or flag is a usage error and
exits `2`.

## Exit codes

The complete, frozen table:

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | unexpected internal error |
| 2 | usage error — bad flags or arguments |
| 3 | precondition not met — session already running, or none to stop |
| 4 | RESERVED — blocked by a required security update (v0.2+) |
| 5 | diagnostics found problems (`doctor`) |

Mapping domain errors to these codes happens in exactly one place (overview,
rule 3), which is what makes the table testable rather than aspirational.

Codes `6`–`125` are unassigned. Awake does not use `126`, `127`, or `128+n`,
which shells reserve for their own meanings.

## Help text

Each command's help states what it does, its arguments and flags with
defaults, and its exit codes. `awake start --help` should be enough to
predict the command's behaviour without reading the docs — the vision's test
is that a stranger can predict what the binary will do.

Help goes to stdout and exits `0` when requested with `--help`; usage shown
because of an *error* goes to stderr and exits `2`.

## Stability policy

| Change | Classification |
| --- | --- |
| New command, flag, or JSON field | additive — minor |
| New exit code for a new condition | additive — minor |
| Renaming or removing a command or flag | breaking — major |
| Changing an exit code's meaning | breaking — major |
| Changing a JSON field's type or meaning | breaking — major |
| Changing human-readable text | not a contract change |

Human output is deliberately **not** part of the contract. Scripts that parse
it are unsupported, which is precisely why `--json` exists and why `status`
is documented as the supported programmatic surface.

## Open questions

1. **A `--quiet` flag** suppressing non-essential human output. Probably
   worth having; adding it later is additive, so it is not urgent.
2. **`--config <path>`** to point at an alternative config file. Useful for
   testing, but it widens the surface and every added flag is a permanent
   promise. Leaning no for v0.1.0.
3. **Shell completions** — release polish rather than contract, but the
   command and flag names above are what they would be generated from.
