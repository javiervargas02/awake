# Release process

> Status: Ratified 2026-08-07.

This is the checklist for publishing a release. It is written to be followed
top to bottom, and the steps that protect users are marked **required** —
skipping one is a decision, not an oversight.

## Versioning

Semantic: `major.minor.patch`.

| Change | Version |
| --- | --- |
| Bug fixes, doc fixes, non-breaking improvements | patch |
| New commands, flags, modes, config keys, log events | minor |
| Anything that breaks a published contract | major |

The published contracts are: CLI commands and flags, exit codes, config keys,
the JSON log schema (`schema_version`), and the `--json` output shapes.
Human-readable text is not a contract and may change freely.

## Before tagging

1. **`make check` is green** — vet, formatting, and the full suite under the
   race detector. *(required)*
2. **`make test-system` is green on real macOS** — including the orphan test:
   SIGKILL Awake mid-session and confirm nothing survives. **If this is
   skipped, the release does not ship.** An orphaned assertion is the worst
   bug this project can produce. *(required)*
3. **The manual checklist below has been run on a real machine.** *(required
   for minor and major releases; patch releases may skip items unrelated to
   the fix)*
4. **CHANGELOG.md is updated**: the `[Unreleased]` section becomes the new
   version with today's date, entries are meaningful, and breaking changes
   are called out explicitly. *(required)*
5. Contract tests still describe reality: if the release adds events or JSON
   fields, the schema tests were extended deliberately, not just made to
   pass.

## Manual pre-release checklist

Automation cannot cover these (see the
[testing strategy](architecture/testing.md)); a human runs them:

- [ ] Start a session, close the laptop lid: the machine sleeps (documented
      behaviour), and on wake the session state and logs are coherent.
- [ ] Start a session, let the machine actually idle past the point it would
      normally sleep: it stays awake.
- [ ] Start a session, put the machine to sleep from the Apple menu, wake it
      after the deadline: the session is `completed` with a non-zero
      `overrun` in its trace.
- [ ] Reboot mid-session: the next command reports the crashed session and
      recovers it.
- [ ] Upgrade from the previous released version: old state files are read
      correctly, `doctor` is clean.
- [ ] `awake update check` against the live manifest behaves correctly both
      before and after the manifest update (below).

## Publishing

6. **Tag and push**: `git tag v0.X.Y && git push origin v0.X.Y`.
7. **Build release binaries** for `darwin/arm64` and `darwin/amd64` with
   `make build` (the Makefile stamps the version from the tag). Name them
   `awake-v0.X.Y-darwin-arm64` and `awake-v0.X.Y-darwin-amd64`, and record
   their SHA-256 checksums in the release notes.
8. **Create the GitHub Release** from the tag. The notes are the changelog
   section for this version, plus the checksums.
9. **Update `updates/manifest.json`** — version, severity, release date, and
   the release-notes URL — and push it. **This step is required and easy to
   forget: if the manifest is not updated, existing installs will never learn
   this release exists, and the failure is silence, not an error.**
   *(required)*
10. **Verify the loop closes**: wait for GitHub Pages to deploy, then run the
    *previous* release's binary — `awake update check --force` must report
    this new version and link its notes. *(required)*

## Severity

Choose the manifest `severity` honestly:

| Severity | Meaning |
| --- | --- |
| `optional` | nice to have |
| `recommended` | most users should take it |
| `required` | something is materially broken without it |
| `security` | fixes a vulnerability — also gets a Security changelog section |

v0.1.x carries severity as information only; nothing is enforced. That is by
design (ADR-0005) and changes only with a deliberate v0.2 decision.

## After publishing

- Close or update anything the release resolves.
- If the release was `security`: the advisory (if any) is published, and the
  changelog Security section links it.
