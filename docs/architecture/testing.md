# Architecture — Testing strategy

> Status: Ratified 2026-08-07.
>
> Depends on every preceding architecture document.
> Closes the architecture series.

## What testing is for here

Awake's product is trust. Every claim it makes — the session ends when it
says, nothing survives a crash, the logs contain nothing personal — is a
promise, and an untested promise is a hope.

So the organising rule is not a coverage number:

> **Every claim in the MVP's definition of done has a test that would fail if
> the claim stopped being true.**

Coverage percentage is a diagnostic, never a target. A project can reach 90%
while testing none of the four things that actually matter.

## Tooling

**Go's standard `testing` package, and nothing else.** Principle 7 applies to
test dependencies too — arguably more so, since an assertion library is a
dependency that buys syntax rather than capability.

Go's convention is **table-driven tests**: one test function holding a slice
of cases, each run as a named subtest. It is worth adopting deliberately
rather than discovering later, because it makes adding the seventeenth edge
case a one-line change and gives every case its own name in the output.

Two standard-library facilities carry most of the weight: temporary
directories that are created and cleaned up per test, and cleanup hooks that
run even when a test fails.

## The layers

| Layer | Runs against | Speed | Where |
| --- | --- | --- | --- |
| **Unit** | fakes for clock, platform, store | instant | every commit |
| **Integration** | real filesystem in a temp directory, fake platform | fast | every commit |
| **System** | the real built binary, real macOS, real power assertions | slow | macOS CI + before release |

Most tests are integration tests, and that is deliberate. The interesting
behaviour in Awake is *file states and recovery*, which a real filesystem in a
temporary directory exercises honestly and cheaply. Mocking the filesystem
would test our mock.

## The seams, and the rule for using them

The overview's rules exist to create four seams: an injected clock, an
injected root directory, the platform interface, and the store interface.

**The rule: never fake the thing under test.** A recovery test uses a real
temp directory and a fake platform. A platform test uses the real platform
and cares nothing about state. Faking both sides of a behaviour tests the
wiring of the test.

### Fakes we need

- **Clock** — advanced manually. Lets a 30-minute deadline be tested in
  microseconds, and lets a system sleep be simulated by jumping the clock
  forward past a deadline.
- **Platform** — reports configurable capability, can be told to fail at
  start, and can signal an unexpected mid-session death on demand.
- **Update server** — a local HTTP test server serving manifests, including
  malformed ones. **No test ever touches the real network.** A test suite
  that fails on a plane is a broken test suite.

## Required tests for v0.1.0

Tied one-to-one to the MVP's definition of done. These are release blockers,
not aspirations.

| # | Claim | Test |
| --- | --- | --- |
| 1 | A session ends when its duration elapses | Fake clock advanced past the deadline; assert `completed` / `duration_elapsed` |
| 1 | The machine is genuinely kept awake | **System test**: start a session, query real assertions, confirm one exists |
| 2 | Ctrl-C ends cleanly with a natural end's guarantees | Signal the process; assert `stopped` / `interrupted`, terminal record written, mechanism gone |
| 3 | Every session produces a readable trace | Run a session; parse every line of its trace as JSON; assert the expected event sequence |
| 4 | Deleting `~/.awake` never breaks the next command | Delete between commands, and mid-session; assert every command still works |
| 4 | Deleting `~/.awake` mid-session cannot produce two sessions | Delete mid-session, attempt a concurrent start, assert refusal with exit 3 |
| 5 | `doctor` is clean when healthy, and finds every injected fault | Fault-injection matrix below |
| 6 | Exit codes match the contract | Table test over every command × condition |
| 6 | The log schema has not silently changed | Schema contract test below |
| — | **Nothing survives a `SIGKILL`** | **System test**, below |
| — | Logs contain nothing personal | Privacy allowlist test, below |

## Fault injection matrix

Every file class from ADR-0003, in every broken state, against `doctor` and
`repair`:

| File | Fault | Expected `doctor` | Expected `repair` |
| --- | --- | --- | --- |
| root dir | absent | warning | recreated, correct permissions |
| root dir | permissions too broad | warning | tightened |
| `config.toml` | absent | warning | generated with comments |
| `config.toml` | unparseable | problem | quarantined, defaults generated |
| `config.toml` | unknown key | warning | untouched (it parses) |
| `config.toml` | implausible value | warning | untouched |
| `session.json` | absent | warning | nothing to do |
| `session.json` | unparseable | problem | quarantined |
| `session.json` | non-terminal, lock free | problem | terminal `failed` / `crashed`, `session.recovered` |
| `update.json` | unparseable | problem | discarded |
| `logs/` | absent | warning | recreated |
| `logs/` | not writable | problem | permissions fixed |
| lock | runtime dir unavailable | problem | reported; exclusivity degrades |
| quarantine | files present | warning | untouched without the flag; deleted with it |

