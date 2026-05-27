# Architecture

## Overview

```
gg (CLI, Go)
│
├── Qdrant (vector store)          localhost:6334
│   ├── <project>-decisions        gg record / gg search
│   ├── <project>-rejections       gg record --decision-status=rejected
│   ├── <project>-tasks            gg task
│   ├── <project>-bugs             gg bug
│   ├── <project>-discussions      (legacy; CLI verbs removed in v0.3)
│   ├── <project>-notes            (legacy; CLI verbs removed in v0.3)
│   └── <project>-messages         gg tell / gg inbox
│
├── Memgraph (graph DB)            localhost:7687 (optional)
│   ├── Symbol nodes               exported functions, types, variables
│   ├── File nodes                 source files
│   └── IMPORTS edges              dependency relationships
│
└── Ollama (embeddings)            localhost:11434
    └── nomic-embed-text (768-dim) semantic search vectors
```

## Directory layout

```
~/.gg/                             shared infra (one per machine)
├── docker-compose.yaml
└── projects/
    └── <project_id>/              per-project runtime state (never committed)
        ├── telemetry.jsonl
        └── cache/
            └── search-lkg/

<project_root>/
└── .gg/                           per-project metadata (committed to git)
    ├── config.yaml                project_id + service endpoints
    ├── RULES.md
    ├── AGENTS.md (optional)
    ├── .gitignore                 excludes runtime state if written locally
    └── outbox/                    crash-safety queue for index writes
```

`gg doctor --heal` migrates any legacy `.gg/telemetry.jsonl` or `.gg/cache/` entries to the runtime dir.

## Project isolation

Every Qdrant point and every Memgraph node carries a `project_id` field. Multiple projects can share the same backend without data leakage. The project ID is set in `.gg/config.yaml` and injected automatically by all store/graph writes.

## Code packages

| Package | Responsibility |
|---|---|
| `cmd/` | Cobra commands — thin handlers that delegate to internal packages |
| `internal/store/` | Qdrant client — decisions, tasks, bugs, notes, discussions, messages |
| `internal/graph/` | Memgraph client — code knowledge graph via Bolt protocol |
| `internal/embedding/` | Ollama HTTP client — generates float32 vectors |
| `internal/index/` | SCIP pipeline — runner, parser, changed-file detection, version compat |
| `internal/outbox/` | Crash-safety queue for Memgraph writes |
| `internal/config/` | Config loading, project root detection |

## Crash safety

`gg index` writes to both Qdrant (via store) and Memgraph (via graph). To survive crashes mid-write:

1. An outbox entry is written to `.gg/outbox/<id>.json` before the Memgraph write.
2. On success the entry is deleted.
3. `gg doctor --reconcile` surfaces any surviving entries and shows the repair command.

All Memgraph writes use `MERGE` semantics (`UpsertNode`, `UpsertEdge`) so replay is safe.

## CodeGraph freshness contract

Memgraph is a derived CodeGraph projection, not a background service owned by
gg. Successful index runs update `.gg/index-state.json` with per-language SHAs
and working-tree fingerprints. `gg session-start`, `gg next`, `gg impact`,
`gg doctor`, and `gg index status` all render the same shared freshness
contract: status (`ready`, `missing`, `stale`, `unavailable`, `unknown`,
`not_applicable`), reason (`missing_graph`, `empty_graph`,
`memgraph_unavailable`, `language_missing`, `non_ancestor`, `changed_files`,
`module_manifest_changed`, `fingerprint_mismatch`, `not_applicable`,
`unknown`), counts, repair command, and foreground-watch hint.

Freshness tracks source files plus selected module manifests only:
`go.mod` for Go; `package.json`, `tsconfig.json`, and `jsconfig.json` for
TypeScript/JavaScript; `pyproject.toml`, `setup.py`, `setup.cfg`, `Pipfile`,
and `requirements.txt` for Python; and `Package.swift`, Xcode project files,
and Xcode workspace files for Swift. Dependency lockfiles such as `go.sum`,
`package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`, `poetry.lock`, and `uv.lock`
are deliberately excluded from CodeGraph freshness because lockfile-only
version churn does not change the source symbol/import graph. If dependency
upgrades require a fresh graph, run `gg doctor --fix-index` explicitly after the
upgrade.

Swift support is recognition plus externally-backed SCIP ingestion. gg detects
SwiftPM packages, Xcode projects/workspaces, and `.swift` source files, but it
does not bundle a native Swift SCIP generator. A compatible `scip-swift` binary
must accept `scip-swift index --output <file> <project-root>` and write a valid
SCIP file. It must exit non-zero on failure and emit document paths either
relative to `<project-root>` or absolute paths under the gg project root.

There is no automatic background indexing daemon. The canonical one-shot repair
path is `gg doctor --fix-index` (or an explicit `gg index --lang <lang>` when the
operator knows the target language). Active refresh is opt-in only:
`gg index --watch` and `gg watch --index` start a foreground watcher, hold a
project-local lock, debounce source/module changes, and stop when the operator
presses Ctrl-C.

Commands that support JSON include the same additive `codegraph` object with
`background_refresh: false`, `foreground_watch_available`, and a language-aware
`foreground_watch_command` when gg can infer the language safely.

## Embedding model metadata

`.gg/embedding-meta.json` records the model name and dimension used when the collections were created. If the configured model changes, `gg` fails fast with an error rather than silently writing incompatible vectors. Use `gg reembed --confirm` to migrate.

## Security

All Cypher queries go through `graph.runQuery` / `graph.runQueryNoPID` in `internal/graph/queries.go`. Direct `sess.Run` calls outside the `graph` package are forbidden — this single choke-point makes project-isolation auditable.

## Multi-agent model

```
Agent A              Agent B              Agent C
  │                    │                    │
  ▼                    ▼                    ▼
gg record           gg task create       gg search
  │                    │                    │
  └────────────────────┴────────────────────┘
                        │
                   Qdrant + Memgraph
                   (shared backend)
```

No central coordinator. No daemon. Each agent is a subprocess that reads/writes the shared store via the `gg` CLI.
