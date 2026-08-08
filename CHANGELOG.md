# Changelog

All notable changes to Awake are documented here. Entries are meant to be
meaningful, not vague: each one says what changed and, where it matters, why.

The format follows [Keep a Changelog](https://keepachangelog.com/), and Awake
uses [semantic versioning](https://semver.org/): patch releases fix, minor
releases add, major releases break. CLI commands, flags, exit codes, config
keys, and the JSON log schema are public API from v0.1.0 onward.

## [Unreleased]

### Added

- Sessions: `awake start [duration]` keeps a macOS machine awake for a
  bounded session that ends on its own. `--indefinite` runs until stopped and
  is never the default. Exactly one session can run at a time, enforced by an
  OS-level lock that survives even deletion of Awake's state directory.
- Assertion verification: after starting, Awake confirms with the system that
  a sleep assertion was actually registered and is attributable to its own
  process. A session that cannot be verified says so rather than claiming
  success.
- Process-lifetime guarantee: no process Awake starts can outlive Awake,
  including under `kill -9`, enforced at the OS level and covered by a system
  test against real macOS.
- `awake stop` ends the running session from any terminal. `awake status`
  reports the current or most recent session; `--json` output is the
  supported programmatic interface to Awake's state.
- Structured logging: every meaningful action is one JSON line in a global
  log, and each session additionally gets its own trace file under
  `~/.awake/logs/sessions/`. The event vocabulary and envelope are public
  API, and a privacy allowlist test guarantees no undeclared field can reach
  a log.
- Crash recovery: a session whose process died is detected and resolved by
  the next command, automatically. Corrupt files are set aside for
  inspection, never deleted.
- `awake doctor` diagnoses the installation (and never modifies anything);
  `awake repair` applies exactly the fixes doctor identified, and nothing
  else. `repair --clean-quarantine` deletes set-aside files when you say so.
- `awake update check` reports whether a newer release exists, with a link to
  its release notes. Results are cached; Awake never installs updates, never
  sends its version, and with `updates.enabled = false` makes no network
  requests at all. `doctor` surfaces a known available update without
  touching the network.
- Configuration at `~/.awake/config.toml`: optional, generated with inline
  documentation on request, never edited by Awake once it exists. Invalid
  values degrade per key with a logged warning rather than failing the file.
- `awake version` reports the version compiled into the binary. It reads no
  state and leaves no trace on disk.
