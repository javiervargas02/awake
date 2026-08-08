# Update manifest

`manifest.json` is the document Awake fetches to learn whether a newer release
exists. It is served by GitHub Pages at:

```text
https://javiervargas02.github.io/awake/updates/manifest.json
```

It is plain data. Awake reads a version string and a severity, compares the
version locally, and prints a sentence. Nothing in this file can cause Awake to
download or execute anything — see
[ADR-0009](../docs/adr/0009-update-manifest.md).

## Publishing a release means updating this file

**If this file is not updated, Awake will not notice the new release — and the
failure is silence, not an error.** That is why it belongs on the release
checklist rather than in someone's memory.

```json
{
  "schema_version": 1,
  "channels": {
    "stable": {
      "version": "0.1.0",
      "severity": "optional",
      "released": "2026-08-07",
      "notes_url": "https://github.com/javiervargas02/awake/releases/tag/v0.1.0"
    }
  }
}
```

| Field | Meaning |
| --- | --- |
| `version` | the newest release on this channel, semantic and without a leading `v` |
| `severity` | `optional`, `recommended`, `required` or `security` |
| `released` | the release date, for humans reading this file |
| `notes_url` | where to read about the release |

`severity` is carried and displayed in v0.1.0 but enforces nothing. Blocking
new sessions on a `security` release is reserved for v0.2.

Only the `stable` channel exists. Adding `beta` or `nightly` later is additive:
older binaries that do not know a channel fall back to `stable`.
