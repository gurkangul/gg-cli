# gg — One brain, any agent

[![CI](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/gurkangul/gg-cli)](https://github.com/gurkangul/gg-cli/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gurkangul/gg-cli)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Alpha](https://img.shields.io/badge/status-alpha-orange)](https://github.com/gurkangul/gg-cli/releases)

> **Status: Alpha.** API and storage format may change between releases.
> Suitable for personal projects and early adopters. Not recommended for
> production multi-team environments yet.

**gg** is a shared knowledge base CLI for multi-agent AI development setups.

When you run several AI agents in parallel (different terminals, different roles), each one starts with a blank slate. gg fixes the three failure modes that follow:

| Problem | What gg does |
|---|---|
| Every agent re-derives the same context | Shared decisions, rejections, and notes — searchable by all agents |
| Impact-blind fixes create fix loops | `gg index` builds a code graph; `gg context` surfaces what a change touches |
| Rejected approaches get retried | Rejections are first-class records, returned by `gg search` |

**One brain. Any agent. Subprocess interface — no daemon, no REST.**

---

## Why this exists

Running multiple AI agents in parallel is increasingly common — different terminals, different specializations, different tasks. The problem is that each agent starts with a blank slate every time. Three failure modes follow reliably:

1. **Every agent re-derives the same context.** Agent A figures out the auth approach; Agent B spends 10 minutes reaching the same conclusion. Multiply by n agents and n sessions.
2. **Impact-blind fixes create fix loops.** An agent patches a symptom without knowing what else calls the function. The next agent sees the symptom again and patches it again.
3. **Rejected approaches keep getting re-proposed.** "What about using Redis for the session store?" — answered three times across three sessions because nothing remembered the rejection.

gg is a shared brain. Agents call it as a subprocess. Decisions, rejections, tasks, and code-graph facts live in a local vector store that all agents query. A decision made by one is immediately visible to the others.

---

## Prerequisites

**Required:** [Docker](https://docs.docker.com/get-docker/) with Compose v2.
`gg init` runs the three services below as containers via a shared
`~/.gg/docker-compose.yaml` — no manual install needed for Qdrant /
Memgraph / Ollama.

| Service | Purpose | Default |
|---|---|---|
| [Qdrant](https://qdrant.tech) | Vector store (decisions, tasks, notes, bugs, …) | `localhost:6334` |
| [Ollama](https://ollama.ai) | Local embeddings | `http://localhost:11434` |
| [Memgraph](https://memgraph.com) | Code knowledge graph (optional, for `gg index`) | `bolt://localhost:7687` |

Embedding model: `nomic-embed-text` (768-dim) — `gg init` pulls this
into the Ollama container automatically on first run.

---

## Install

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@latest
```

The binary is `gg`. The repo is `gg-cli` for descriptiveness; the
command stays short.

Or build from source:

```sh
git clone https://github.com/gurkangul/gg-cli
cd gg-cli
go build -o gg ./cmd/gg
```

---

## Quick start

```sh
# 1. Initialize gg for a project (run once per project)
cd /path/to/your/project
gg init

# 2. Verify services and binaries
gg doctor

# 3. Index the codebase (requires Memgraph + a SCIP indexer)
gg index --lang go

# 4. Record a decision
gg record "use JWT for auth" --reason "stateless, scales well" --tags "auth,api"

# 5. Search context before making a change
gg context "authentication"

# 6. Check status (what tasks are open, any unread messages?)
gg status
```

---

## Commands

### Session / context

| Command | Description |
|---|---|
| `gg status` | Open tasks, pending messages, recent decisions |
| `gg search "topic"` | Semantic search over decisions and rejections |
| `gg context "topic"` | Unified context bundle: decisions + rejections + notes + code graph |

### Decisions & rejections

| Command | Description |
|---|---|
| `gg decide "text" --reason "why" --tags "t1,t2"` | Record a decision |
| `gg reject "text" --reason "why" --tags "t1,t2"` | Record a rejected approach |

### Tasks

| Command | Description |
|---|---|
| `gg task create "title" --detail "…" --priority high` | Open a task |
| `gg task list` | List tasks (add `--ready` to filter unblocked ones) |
| `gg task get TASK-ID` | Show task details |
| `gg task done TASK-ID "summary"` | Mark a task as done |
| `gg task block TASK-ID --reason "…"` | Mark a task as blocked |
| `gg task deps TASK-ID` | Show dependency status |

### Bugs

| Command | Description |
|---|---|
| `gg bug report "title" --detail "…" --severity high` | Report a bug |
| `gg bug list` | List bugs |
| `gg bug start BUG-ID` | Move to "fixing" |
| `gg bug fix BUG-ID "summary"` | Mark as fixed |
| `gg bug wontfix BUG-ID "reason"` | Close as won't-fix |
| `gg bug triage BUG-ID` | Auto context bundle for fixing |

### Discussions

Open discussions track unresolved questions so they survive across sessions.

| Command | Description |
|---|---|
| `gg discuss open "topic" --detail "…"` | Open a discussion |
| `gg discuss list` | List open discussions |
| `gg discuss resolve DISC-ID --via decision --summary "…"` | Close with a resolution |
| `gg discuss dismiss DISC-ID --reason "…"` | Close as irrelevant/superseded |

### Notes

```sh
gg note "observed X while working on TASK-NNN — might affect Y"
gg note list
gg note search "topic"
```

Notes are semantically searchable and have no lifecycle.

### Messaging between agents

```sh
gg tell "developer" "TASK-017 done: auth middleware ready" --from architect
gg inbox                  # read your messages
```

### Code index (requires Memgraph)

```sh
gg index --lang go               # full index
gg index --lang go --changed     # incremental (delta from last indexed SHA)
gg index --lang typescript
gg index --lang python
```

Supported indexers: `scip-go`, `scip-typescript`, `scip-python`.

### Keeping agent context small

gg output grows with the knowledge base; on mature projects a single `gg
context` can span hundreds of lines. Two orthogonal options:

- `--compact` on verbose commands — one line per item (IDs, titles, dates),
  no reasons or detail bodies. The agent picks which items to fetch in full:

  ```sh
  gg context "auth" --compact
  gg search "jwt" --compact
  gg task get TASK-042 --compact
  gg impact src/auth.go --compact
  ```

- A generic shell-output compressor like [RTK](https://github.com/rtk-ai/rtk)
  transparently shrinks all tool output (git, tests, gg) before it reaches
  the model's context. Independent of gg; optional.

### Observability (GG_TRACE=1)

| Command | Description |
|---|---|
| `gg trace show [--op=X] [--since=1h] [--limit=N]` | Print recorded spans, newest first |
| `gg trace summary [--since=1h]` | Per-operation p50/p95/p99 latency breakdown |
| `gg trace clear [--older-than=7d]` | Delete old trace JSONL files |

Enable recording: `GG_TRACE=1 gg search "topic"`. See [`docs/commands/trace.md`](docs/commands/trace.md) for details.

### Ops

| Command | Description |
|---|---|
| `gg init` | Initialize Qdrant collections and register project |
| `gg doctor` | Check service connectivity and indexer binaries |
| `gg doctor --install-indexers` | Auto-install missing SCIP binaries |
| `gg doctor --reconcile` | Surface incomplete dual-store writes and show repair commands |
| `gg reembed --confirm` | Migrate all collections to the currently configured embedding model |

---

## Configuration

`gg init` creates `.gg/config.yaml` in the project root. Edit to match your setup:

```yaml
project_id: <uuid>              # auto-generated by gg init — do not edit
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
  username: ""
  password: ""                  # leave empty; use MEMGRAPH_PASSWORD instead (see below)
```

### Security — Credentials

**Never write a Memgraph password into `.gg/config.yaml`.**
The file is easy to accidentally commit. Use environment variables instead — they
override the corresponding config fields at runtime:

| Env var | Overrides |
|---|---|
| `MEMGRAPH_PASSWORD` | `memgraph.password` |
| `MEMGRAPH_USERNAME` | `memgraph.username` |
| `MEMGRAPH_URI` | `memgraph.uri` |

```sh
# Shell or .env (gitignored)
export MEMGRAPH_PASSWORD="your-password"
gg status   # password is picked up automatically
```

Add `.gg/config.yaml` to your project's `.gitignore` as an extra precaution
(the project UUID inside is not secret, but belt-and-suspenders never hurts):

```
.gg/config.yaml
```

---

## Multi-agent pattern

Each agent runs `gg status` at session start — this is enforced by `AGENTS.md`.

```
Agent A (architect)          Agent B (developer)          Agent C (reviewer)
gg status                    gg status                    gg status
↓                            ↓                            ↓
sees open tasks,             picks up TASK-017,           sees decision about auth,
unread messages              gg decide "JWT chosen"       gg search "JWT" → finds it
```

All agents write to the same Qdrant + Memgraph backend. A decision made by one is immediately visible to the others.

---

## Architecture

```mermaid
flowchart LR
    subgraph agents["Agents (any terminal)"]
        A1[Claude Code]
        A2[Cursor]
        A3[Aider / Codex]
    end

    subgraph gg["gg CLI (subprocess)"]
        CMD[cobra commands]
        OB[outbox\ncrash guard]
    end

    subgraph storage["Local storage (Docker)"]
        QD[(Qdrant\nvector store)]
        OL[Ollama\nnomic-embed-text]
        MG[(Memgraph\ncode graph)]
    end

    A1 & A2 & A3 -->|gg record / search / context| CMD
    CMD -->|decisions · tasks · notes · rejections| QD
    CMD --> OL
    OL -->|768-dim vectors| QD
    CMD -->|index writes| OB
    OB -->|symbols · calls · imports| MG
```

```
gg (CLI)
├── cmd/           — cobra commands
├── internal/
│   ├── store/     — Qdrant client (decisions, tasks, bugs, notes, discussions, messages)
│   ├── graph/     — Memgraph client (code knowledge graph: Symbol, File, Package nodes)
│   ├── index/
│   │   ├── parser/    — SCIP file parser
│   │   ├── runner/    — SCIP indexer resolution and execution
│   │   ├── changed/   — git diff + IsAncestor for incremental indexing
│   │   ├── state/     — index-state.json (last indexed SHA)
│   │   └── compat/    — indexer version manifest
│   ├── embedding/ — Ollama embedding generator + model metadata
│   └── outbox/    — file-based crash-safety queue for Memgraph writes
└── AGENTS.md      — agent behavior rules (session start, decisions, tasks, …)
```

**Isolation:** every Qdrant point and every Memgraph node carries a `project_id` property. Multiple projects can share the same backend without data leakage.

**Crash safety:** `gg index` writes an outbox entry before touching Memgraph. On success the entry is deleted. If the process dies mid-write, `gg doctor --reconcile` surfaces the pending entry and shows the exact repair command.

---

## Tech Stack

| Layer | Technology | Why |
|---|---|---|
| CLI framework | [Cobra](https://github.com/spf13/cobra) | Idiomatic Go subcommand routing |
| Language | Go 1.26.2 | Single-binary distribution, no runtime deps |
| Vector store | [Qdrant](https://qdrant.tech) via gRPC | Semantic search on decisions, tasks, notes, rejections |
| Code graph | [Memgraph](https://memgraph.com) via Bolt | Structural queries: `CALLS`, `IMPORTS`, `DEFINES` |
| Embeddings | [Ollama](https://ollama.ai) — `nomic-embed-text` 768-dim | Local, no API key, runs in the same Docker stack |
| Code indexers | SCIP (`scip-go`, `scip-typescript`, `scip-python`) | Language-agnostic symbol graph with cross-file resolution |

---

## Engineering Decisions

Decisions that shaped the design — and why the alternatives lost.

**No daemon, subprocess interface only.**
A background daemon would require a service manager, a PID file, and crash recovery. Instead, `gg` is a stateless CLI: agents call it as a subprocess, Docker provides the stores. This eliminated an entire class of process-lifecycle bugs and kept the distribution a single binary. See [docs/architecture.md](docs/architecture.md).

**Two-store architecture: Qdrant + Memgraph.**
Decisions, tasks, and rejections need fuzzy semantic search (`gg search "auth"` should surface JWT discussions even if the query doesn't match exact words). Code impact queries need structural traversal (`gg impact src/auth.go` follows call chains). A single store forces a compromise in both. Two purpose-built stores, one per query type.

**`gg record` with `--stance` flag instead of separate `decide`/`reject` verbs.**
Early design had six command verbs. Five (record, note, task, bug, discuss) covers the full lifecycle with less surface area. `gg decide` and `gg reject` remain as aliases for agent compatibility, but `record --stance=accept|reject` is canonical. See the 6→5 verb taxonomy decision.

**Outbox pattern for dual-store consistency.**
When `gg index` writes to both Qdrant and Memgraph, a crash between the two writes leaves the stores out of sync. gg writes a `.gg/outbox/<id>.json` entry before the Memgraph write and deletes it on success. `gg doctor --reconcile` surfaces any dangling entries. No saga framework, no distributed transaction — just a file and a reconciler.

**`project_id` as the isolation primitive.**
Rejected: Memgraph 3.x multi-database feature (not broadly available, adds infra coupling). Chosen: every Qdrant point and every Memgraph node carries a `project_id` UUID injected at the `runQuery` level in `internal/graph/queries.go`. A new project gets a new UUID from `gg init`; shared infra at `~/.gg/` serves all projects without data leakage.

**SCIP-first hybrid parser (SCIP + tree-sitter fallback).**
Pure SCIP gives high-quality cross-file symbol resolution for supported languages (Go, TypeScript, Python via `scip-go`, `scip-typescript`, `scip-python`). Tree-sitter covers languages with no SCIP indexer. The spike showed scip-go at 0.97s for a real repo, 3.78ms ParseFile, 1365 symbols — production-ready. Rejected: writing a custom AST parser (maintenance surface); Docker-based SCIP fallback (day-1 complexity).

---

## Telemetry (local-only, opt-out)

`gg` writes a single JSON line per command to `<project>/.gg/telemetry.jsonl`
recording verb usage (which `gg` commands ran, agent vs human). This data is
**strictly local — never sent anywhere over the network** — and powers gg's
own dogfood metric (DISC-008): are agents actually following AGENTS.md rules?

The file is append-only; you can rotate, inspect, or delete it freely.

Disable with:

```bash
export GG_TELEMETRY=0   # also: false, no, off
```

---

## Documentation

| Doc | Purpose |
|---|---|
| [docs/getting-started.md](docs/getting-started.md) | Installation and first run |
| [docs/commands.md](docs/commands.md) | Full command reference |
| [docs/cli/](docs/cli/) | Auto-generated per-command CLI reference (regenerated by `go run ./tools/docs-gen`) |
| [docs/architecture.md](docs/architecture.md) | Package layout, crash safety, isolation |
| [docs/adapters.md](docs/adapters.md) | Agent integrations, SCIP indexers, git hooks |
| [docs/roadmap.md](docs/roadmap.md) | Phase plan and vision (historical) |
| [AGENTS.md](AGENTS.md) | Agent behavior contract (runtime) |

---

## Roadmap

| Phase | Status | Scope |
|---|---|---|
| Phase 1 — Core CLI | ✅ Done | Record/search/task/bug/discuss/note/tell, Qdrant store, AGENTS.md protocol |
| Phase 2 — Code Intel | ✅ Done | `gg index`, `gg impact`, `gg context`, SCIP parsers, Memgraph graph, outbox crash safety |
| Phase 3 — Adoption polish | 🚧 In progress | README/docs quality, multi-tenancy, agent hook enforcement, `gg session-start` |

See [docs/roadmap.md](docs/roadmap.md) for the detailed phase plan.

---

## Contributing

Contributions welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

For security issues, see [SECURITY.md](SECURITY.md) — please do not open a public issue.

---

## License

MIT — see [LICENSE](LICENSE).