Two assertions apply to every row: **`repair` is idempotent** (running it
twice changes nothing the second time), and **`repair` never acts on anything
`doctor` did not report**.

## The orphan test

The one test that cannot be faked, and the one ADR-0006 makes mandatory.

1. Start a real session on real macOS.
2. Confirm the mechanism process exists and an assertion is registered.
3. `SIGKILL` Awake — no cleanup code runs.
4. Assert the mechanism process is gone and no assertion remains.
5. Assert the next command detects the stale session and recovers it.

Step 4 verifies layer 1 of the lifetime guarantee (the process tie) in the
only way that means anything: with our own cleanup code deliberately skipped.

This test is slow, requires a real machine, and will be tempting to skip.
Naming it here is the countermeasure. **If it is skipped, the release does not
ship** — an orphaned assertion is the worst bug this project can produce.

## Schema contract tests

The log event vocabulary and the `--json` shapes are public API (principle 8).
Public API needs a test that fails when it changes, because the failure mode
we are guarding against is not a bug — it is a rename that nobody noticed was
breaking.

**Log events**: a test enumerating every event name and its required `data`
fields. Adding an event means adding a line; renaming one means changing a
line — and that diff is the prompt to write the changelog entry and consider
whether `schema_version` must increment.

**CLI JSON**: golden files under `testdata/` for each command's `--json`
output, with volatile fields (timestamps, IDs, PIDs) normalised before
comparison. Golden files make an unintended shape change visible in a diff
rather than invisible in a passing test.

## The privacy test

Principle 5 is absolute, so it gets mechanical enforcement rather than
review discipline.

A test runs a full session and then walks every line of both logs, asserting
that **every field name appears in an explicit allowlist**. Not "assert no
password appears" — an allowlist, so that a new field added without thought
fails the test until someone confirms it is safe.

This inverts the usual burden. A denylist asks us to imagine every way private
data could leak; an allowlist requires a deliberate decision before anything
new is ever written to a log file.

Paired with it: a test asserting that raw command-line arguments never reach a
log, since that is the most plausible route for arbitrary user text to arrive.

## Concurrency tests

| Scenario | Assertion |
| --- | --- |
| Two starts, simultaneous | Exactly one succeeds; the other exits 3 with `session.start_refused` |
| `stop` while running | Session ends `user_stopped`; stopper exits 0 |
| `stop` with none running | Exit 3, nothing mutated |
| `status` during a session | Reports running; does not disturb the session |
| Many concurrent writers to the global log | Every line parses as valid JSON |

The first row is what ADR-0008 exists for, and it needs enough repetitions to
be meaningful — a race that reproduces once in fifty runs is not proven by one
run.

## Time-dependent tests

All of these use the fake clock; none of them sleep.

- Deadline reached → `completed` / `duration_elapsed`.
- Clock jumped past the deadline (simulating system sleep) → `completed`, with
  a non-zero `overrun` recorded.
- Indefinite session → no deadline, ends only on request.
- Deadline in the past at creation → usage error, not a zero-length session.

A test that calls `sleep` to wait for a timer is a test that is slow now and
flaky later.

## What we cannot test automatically

Honest gaps, which become a **manual pre-release checklist** rather than
pretending automation covers them:

- real system sleep and wake during a session,
- lid close (which we document as *not* prevented — verify it still behaves as
  documented),
- reboot during a session, then recovery on next launch,
- behaviour on battery versus AC power,
- an actual upgrade from the previous released version.

This list belongs in the release process document, which is the next piece of
Stage 0.

## CI

- **Every commit**: unit and integration tests, vet, formatting check, and a
  race-detector run. Go's race detector is worth enabling by default here —
  the session owner, the signal handler, and the mechanism watcher are
  genuinely concurrent, and races there would be intermittent in exactly the
  way that erodes trust.
- **macOS runner**: system tests including the orphan test.
- **Never**: network access. The update tests use a local server.

## Open questions

1. **Where system tests live.** Go's build tags can keep slow tests out of the
   default run, at the cost of them being easy to forget. Leaning toward a
   tag plus an explicit CI job, so "forgotten" shows up as a missing job
   rather than silence.
2. **Fuzzing the log and manifest parsers.** Go has built-in fuzzing, and the
   update manifest is untrusted input (ADR-0005) — the strongest candidate.
   Probably M9, not M1.
3. **Flake policy.** A flaky test in a trust-focused project is worse than no
   test, because it teaches people to ignore red. Quarantine or delete?
