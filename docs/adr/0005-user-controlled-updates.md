# ADR-0005 — User-controlled updates: notify, never install

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 1, 5 and 9, [MVP](../mvp.md)

## Context

An outdated security-sensitive utility is a real risk, so Awake needs an
update story. But auto-updating software is also one of the most common ways
a tool violates user trust: it changes behaviour the user did not ask for,
at a moment they did not choose, sometimes mid-task.

Ghostty's model is the reference point: tell the user an update exists, let
them decide when. Principle 1 makes this non-negotiable rather than a
preference.

## Decision

**Awake checks for updates. Awake never installs them.** The tool reports
that a newer version exists and tells the user how to get it. Installation
is always a separate, explicit user action.

**Checks are cached and interval-gated.** Results live in `update.json` with
the time of the check. No CLI command performs a network round trip if a
recent cached answer exists.

**No command ever blocks on the network.** A check that is slow,
unreachable, or offline is a logged non-event. Being on a plane must not
degrade any part of Awake.

**The check is disableable**, completely, via configuration. A user who
turns it off gets a tool with zero network activity — which is then the only
network activity Awake has ever had (principle 5).

**Severity is carried but not enforced in v0.1.0.** The manifest schema
includes `optional | recommended | required | security` so that policy can
be added later without a breaking change. v0.1.0 enforces nothing and blocks
nothing.

**The strongest action Awake will ever take is refusing to start a new
session** on a `security` release, in a future version, with a clear
explanation and a documented override path. Exit code 4 is reserved for it
now. Even then, Awake does not install anything: blocking and installing are
different powers, and Awake claims only the first.

**Awake never executes remote code.** The manifest is data — versions,
severities, release notes, URLs — and is treated as untrusted input. It is
fetched over HTTPS, its integrity is verified, and it is parsed
defensively. There is no code path where a remote document can cause
execution.

**In-progress sessions are never disturbed by an update check.** A running
session outranks update notification, always.

## Consequences

- Some users will run outdated versions indefinitely. That is the accepted
  cost of principle 1, mitigated by clear notification rather than force.
- Security response is slower than an auto-updater's. The mitigation —
  blocking new sessions on a security release — is deliberately weaker than
  the industry norm, and that trade-off is the point.
- Distribution channels matter more. A package manager (Homebrew) is a
  natural complement: it gives users a one-command update path that Awake
  does not have to implement. Packaging is a release-process concern, not an
  architectural one.
- Manifest hosting and the integrity-verification mechanism are unresolved
  and gate M9. They must be settled in their own ADR before the update
  subsystem is built.
- The update subsystem must be fully testable offline, with no live network
  in the test suite.

## Alternatives considered

**Silent auto-update.** Best security posture, standard for modern desktop
apps. Rejected outright: it is a direct violation of principle 1, and for a
tool whose entire value proposition is trust, the cure is worse than the
disease.

**Install on next launch** (the Chrome model). Less intrusive than
mid-session updates. Rejected: the user still did not choose it, and "your
binary silently changed between runs" is exactly the surprise principle 3
forbids.

**Self-update on explicit command only** (`awake update install`). Genuinely
compatible with these principles and likely a good v0.3 feature. Deferred,
not rejected: it requires binary replacement, checksum and signature
verification, and rollback on failure — a substantial subsystem that would
dominate the MVP.

**No update system at all.** Simplest, and defensible for a small utility.
Rejected: users deserve to know when a security fix exists, and silence is
its own failure of principle 9.

**Package manager only, no in-app check.** Zero network code in Awake.
Attractive, but it only serves users who installed that way and leaves
everyone else unaware. The in-app check is the floor; packaging is additive.
