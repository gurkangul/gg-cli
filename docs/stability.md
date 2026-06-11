# Stability & Versioning Policy

> **Status:** **In effect as of 1.0.0.** The guarantees below are binding from
> the 1.0.0 release onward. The companion [1.0 readiness audit](./1.0-readiness.md)
> records how each guarantee was met and which items were explicitly deferred
> past 1.0.

This document defines what gg promises to keep stable, how versions map to
change scope, how commands are tiered, and how deprecations are handled.

---

## 1. SemVer mapping

gg follows [Semantic Versioning](https://semver.org/). As of **1.0.0**:

| Bump | Meaning |
|------|---------|
| **MAJOR** (`2.0.0`) | A breaking change to a **stable** command/flag contract, or a storage format change that is **not** covered by an automatic forward-compatible migration. |
| **MINOR** (`1.1.0`) | Additive, backward-compatible change: new commands, new flags, new optional config keys, new storage fields that older binaries ignore. |
| **PATCH** (`1.0.1`) | Bug fix or doc change with no contract or schema impact. |

**History.** Before `1.0.0` (the `0.x` series) this mapping was aspirational and
minor `0.x` bumps could contain breaking changes. From `1.0.0` onward the mapping
is binding for the **stable** tier; experimental commands remain exempt (§2).

---

## 2. Command stability tiers

Every user-reachable command belongs to exactly one tier. The current
classification lives in the [command-surface audit](./1.0-readiness.md#command-surface-audit-ac-2).

### Stable
The core contract. Command name, subcommand names, the meaning of documented
flags, exit-code semantics, and the shape of `--json` output are **frozen**.

- A breaking change to a stable command requires a **MAJOR** bump **and** a
  deprecation cycle (§4): the old behaviour keeps working, with a stderr warning,
  until the next major.
- New optional flags and additive `--json` fields are allowed in a MINOR (they do
  not break existing callers).

### Experimental
Marked as such in `--help`/docs. May change shape, flags, or output — or be
removed — in a **MINOR** without a deprecation cycle. Use in scripts at your own
risk. Debug/observability scaffolding (e.g. trace inspection, audit/metrics
reporting) starts here until it has earned a stable contract.

### Internal (hidden)
Registered with cobra `Hidden: true`. Not listed in `gg --help`, not documented
in `docs/cli/`, and **not** part of any public contract. These are invoked by gg's
own hooks/integrations and may change or disappear at any time, including in a
PATCH. Example today: `gg gsd-guard` (the Claude Code PreToolUse guard).

---

## 3. Storage forward-only compatibility

gg persists shared memory on disk and in local services. The 1.0 guarantee is
**forward-only readability**:

> An existing project store (`.gg/` in the repo and the host-level `~/.gg/`
> registry/runtime) written by gg version *N* stays readable by any gg version
> *≥ N* within the same MAJOR. Newer binaries never refuse to load an older,
> valid store.

How this is honoured:

- **Additive fields.** New payload/record fields are added as optional. Older
  binaries ignore unknown fields; newer binaries tolerate their absence. This is
  a MINOR change.
- **Format changes ship a migration.** When a format genuinely changes shape
  (dimension change, re-keying, re-embedding), the new binary carries an
  automatic, idempotent migration rather than breaking the old store. The
  reference implementation is the Qdrant re-embed migration in
  [`internal/store/reembed.go`](../internal/store/reembed.go) (`ReembedAll`):
  it reads every existing point (overlaying JSONL as the source of truth so
  JSONL-only/mutated entries are not lost), drops and recreates collections at
  the new vector size, and re-inserts — retry-safe if interrupted. New
  migrations should follow this read-all → recreate → re-insert, idempotent,
  JSONL-authoritative pattern.
- **Versioned snapshots.** The portable brain snapshot already carries an explicit
  `schema_version` in its `manifest.json` (see [docs/brain-export.md](./brain-export.md));
  a bump there triggers an import-time migration, not a hard failure.

A storage change that *cannot* be migrated forward automatically is a **MAJOR**
break and must be called out in the CHANGELOG.

---

## 4. Deprecation policy

When a stable command, subcommand, or flag is on its way out:

1. **Mark it.** It is announced as deprecated in `--help`/docs with the
   replacement spelled out.
2. **Warn, don't break.** Invoking it still works, but prints a one-line warning
   to **stderr** (never stdout, so pipelines and `--json` consumers are
   unaffected) naming the replacement.
3. **Survive until the next major.** A command/flag deprecated in version *X*
   keeps working — with the warning — until at least the next **MAJOR**. Removal
   only happens on a major bump.
4. **Log it.** Every deprecation and every eventual removal is recorded in
   [CHANGELOG.md](../CHANGELOG.md).

Today two commands already follow the warn-don't-break shape ad hoc:
`gg decide` → `gg record`, and `gg reject` → `gg record --decision-status=rejected`
(each prints a stderr warning). There is **no shared deprecation helper or cobra
`Deprecated:` usage yet** — see the
[deprecation mechanism assessment](./1.0-readiness.md#deprecation-mechanism-assessment-ac-5)
for the current state and the filed follow-up.

---

## 5. config.yaml additive-keys rule

`.gg/config.yaml` is loaded with a lenient YAML decode
([`internal/config/linked_projects.go` `LoadFromGGDir`](../internal/config/linked_projects.go)):

- **New keys are additive.** Adding an optional key is a MINOR change; older
  binaries ignore keys they don't recognise.
- **Removed/renamed keys warn, don't error.** A key that gg no longer reads is
  silently ignored today (unknown keys do not fail the load). Under the 1.0
  contract, a *removed* key should emit a deprecation warning (§4) rather than
  silently doing nothing, and must never hard-fail an otherwise valid config.
- **Defaults fill gaps.** `ApplyDefaults()` supplies values for absent keys, so a
  minimal config from an older gg still loads.

Renaming a config key without a backward-compatible alias + warning is a
**MAJOR** break.

---

## See also

- [1.0 readiness audit & punch-list](./1.0-readiness.md)
- [Portable brain snapshot format (schema_version)](./brain-export.md)
- [Architecture overview](./architecture.md)
- [CHANGELOG](../CHANGELOG.md)
