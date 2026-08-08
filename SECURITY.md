# Security policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately via
[GitHub's private vulnerability reporting](https://github.com/javiervargas02/awake/security/advisories/new)
rather than opening a public issue. You should receive an acknowledgement
within a week.

Please include what you found, how to reproduce it, and what you believe the
impact is. There is no bug bounty; there is credit in the release notes if
you want it.

## How fixes are delivered

Security fixes ship as ordinary releases, called out under a **Security**
heading in the changelog, and published in the update manifest with
`severity: security` — so `awake update check` and `awake doctor` will tell
users a security release exists and link to its notes.

**Awake never installs updates on its own** (see
[ADR-0005](docs/adr/0005-user-controlled-updates.md)). Users always install
security fixes themselves; Awake's job is to make sure they find out.

## Scope: what Awake does and does not do

Knowing the design helps judge what is and is not a vulnerability:

- Awake executes exactly one external program: `/usr/bin/caffeinate`,
  addressed by absolute path, never via `PATH` lookup.
- Awake's only network activity is fetching a static JSON manifest over
  HTTPS from a URL compiled into the binary. The manifest is treated as
  untrusted input: size-limited, parsed defensively, and incapable of causing
  execution or download. Redirects to non-HTTPS are refused. With
  `updates.enabled = false`, Awake makes no network requests at all.
- The update manifest is not signed, deliberately: since Awake never installs
  anything from it, the worst a forged manifest can do is misstate a version
  number, and signing cannot prevent the attack that matters (suppressing a
  notification). The full reasoning is in
  [ADR-0009](docs/adr/0009-update-manifest.md). This position must be
  revisited before any self-update feature ships.
- State lives in `~/.awake` with `0700`/`0600` permissions, plus one
  zero-byte lock file in a per-user temp directory.
- Logs never contain keystrokes, clipboard contents, window titles,
  application names, or user paths — enforced by an allowlist test.

Reports that Awake keeps a machine awake when asked to are working as
intended; reports that it keeps a machine awake when *not* asked to, that a
process outlives it, or that anything above is untrue are exactly what this
policy is for.
