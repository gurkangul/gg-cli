# gg brain export / import — Spec v1

> Schema version: **1** (frozen). Migration path to v2 is described at the end of this document.

This document is the authoritative reference for the `gg brain export|import|status` command family and the `.gg/brain/` on-disk layout. TASK-134 (writer) and TASK-135 (importer) implement against this spec.

---

## Motivation

gg's memory lives in two stores: Qdrant (decisions, tasks, messages, rejections, discussions, notes, bugs) and Memgraph (code graph: symbols, files, packages, edges). Both are Docker-backed and local-only. Moving a project to another machine or sharing context across team members currently requires manual container migration — error-prone and opaque.

`gg brain export` serialises both stores to git-trackable text files under `.gg/brain/`. `gg brain import` reconstructs both stores from those files on a target machine. The result is a portable, diffable brain snapshot that travels with the repo.

**Key constraints baked into this spec:**

- Git-trackable: JSONL + JSON only, no binary blobs, no tarballs.
- Deterministic: identical store state → identical file content (sort order, float encoding, key ordering all pinned).
- Payload-only: vectors are **excluded** from export. Import calls `gg reindex --embed` (or equivalent) to rebuild them. This keeps files small and avoids cross-machine vector incompatibility.
- Idempotent import: running import twice on the same `.gg/brain/` must produce identical store state.

---

## Directory layout

```
.gg/
  brain/
    manifest.json        # meta + checksums + embedding identity
    decisions.jsonl      # one JSON object per line
    tasks.jsonl
    messages.jsonl
    rejections.jsonl
    discussions.jsonl
    notes.jsonl
    bugs.jsonl
    edges.jsonl          # Memgraph relationships
    chunks.jsonl         # Memgraph nodes (Symbol, File, Package, …)
```

All files are UTF-8, LF line endings. Empty collections produce empty JSONL files (0 bytes), not missing files.

### .gitignore semantics

The following lines belong in `.gg/.gitignore` (managed by `gg init` and `gg doctor`):

```
# Runtime — never commit
cache/
*.seq
runtime/

# Brain export — commit this
# (no ignore entry for brain/)
```

`.gg/brain/` is intentionally **not** gitignored. Committing it is the entire point.

---

## manifest.json schema

```json
{
  "schema_version": 1,
  "gg_version": "0.1.0-alpha",
  "project_id": "65af2aa9-3ba2-4d31-817b-f2dd881bc199",
  "exported_at": "2026-04-17T00:00:00Z",
  "embedding_model": "nomic-embed-text",
  "embedding_dim": 768,
  "counts": {
    "decisions": 42,
    "tasks": 127,
    "messages": 91,
    "rejections": 18,
    "discussions": 6,
    "notes": 3,
    "bugs": 2,
    "edges": 1204,
    "chunks": 388
  },
  "sha256": {
    "decisions.jsonl": "e3b0c44298fc1c149afb...",
    "tasks.jsonl": "...",
    "messages.jsonl": "...",
    "rejections.jsonl": "...",
    "discussions.jsonl": "...",
    "notes.jsonl": "...",
    "bugs.jsonl": "...",
    "edges.jsonl": "...",
    "chunks.jsonl": "..."
  }
}
```

### Field definitions

| Field | Type | Notes |
|---|---|---|
| `schema_version` | int | Always `1` for this spec. Increment only on breaking changes. |
| `gg_version` | string | `go build -ldflags` version string of the exporting binary. |
| `project_id` | string | UUID of the source project. Import preserves this as `source_project_id`. |
| `exported_at` | string | RFC3339 UTC, **truncated to the second** (not wall-clock microseconds). Determinism: if the export is re-run within the same second from identical store state, `exported_at` will differ — this is acceptable. All other fields must be stable. |
| `embedding_model` | string | Model name from `embedding-meta.json`. |
| `embedding_dim` | int | Vector dimension from `embedding-meta.json`. |
| `counts` | object | Line count per JSONL file. Import MUST verify these match actual line counts before processing. |
| `sha256` | object | Lowercase hex SHA-256 of each JSONL file. Import MUST verify before processing. |

