# ADR-0007 — Configuration format: TOML, and the first dependency

- **Status:** Accepted (ratified 2026-08-07)
- **Date:** 2026-08-07
- **Relates to:** principles 3, 4 and 7, [ADR-0003](0003-local-first-recoverable-state.md), [MVP](../mvp.md)
- **Blocks:** M3 (file store)

## Context

Awake needs a configuration file. The MVP surface is deliberately tiny —
three keys — but the format decision outlives the MVP, gets baked into the
file store, and becomes public API under principle 8 the moment it ships.

Four pressures apply, and they pull against each other:

1. **Principle 7 prefers the standard library.** Go's standard library
   includes exactly one config-shaped format: JSON. Everything else is a
   dependency, and this would be Awake's first.
2. **The file is written for humans and edited by hand.** It is not an
   interchange format; it is a small document a user opens in an editor,
   occasionally, months apart.
3. **Recovery regenerates it** (ADR-0003). A file Awake recreates from
   defaults is also a teaching surface — the best moment to explain what the
   keys mean is inside the file itself.
4. **Predictable over clever** (principle 3). A config format with parsing
   surprises undermines the property the whole project is selling.

Pressure 1 says JSON. Pressures 2 through 4 say a format JSON cannot
provide, because **JSON has no comments** — and a regenerated config that
cannot explain itself is a missed obligation under principle 3, not merely
an inconvenience.

## Decision

**Configuration is TOML.** The file is `~/.awake/config.toml`.

TOML is chosen over the alternatives because it is the only widely-supported
option that is simultaneously commentable, unambiguous, and boring: no
significant whitespace, no implicit type coercion, no value that silently
parses as something other than what it looks like. It was designed for
exactly this job — small, hand-edited application config — and its 1.0 spec
is stable and unlikely to move under us.

The following rules are binding:

**Configuration expresses preference, never fact.** Restating ADR-0003:
nothing authoritative about the program lives here. Config cannot change
what version Awake is, what a session did, or what the logs say.

**The file is optional.** Its absence is a normal state, not a fault. Awake
runs entirely on built-in defaults with no config file present, and
`doctor` reports that as healthy.

**Awake never edits the user's config file.** It creates the file when none
exists; it never rewrites, reformats, or migrates one the user owns. This
guarantees a hand-edited file with hand-written comments is never mangled by
the tool — and it means we never need comment-preserving serialization.

**A generated config is self-documenting.** When Awake creates the file, it
writes the defaults with explanatory comments: what each key does, its
accepted values, and its default.

**Invalid input degrades per key, never aborts.** An unparseable file falls
back to defaults entirely (and is set aside per ADR-0003); an invalid
*value* falls back for that key alone. Every fallback emits
`config.defaulted` naming the key and the reason. A bad config never
prevents a session from starting.

**Unknown keys are a warning, not an error.** A user running an older binary
against a newer config, or with a typo, gets a diagnosable warning from
`doctor` and a working tool.

**Precedence is: built-in defaults → config file → command-line flags.**
Environment variables are deliberately not a source in v0.1.0; adding a
layer later is additive, removing one is breaking.

**The parser library is an implementation detail, isolated behind the config
package.** No other package imports it, and no core type is defined by it.
Swapping the library is not a breaking change; changing the *format* is.

**The library is `BurntSushi/toml`**, chosen on these criteria: zero
transitive dependencies, a stable and unexcited release history, TOML 1.0
compliance, and a codebase small enough to audit. `pelletier/go-toml/v2` was
the alternative on performance grounds — irrelevant for a three-key file read
once per command.

**Migration across major versions is explicit, never automatic.** Two rules
above make this cheap: unknown keys warn rather than fail, and invalid values
degrade per key. An older config against a newer binary therefore keeps
working, so there is no forced-migration moment. If a genuine key rename ever
lands in a major version, the escape hatch is a user-invoked
`awake config migrate` that shows what it would change and requires the user
to run it — never a rewrite that happens on its own. Until the config surface
is large enough to warrant that command, `doctor` naming the affected key is
sufficient.

**This dependency is reviewed at each release** as part of the release
checklist, not adopted and forgotten.

## Consequences

- Awake acquires its first external dependency. That is a real cost under
  principle 7 and should be stated plainly in the README rather than
  discovered in `go.mod`.
- The dependency's blast radius is bounded: one package, one direction, no
  core types touched. If the library is abandoned, the replacement is a
  contained change — or, at the limit, a small hand-written parser, since
  the subset of TOML we use is trivial.
- `config.yaml` becomes `config.toml` in the MVP and in ADR-0003. Both
  documents are updated; neither had been ratified.
- "Never edit the user's file" means config migrations across major versions
  cannot be automatic. This is a real constraint on future config changes,
  but a mild one: because unknown keys warn and invalid values degrade, an
  outdated config never breaks a working install, so migration is advisory
  rather than urgent.
- Generated-config comments are user-facing documentation and must be kept
  accurate as keys change — a maintenance obligation, and one worth having.

## Alternatives considered

**JSON, from the standard library.** Zero dependencies, and the only option
that fully satisfies principle 7 on its own terms. Rejected: no comments, so
a regenerated file cannot explain itself; no trailing commas and strict
quoting, so hand-editing is error-prone in exactly the way that generates
avoidable recovery events. JSON is an excellent interchange format — which
is why the *logs* use it — and a poor hand-edited config format. Choosing it
would satisfy the letter of principle 7 while failing principle 3.

**YAML.** The most familiar option, and the format assumed in earlier drafts
of the MVP. Rejected on predictability grounds: significant whitespace makes
hand-editing fragile, and its implicit typing is genuinely surprising —
unquoted values that look like one type parsing as another is a well-known
class of YAML bug. For a project whose selling point is "you can predict
what this will do," shipping a config format with famous footguns is the
wrong trade. Separately, the maintenance status of the canonical Go YAML
package should be verified before anyone reconsiders this — it is not a
settled ecosystem the way TOML's is.

**A hand-written `key = value` parser, no dependency.** Tempting: our config
is three keys, and this keeps the dependency count at zero. Rejected — but
it is the closest call here. We would own a parser forever, and every
sharp edge (quoting, escaping, comments, nesting when a fourth key needs
structure) would be rediscovered badly. If the TOML dependency ever becomes
untenable, this is the fallback, and the "never edit the user's file" rule
keeps that door open.

**No config file in v0.1.0 — flags only.** Genuinely attractive, and it
would simplify M3. Rejected for two reasons: update-check preferences are
inherently persistent (a per-invocation flag cannot express "check daily"),
and the config load-and-recover path is one of the architectural behaviours
the MVP exists to prove. Deferring it would move the risk, not remove it.

**Environment variables as a config source.** Rejected for v0.1.0, not
forever: a precedence layer is easy to add and impossible to remove without
breaking someone. Revisit when there is a concrete need, likely containers
or CI.
