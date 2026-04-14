# gg — One brain, any agent

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
gg decide "use JWT for auth" --reason "stateless, scales well" --tags "auth,api"

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
project_id: my-project          # unique per project
qdrant:
  host: localhost
  port: 6334
embedding:
  host: http://localhost:11434
  model: nomic-embed-text
memgraph:
  uri: bolt://localhost:7687
  username: ""
  password: ""
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

## License

MIT — see [LICENSE](LICENSE).
