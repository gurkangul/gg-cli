# Architecture

## Overview

```
gg (CLI, Go)
│
├── Qdrant (vector store)          localhost:6334
│   ├── <project>-decisions        gg record / gg search
│   ├── <project>-rejections       gg record --stance=reject
│   ├── <project>-tasks            gg task
│   ├── <project>-bugs             gg bug
│   ├── <project>-discussions      (legacy; CLI verbs removed in v0.3)
│   ├── <project>-notes            (legacy; CLI verbs removed in v0.3)
│   └── <project>-messages         gg tell / gg inbox
│
├── Memgraph (graph DB)            localhost:7687 (optional)
│   ├── Symbol nodes               exported functions, types, variables
│   ├── File nodes                 source files
│   └── IMPORTS / CALLS edges      dependency relationships
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