### Embedding mismatch on import

If the importing machine's configured `embedding_model` differs from `manifest.embedding_model`, import MUST abort with a clear error:

```
gg brain import: embedding model mismatch
  export used: nomic-embed-text (dim 768)
  this machine: mxbai-embed-large (dim 1024)
  Run: gg brain import --skip-embed-check   # import payload only, no vectors rebuilt
    or: configure the same model in .gg/config.yaml, then retry
```

`--skip-embed-check` is the escape hatch for deliberate cross-model migrations; the importer still writes payloads but skips the subsequent `gg reindex --embed` trigger.

---

## JSONL record format

### Qdrant collections (decisions, tasks, messages, rejections, discussions, notes, bugs)

Each line is a JSON object with exactly these top-level keys (alphabetical):

```json
{"id": "<uuid>", "payload": { ... }}
```

- `id`: Qdrant point UUID string.
- `payload`: flat or nested map — all fields from the Qdrant payload, **verbatim**. No field stripping, no renaming. Timestamp fields are preserved as-is (they are stored as strings in Qdrant already).
- Vectors are **omitted**. The `vector` key must not appear.

### Memgraph nodes (chunks.jsonl)

```json
{"id": "<domain-key>", "label": "Symbol", "properties": { ... }}
```

- `id`: **Stable domain-key composite** — machine-independent and Memgraph-restart-invariant (see below).
- `label`: node label (e.g. `"Symbol"`, `"File"`, `"Package"`).
- `properties`: node property map. The `project_id` property is included as-is (it will be rewritten to the target project ID on import).

#### Node ID construction (stable domain keys)

The `id` field is a composite of the node label and its merge keys, ensuring identical output regardless of Memgraph internal element IDs (which change across restarts and are meaningless on another machine):

| Label | Format | Example |
|---|---|---|
| `File` | `file:<path>` | `file:internal/graph/client.go` |
| `Symbol` | `symbol:<source_file>#<name>` | `symbol:cmd/main.go#Run` |
| `Package` | `package:<import_path>` | `package:github.com/foo/bar` |
| unknown | `node:<label>:<name>` | `node:Custom:singleton` |

Nodes with no usable domain key (e.g. a File missing `path`) are silently skipped during export.

### Memgraph edges (edges.jsonl)

```json
{"dst": "<domain-key>", "properties": { ... }, "src": "<domain-key>", "type": "CALLS"}
```

Keys are always alphabetical: `dst`, `properties`, `src`, `type`.

- `src` / `dst`: Stable domain keys of the endpoints (same format as `chunks.jsonl` `id`). Not Memgraph element IDs.
- `type`: relationship type string (e.g. `"CALLS"`, `"IMPORTS"`, `"DEFINES"`).
- `properties`: relationship property map (may be empty `{}`).

---

## Determinism rules

These rules ensure that identical store state always produces byte-identical JSONL output.

### Sort order

| File | Primary sort | Secondary sort |
|---|---|---|
| `decisions.jsonl` … `bugs.jsonl` | `id` (UUID string, lexicographic) | — |
| `chunks.jsonl` | `id` (element ID string, lexicographic) | — |
| `edges.jsonl` | `src` | `dst`, then `type` |

### Canonical JSON encoding

- Keys: **alphabetical** within each object at every nesting level.
- No trailing spaces.
- No pretty-printing — compact, single-line JSON per record.
- Line ending: `\n` (LF only, no CRLF).
- Float encoding: `%.6f` (six decimal places, no trailing zeros stripped). Example: `1.000000`, `0.123456`.
- Booleans and integers: standard JSON literals (`true`, `false`, `42`).
- Null values: `null` (not omitted).
- Strings: standard JSON escaping. No HTML-safe encoding (`<`, `>`, `&` are NOT escaped to `\u003c` etc.).

