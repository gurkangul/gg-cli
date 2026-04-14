# gg impact

Show the downstream impact of changing a source file — who depends on it, what it exports, and what project knowledge is related.

```
gg impact <file> [flags]
```

## What it reports

1. **Downstream dependents** — files that directly `import` the given file (1-hop traversal from the code graph). Requires Memgraph and `gg index` to have been run.
2. **Exported symbols** — all `Symbol` nodes for the file in the graph (functions, types, constants, variables with public visibility or boundary-crossing relevance).
3. **Related knowledge** — top-N decisions, tasks, and rejections from the Qdrant knowledge base, retrieved via semantic similarity to the file's basename and path.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--kb-limit N` | `5` | Max results per knowledge-base collection (decisions, tasks, rejections) |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | No affected dependents found (or graph not indexed) |
| `1` | One or more downstream dependents found |
| `2` | Command error (bad arguments, Qdrant/Memgraph unavailable) |

> Note: the exit code distinguishes "clean change" (0) from "change with blast radius" (1) so CI/pre-push gates can fail on impact.

## Hop depth

The current implementation uses a **1-hop direct-import** query:

```cypher
MATCH (d:File {project_id: $pid})-[:IMPORTS]->(f:File {path: $path, project_id: $pid})
RETURN d.path AS dep
```

Multi-hop traversal (`--hops N`) is planned for a future release. For now, to see 2-hop impact manually:

```sh
# Find direct dependents of foo.go
gg impact internal/foo.go

# Then run impact on each dependent
gg impact internal/bar.go   # bar.go imports foo.go
```

## Related knowledge selection

The semantic search query is the file's **basename** (e.g. `decisions.go` for `internal/store/decisions.go`). This balances precision (file-specific terms) with recall (related architectural decisions that mention the module name).

Top-5 results are returned per collection. Use `--kb-limit` to increase this.

## Graph prerequisites

Graph features (dependents, symbols) require:

1. Memgraph configured in `.gg/config.yaml` (`memgraph.uri`)
2. `gg index` run at least once for the relevant language

If Memgraph is not configured, `gg impact` degrades gracefully — the knowledge-base search still runs, and a warning is printed.

## Examples

```sh
# Check what changing the store client affects
gg impact internal/store/client.go

# JSON output for scripting
gg impact internal/store/client.go --output json

# CI pre-push gate: fail if blast radius > 0
gg impact "$CHANGED_FILE"
if [ $? -eq 1 ]; then
  echo "⚠ This change has downstream dependents — review impact output above"
  exit 1
fi
```

## See also

- [`gg check`](check.md) — pre-push gate that combines impact + linting
- [`gg context`](../commands.md#context) — full context bundle for a topic
- [`gg index`](../commands.md#index) — build the code graph
