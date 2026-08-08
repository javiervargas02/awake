# ADR-0009 — Update manifest: a static document, hosted from the repository

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 1, 5, 6 and 7,
  [ADR-0005](0005-user-controlled-updates.md)
- **Blocks:** M9 (update checking), architecture document 5

## Context

ADR-0005 settled the update *policy* — check, notify, never install — but
left two questions open, and they gate the last milestone before v0.1.0:
where the manifest lives, and how its integrity is established.

The second question is usually the hard one, and the reason it is not hard
here is worth stating plainly, because it is easy to over-engineer by
analogy with other updaters.

**Awake does not download or execute anything.** A check fetches one small
document containing a version string and a severity, compares it against the
version compiled into the binary, and prints a sentence. There is no code
path from the manifest to execution, no binary replacement, and no
installation.

That bounds the threat model to three cases:

1. **Forgery** — an attacker serves a manifest claiming a version that does
   not exist. The user is told to go and fetch something that is not there.
   Irritating; not dangerous.
2. **Suppression** — an attacker hides a security release by tampering with
   or blocking the response. This is the genuinely harmful case, and it is
   **not solvable by signing**: anyone able to tamper with the response can
   equally drop it, and a check that cannot reach the network is already
   treated as a non-event by design.
3. **Observation** — whoever serves the manifest learns that a machine at a
   given IP address checked for updates.

Signing addresses case 1 and nothing else. It becomes genuinely necessary
when self-installation arrives (v0.3), because then a manifest can point at a
binary, and that is a different decision on a different day.

## Decision

**The manifest is a static JSON document, of our own design, served over
HTTPS from the project's own repository** — GitHub Pages, published from the
repo, with the release process responsible for updating it.

**Not the GitHub Releases API.** It is rate-limited for unauthenticated
callers, its schema is GitHub's rather than ours — `severity`, which the
entire update policy turns on, would have to be smuggled into a release-notes
string — and it is an API surface we do not control. A static file has none
of those properties and is trivially cacheable and testable.

**The manifest is data and is treated as untrusted input.** It is parsed
defensively, with a response size limit and a short timeout, and no field in
it can cause execution. An unparseable manifest is a failed check, which is a
logged non-event.

**Transport security is TLS, and that is the whole integrity story for
v0.1.0.** Certificate validation is what stops case 1 from a network
attacker. There is no signature and no signing key, deliberately: a key we
cannot yet manage, rotate or revoke responsibly would be security theatre,
and the harm it would prevent is a wrong version number. This position is
documented in the README rather than left implied, and it is revisited —
necessarily — when self-installation is designed.

**The current version is never sent.** Comparison happens locally. Sending it
would let whoever serves the manifest observe version distribution across
users, which is telemetry by another name and is forbidden by principle 5.
The request carries a generic user agent identifying Awake, with no version
and no machine information.

**The manifest URL is compiled into the binary and is not configurable.** A
user-settable update URL is a supply-chain hole: anything able to edit
`config.toml` could point Awake at an attacker's manifest. Configuration may
disable the check entirely (`updates.enabled = false`) and control its
frequency; it may not redirect it. Tests inject a URL through the internal
API, not through config.

**Redirects to non-HTTPS are refused**, and redirects are bounded.

**The manifest schema is versioned and channel-shaped**, so that beta and
nightly channels are additive later:

```text
schema_version   integer
channels         map of channel name to:
                   version           the newest release on this channel
                   severity          optional | recommended | required | security
                   released          date
                   notes_url         where to read about it
```

Only `stable` exists in v0.1.0. An unknown channel in config falls back to
`stable` with a warning, per ADR-0007's per-key degradation.

## Consequences

- **No infrastructure and no recurring cost.** No domain, no server, nothing
  to keep running — which matters for a project whose maintenance burden
  should stay proportionate to a small utility.
- **The release process gains a step**: publishing a release must update the
  manifest, or Awake will not notice its own new version. This must be part
  of the release checklist, and is a good candidate for automation, because a
  manual step that is skipped produces silence rather than an error.
- **An update check reveals the user's IP address to the host.** True of
  every update check that has ever existed, and precisely why the feature is
  disableable. It is stated in the README rather than discovered.
- **Awake trusts the certificate authority system.** That is the same trust
  every HTTPS client makes, and the alternative — pinning — would break the
  moment a certificate rotated.
- **Case 2, suppression, remains unmitigated.** A user who never reaches the
  network never learns about a security release. Nothing in a notify-only
  design can fix this; the mitigation is that Awake is honest about when it
  last checked, so `doctor` and `status` can show a stale check rather than
  implying freshness.
- **Signing is deferred, not dismissed.** When self-installation is designed,
  a manifest gains the power to direct a download, and this ADR must be
  superseded before that ships.

## Alternatives considered

**The GitHub Releases API.** Zero publishing work: the release *is* the
manifest. Rejected on three counts — unauthenticated rate limiting, a schema
we do not control and cannot express `severity` in, and coupling to an API
that changes on someone else's schedule. Worth revisiting only if
maintaining a static manifest proves error-prone in practice.

**`raw.githubusercontent.com` instead of Pages.** Works with no
configuration at all, which is genuinely attractive. Rejected as the primary
choice because it is a source-code endpoint rather than a content one, with
caching behaviour that is not contractual. It is the obvious fallback if
Pages is ever unavailable, and switching is a one-constant change.

**A self-hosted manifest on our own domain.** Full control, and the only
option that avoids GitHub seeing the requests. Rejected: it costs money and
attention forever, and moves the observation from one third party to
ourselves — which is worse, not better, for a project that promises no
telemetry.

**Signing the manifest with an embedded public key.** The instinctive answer,
and it prevents forgery. Deferred: it does not address suppression, which is
the harm that matters; it introduces key generation, storage, rotation and
revocation, none of which this project can yet do responsibly; and the damage
it prevents in a notify-only design is a wrong version number. Doing it badly
would be worse than not doing it, because it would invite trust the
implementation had not earned.

**Checking against a package manager instead** (asking Homebrew what the
latest version is). Attractive for users who installed that way, and useless
for everyone else. Complementary, not a substitute — as ADR-0005 already
notes.

**No update check at all.** Simplest and most private. Rejected in ADR-0005;
this ADR does not revisit it.