The Go implementation must use a custom encoder that enforces alphabetical key order and `%.6f` float encoding. The standard `encoding/json` marshaller does not guarantee key order — use `sort.Strings` on map keys before serialisation, or a dedicated canonical-JSON library.

### Timestamp handling

Payload timestamp fields (e.g. `created_at`, `updated_at`) are stored as strings in Qdrant and are exported verbatim. No truncation or normalisation is applied during export. This preserves exact round-trip fidelity.

`manifest.json`'s `exported_at` field IS normalised (truncated to second) to reduce noise in git diffs.

---

## Import semantics

### Qdrant

- Call `EnsureCollections` before any upserts.
- Upsert each record by `id` — existing records with the same UUID are overwritten.
- Do NOT upsert vectors (they are not in the file). Leave the vector slot empty; the subsequent `gg reindex --embed` run will populate them.
- After all collections are written, verify row counts match `manifest.counts`.

### Memgraph

- Import is a full-replace per project: sweep all existing nodes/edges for `project_id` first (`SweepProject`), then insert from `chunks.jsonl` and `edges.jsonl`.
- Rewrite `project_id` property on every node to the **target** project's ID (not the source's).
- Node IDs in `chunks.jsonl` are stable domain keys — no `oldID → newID` element-ID translation is required. Edge endpoints in `edges.jsonl` use the same domain keys and are resolved directly via MATCH on merge properties.
- If `chunks.jsonl` is empty, skip the Memgraph import step entirely (graph may not exist on source).

### Idempotency

Running `gg brain import` twice from the same `.gg/brain/` must produce the same result:

- Qdrant: upsert is naturally idempotent.
- Memgraph: sweep-then-insert is idempotent (same result each run).
- `manifest.json` checksum verification runs on every import invocation.

---

## gg brain status

`gg brain status` reads `manifest.json` (if present) and reports:

```
Brain snapshot: present
  Exported at:  2026-04-17T16:14:38Z
  gg version:   0.1.0-alpha
  Embedding:    nomic-embed-text (dim 768)
  Counts:       42 decisions · 127 tasks · 91 messages · 388 chunks · 1204 edges
  Checksums:    ✓ all match
```

If `.gg/brain/manifest.json` does not exist:

```
Brain snapshot: none  (run: gg brain export)
```

---

## Schema versioning and migration path

`schema_version: 1` is frozen by this document. Future breaking changes increment to `2` and require a migration note here.

**v1 → v2 migration path (when needed):**

- Add `"schema_version": 2` to `manifest.json`.
- Export tool writes both v1 and v2 formats side-by-side during a transition window, then drops v1.
- Import tool checks `schema_version` and routes to the appropriate reader. If the version is unknown, abort with:
  ```
  gg brain import: unsupported schema version 2 (this binary supports up to v1)
  Update gg to the latest version.
  ```

No migration tooling is planned for v1 → v2 at this time — it will be designed when a concrete breaking change is needed.

---

## Command reference (provisional)

```
gg brain export              # write .gg/brain/ from current Qdrant + Memgraph state
gg brain import              # read .gg/brain/, upsert into local stores, then trigger reindex
gg brain status              # show snapshot metadata and checksum status

# Flags
gg brain export --dry-run    # print what would be written, don't write
gg brain import --skip-embed-check   # bypass embedding model mismatch check
gg brain import --no-reindex         # import payloads, skip gg reindex --embed trigger
```

---

## Related tasks

- **TASK-134** — `gg brain export`: deterministic JSONL writer implementation
- **TASK-135** — `gg brain import`: upsert Qdrant + Memgraph from `.gg/brain/` (idempotent)
- **TASK-136** — Auto re-embed on brain import: regenerate vectors when missing
- **TASK-137** — Secrets scrubber for `gg brain export` (`--strict` + regex redact)
- **TASK-129** — `gg import --from-gsd` (separate namespace, no conflict with `gg brain import`)
