# `gg index --changed` Contract

This document is the authoritative spec for incremental indexing.
Implementation must follow these rules exactly. Any deviation is a bug.

---

## 1. Git Ref Comparison

**Rule:** Compare the current working tree (including unstaged changes) against
the SHA recorded in `.gg/index-state.json` under the key `"last_indexed_sha"`.

```json
{ "last_indexed_sha": "abc123...", "indexed_at": "2026-04-14T13:00:00Z" }
```

**First run (no state file):** fall back to a full index (`gg index` without
`--changed`). Write `index-state.json` on success.

**How to get changed files:**
```sh
git diff --name-only <last_indexed_sha> HEAD
```
Plus any untracked files in languages the indexer knows about.

**Rationale:** `HEAD~1` would miss multi-commit batches and unstaged work.
User-specified `--base <ref>` is day-2. The stored SHA is the only approach
that is robust across arbitrary commit counts.

---

## 2. Invalidation Scope

**Rule: 1-hop invalidation.**

When file `F` is in the changed set:
1. Invalidate `F` — delete all graph nodes with `source_file = F`, re-index.
2. Invalidate direct dependents — for every file `D` that has an `IMPORTS`
   edge pointing to `F`, add `D` to the re-index set.

**Do NOT recurse further.** Full N-hop transitive closure is O(graph size) per
run and kills the "fast incremental" use case. Day-2 optimization if needed.

**Direct dependent lookup:**
```cypher
MATCH (d:File)-[:IMPORTS]->(f:File {path: $changed_path})
RETURN d.path
```

---

## 3. Deleted Symbol Reaping

**Rule: source-file-scoped delete before re-index.**

Before indexing any file in the invalidation set:
1. Delete all `Symbol` nodes where `source_file = path`.
2. Delete all `File` nodes with `path = path` (the file node itself).
3. Delete all edges that referenced any of the deleted nodes (use
   `DETACH DELETE` in Cypher — it removes orphaned edges automatically).
4. Then run the SCIP indexer and write the fresh nodes + edges.

This pattern is idempotent: running it twice produces the same graph state.
It handles all sub-cases automatically: renamed symbol, deleted function,
visibility change, file deleted entirely.

**File deleted entirely:** if the file no longer exists on disk, skip the
SCIP run after deletion. The node and all its edges are gone.

```cypher
// Step 1+2+3 combined:
MATCH (n {source_file: $path}) DETACH DELETE n;
MATCH (f:File {path: $path}) DETACH DELETE f;
```

---

## 4. State Update

After a successful incremental run:
- Read the current `HEAD` SHA: `git rev-parse HEAD`
- Overwrite `.gg/index-state.json` with the new SHA and timestamp
- On any error: **do not update state** — leave the old SHA so the next run
  retries the failed files

---

## 5. `source_file` Property Contract

Every node created by the indexer **must** carry `source_file: <absolute_path>`.
This is what makes reaping possible. Nodes without `source_file` are orphans
that can never be cleaned up — a hard bug.

The parser is responsible for setting this on every `SymbolNode` and `FileNode`
it creates. The runner must pass `root + document.relative_path` when calling
parser callbacks.

---

## 6. What Is Not In Scope (Day-1)

- N-hop transitive closure (day-2)
- Parallel invalidation of multiple files (day-2)
- `--base <ref>` flag for custom comparison point (day-2)
- Cross-project shared graph invalidation (never)
