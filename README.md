# Awake

Awake keeps your computer awake for a bounded, observable, user-controlled
session — and always tells you exactly what it did.

```text
$ awake start 30m
Keeping this computer awake — system mode, until 15:42:07 (30m0s from now).
Press Ctrl-C to stop, or run 'awake stop'.
```

When the 30 minutes are up, the session ends on its own and your machine goes
back to sleeping normally. Every session leaves a trace you can read.

## Why another keep-awake tool?

Most "stay awake" utilities are opaque: you can't tell what they're doing to
your machine, whether they're still doing it, or when they'll stop. Awake is
built on the opposite premise — its core value is **trust**:

- **Bounded by default.** Sessions have a duration and an end. Running
  indefinitely requires asking for it explicitly (`--indefinite`); it is
  never the default.
- **Observable.** Every meaningful action is logged as structured JSON, and
  each session gets its own trace file. If Awake did it, you can see it.
- **Verified.** Awake doesn't just ask macOS to stay awake — it queries the
  system afterwards to confirm a sleep assertion was actually registered, and
  tells you if it couldn't confirm it.
- **Recoverable.** You can delete Awake's entire state directory at any time,
  even mid-session, and nothing breaks.
- **Honest.** No telemetry, no accounts, no hidden behaviour — and a clear
  list of what Awake *cannot* do, below.

Awake is **not a stealth tool**. It does not hide from monitoring software or
disguise its activity. It is an honest system utility with an audit trail.

## Install

Awake currently supports **macOS only** (Apple Silicon and Intel).

Until the first binary release is published, build from source (requires Go):

```text
git clone https://github.com/javiervargas02/awake.git
cd awake
make build          # produces ./awake with version information stamped in
```

## Usage

```text
awake start [duration]     start a session (default 30m; e.g. 90s, 45m, 1h30m)
awake start --indefinite   run until stopped — never the default
awake stop                 end the running session (from any terminal)
awake status               show the current or most recent session
awake doctor               check the installation and explain anything wrong
awake repair               apply the safe fixes doctor identified
awake update check         see whether a newer release exists (never installs)
awake version              print version and build information
```

Sessions run in the foreground: `awake start` holds the terminal until the
session ends, and Ctrl-C ends it cleanly. From another terminal, `awake
status` shows what's running and `awake stop` ends it.

Every command accepts `--json` for machine-readable output and `--verbose` to
mirror log events to stderr as they happen.

### Scripting

`awake status --json` is the supported way to read Awake's state from a
script. The files under `~/.awake` are an implementation detail and may
change; the JSON output and the exit codes below are versioned contracts.

| Exit code | Meaning |
| --- | --- |
| 0 | success |
| 1 | unexpected internal error |
| 2 | usage error |
| 3 | precondition not met — a session is already running, or none is |
| 5 | `doctor` found problems (warnings alone still exit 0) |

## How it works

Awake runs macOS's own `caffeinate` utility (by absolute path) as a child
process, which registers a `PreventUserIdleSystemSleep` assertion with the
system. Awake then queries `pmset` to confirm the assertion actually exists
and is attributable to its own process — a session is never reported as
running while the machine is not actually being kept awake.

**No process Awake starts can outlive Awake.** The mechanism is tied to
Awake's process at the operating-system level, so even `kill -9` cannot leave
an orphaned process silently keeping your machine awake. This is tested
against real macOS on every release.

## What Awake cannot do

These are properties of macOS, not bugs — and you should know them up front:

- **Closing the lid still sleeps the machine.** No assertion overrides
  lid-close sleep.
- **Choosing Sleep from the Apple menu still works.** You are in control;
  Awake does not block an explicit sleep.
- **Critically low battery still sleeps the machine.** Correctly.
- **The display may still sleep and the screen may still lock.** v0.1.0
  prevents idle *system* sleep only; keeping the display awake is a planned
  option, not the current behaviour.
- If the machine does sleep through a session's deadline (lid close, say),
  the session is over when it wakes — it is not extended to compensate, and
  the logs record how late the ending was noticed.

## Your data

Everything Awake stores lives in `~/.awake`:

```text
~/.awake/
├── config.toml     optional settings, documented inline; Awake never edits it
├── session.json    the current or most recent session
├── update.json     cached result of the last update check
└── logs/
    ├── awake.jsonl              everything Awake has done
    └── sessions/<id>.jsonl      one trace per session
```

**You may delete any of it, or all of it, at any time** — even mid-session.
Awake recreates what it needs and says so in its logs. Deleting `~/.awake`
costs you your history and preferences, nothing else.

One file lives outside that directory: a zero-byte lock at
`$TMPDIR/awake-<uid>/session.lock`, which is what guarantees only one session
runs at a time — even if you delete `~/.awake` mid-session. It holds no data
and the OS clears it on reboot.

### Privacy

Awake's logs record what *Awake* did, never what you did. No keystrokes,
clipboard, screenshots, window titles, application names, or personal paths —
ever, at any verbosity. This is enforced by an allowlist test in the suite: a
log field that hasn't been explicitly declared fails the build.

There is no telemetry. Awake's only network activity is the explicit,
cache-backed update check described below, and with `updates.enabled = false`
in the config, Awake makes **no network requests at all**.

## Updates

`awake update check` fetches a small JSON manifest over HTTPS, compares the
published version against the one compiled into the binary, and tells you if
something newer exists — with a link to the release notes.

**Awake never installs updates.** It tells you what's available; installing
is always your action, on your schedule. Results are cached (default: one
check per 24 hours), so no command ever waits on the network, and being
offline is never an error.

Two honest disclosures:

- An update check necessarily reveals your IP address to the host serving the
  manifest (GitHub Pages), as any update check does. That's why the feature
  can be turned off entirely.
- The manifest is served over TLS but is not additionally signed. Since Awake
  never downloads or executes anything from it — the worst a tampered
  manifest can do is misstate a version number — signing would add ceremony,
  not safety. The reasoning is documented in
  [ADR-0009](docs/adr/0009-update-manifest.md), and this position will be
  revisited before any self-update feature ships.

## Reading a session's trace

Each session's log is a few lines of JSON you can actually read:

```text
{"ts":"…","event":"session.created","data":{"mode":"system","requested_duration":"30m0s",…}}
{"ts":"…","event":"mode.started","data":{"mechanism":"caffeinate","assertion_verified":"verified",…}}
{"ts":"…","event":"session.started"}
{"ts":"…","event":"mode.stopped","data":{"mode":"system"}}
{"ts":"…","event":"session.completed","data":{"end_reason":"duration_elapsed","elapsed":"30m0.1s","overrun":"0s"}}
```

A trace contains nothing personal by construction, which makes it safe to
attach to a bug report without review.

## Design and contributing

Awake is deliberately over-documented for its size. If you want to understand
or change it, start here:

- [Vision](docs/vision.md) and [principles](docs/principles.md) — what this
  project is and the constraints every change must satisfy
- [Architecture](docs/architecture/README.md) — eight documents covering the
  session lifecycle, logging, state and repair, the platform layer, the CLI
  contract, and the testing strategy
- [Architecture Decision Records](docs/adr/README.md) — why things are the
  way they are, including the alternatives that were rejected

Interfaces are contracts: CLI commands, flags, exit codes, config keys, and
the log schema are semver-governed once released. Breaking them requires a
major version.

## License

Not yet chosen — this will be settled before the v0.1.0 release.
