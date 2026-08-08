# Architecture — Update checking

> Status: Ratified 2026-08-07.
>
> Implements [ADR-0005](../adr/0005-user-controlled-updates.md) (notify, never
> install) and [ADR-0009](../adr/0009-update-manifest.md) (a static manifest).
> Depends on [Overview](overview.md) and [State and repair](state-and-repair.md).

## What this subsystem may and may not do

It may fetch one document, compare two version strings, cache the answer, and
report it. That is the entire scope, and the boundary is worth stating as a
prohibition rather than a description:

- It never downloads a binary.
- It never writes anything outside `~/.awake/update.json`.
- It never blocks a command, a session, or the program's exit.
- It never sends anything about the machine or the user.

## The flow

```text
awake update check
      │
      ├── updates disabled in config? ──────────── report "disabled", stop
      │
      ├── cache fresh (checked within interval)? ─ report the cached answer
      │        (--force skips this)
      │
      ├── fetch manifest over HTTPS ────────────── failure? log at warn, stop
      │
      ├── parse defensively ───────────────────── unparseable? log at warn, stop
      │
      ├── compare against the compiled-in version
      │
      └── write the cache, report the result
```

Every exit path is a normal outcome. There is no branch in this diagram that
produces an error the user has to act on, which is why `awake update check`
exits `0` even when the network is unreachable: being offline is not a defect
in Awake.

## Fetching

The request is deliberately unremarkable:

| Property | Value | Why |
| --- | --- | --- |
| URL | compiled into the binary | not configurable; a settable URL is a supply-chain hole |
| Method | GET | there is nothing to send |
| Timeout | 5 seconds | a check must never feel like a hang |
| Body limit | 64 KiB | a hostile or broken host cannot exhaust memory |
| Redirects | bounded, HTTPS only | a redirect to plain HTTP is refused, not followed |
| User agent | `awake` | identifies the client, carries no version |

**The current version is never transmitted.** Comparison is local. Sending it
would let the host observe version distribution across users — telemetry by
another name, and forbidden by principle 5.

## Parsing

The manifest is untrusted input and is treated as such: unknown fields are
ignored, a missing channel is not a crash, and no field can influence
anything but a printed sentence.

```text
schema_version   integer
channels
  stable
    version      "0.2.0"
    severity     optional | recommended | required | security
    released     "2026-08-07"
    notes_url    "https://github.com/…/releases/tag/v0.2.0"
```

A `schema_version` newer than we understand is not an error either: Awake
reports that it cannot interpret the manifest and carries on. Refusing to run
because a *notification* format changed would be absurd.

## Comparing versions

Versions are semantic: `major.minor.patch`, with an optional pre-release
suffix that sorts *before* the same version without one.

Two cases need naming because they are normal rather than exceptional:

**The running binary has no comparable version.** Development builds are
stamped from `git describe` and look like `5c0b3ad-dirty`. There is nothing
to compare, so the result is `unknown` and the user is told they are on a
development build. Guessing would be worse than saying so.

**The running binary is newer than the manifest.** Someone built from source,
or the manifest has not caught up with a release. This is reported as up to
date, not as an anomaly.

## The cache

`update.json` holds the last answer and when it was obtained. It is a cache
in the strict sense: nothing depends on it, and discarding it costs one
network request.

```text
version          cache format version
channel          which channel was checked
checked_at       when
result           up_to_date | update_available | failed | unknown
latest_version   what the manifest said
severity         what the manifest said
```

**Freshness is decided by `checked_at` plus `updates.check_interval`.** A
check inside that window returns the cached answer without touching the
network, which is what stops a CLI command from ever waiting on a round trip.
`--force` ignores the window.

A corrupt cache is a `doctor` problem and a `repair` action — discard it —
because a cache that cannot be read is simply a cache that has not been
populated.

**The cache records failures too.** Otherwise an offline machine would retry
on every single command, which is both wasteful and the opposite of what the
interval is for.

## What v0.1.0 deliberately does not do

- **It does not check automatically.** No background check, no check on start,
  no check woven into unrelated commands. `awake update check` is the only
  thing that reaches the network. `notify_on_start` is a v0.2 question, and it
  is a user-visible behaviour change, not a detail.
- **It does not enforce severity.** The field is carried, stored and
  displayed; no policy acts on it. Blocking new sessions on a `security`
  release is reserved for v0.2, and exit code 4 is reserved with it.
- **It does not install.** ADR-0005, permanently for this line.

Carrying severity without enforcing it is deliberate: the schema is the part
that is expensive to change later, and the policy is the part that needs a
real release to have been made before it means anything.

## Failure behaviour

| Failure | Behaviour |
| --- | --- |
| Network unreachable, DNS failure, timeout | `update.check.completed` at `warn`, result `failed`, exit 0 |
| Non-200 response | as above |
| Body too large, or unparseable | as above |
| `schema_version` newer than known | result `unknown`, exit 0 |
| Cache unwritable | the result is still reported; the failure is logged |
| Updates disabled in config | no request is made at all |

The last row is a promise, not an implementation detail: with
`updates.enabled = false`, Awake makes **no network requests of any kind**,
which is what makes principle 5's "the only network activity" claim
verifiable rather than aspirational.

## Testing

Every test uses a local HTTP test server. **No test touches the real
network** — a suite that fails on a plane is a broken suite.

The cases that matter: a manifest offering a newer version, an older one, an
identical one, an unparseable body, a body over the size limit, a non-200
status, a timeout, a redirect to plain HTTP, an unknown `schema_version`, a
development version string, a fresh cache preventing a request, `--force`
overriding it, and `updates.enabled = false` making no request at all. The
last is asserted by failing the test if the server is contacted.

## Release obligation

Publishing a release must update the manifest, or Awake will not notice its
own new version — and the failure mode is **silence**, not an error. This
belongs on the release checklist and is a strong candidate for automation.

`doctor` reports when the last check happened, so a stale answer looks stale
rather than implying freshness. That is the only available mitigation for the
suppression case ADR-0009 leaves open.
