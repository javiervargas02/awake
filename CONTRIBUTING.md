# Contributing to Awake

Thanks for looking. Awake is small on purpose, and its constraints are
written down — reading two short documents first will save you a rewritten
pull request:

- [Principles](docs/principles.md) — the ten constraints every change must
  satisfy. If a change conflicts with one, the change is wrong, or the
  principle must be amended explicitly via an ADR — never eroded quietly.
- [Architecture](docs/architecture/README.md) — how the pieces fit, and why.

## Ground rules

- **Interfaces are contracts.** CLI commands, flags, exit codes, config keys,
  and the JSON log schema are semver-governed public API. Breaking one
  requires a major version and an explicit callout — so most PRs should add,
  not change.
- **Core first, CLI second.** Business logic lives in `internal/` core
  packages; `internal/cli` only parses, calls one operation, and renders. A
  future GUI must never need to reimplement something you put in the CLI.
- **Platform code stays behind `internal/platform`.** No OS conditionals
  anywhere else.
- **Privacy is enforced, not promised.** New log fields must be declared in
  the logging catalogue or the allowlist test will fail your build — that is
  working as intended. Nothing about user activity is ever loggable.
- **Dependencies need a compelling case.** The project has exactly one; the
  bar for the second is high (see
  [ADR-0007](docs/adr/0007-configuration-format.md) for what that case looks
  like).
- Significant features start as an RFC (an issue is fine); architectural
  decisions get an ADR under `docs/adr/` using the
  [template](docs/adr/0000-template.md).

## The mechanics

```text
make check         # vet + formatting gate + full suite under the race detector
make test-system   # real macOS power assertions, including the orphan test
make build         # binary with version stamped in
```

`make check` must be green before a PR; `make test-system` too if you touched
the platform layer or session lifecycle. Tests use the standard library only,
and no test may ever touch the real network.

Commit messages follow the existing history's convention: explain *why*, name
the ADR or principle involved when one is, and call out anything that changes
a published contract.

## What gets declined

Features whose purpose is hiding Awake's activity from monitoring, IT, or
compliance tooling. Awake is an honest utility with an audit trail — that is
its identity, not an oversight. See the positioning constraint in
[CLAUDE.md](CLAUDE.md).
