# gg — One brain, any agent

[![CI](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/gurkangul/gg-cli)](https://github.com/gurkangul/gg-cli/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gurkangul/gg-cli)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Stable 1.0](https://img.shields.io/badge/status-stable%201.0-brightgreen)](https://github.com/gurkangul/gg-cli/releases)

**gg** is a local shared-brain CLI for developers who run more than one AI
coding agent in the same codebase.

Claude Code, Codex, Cursor, Aider, DeepSeek/Qwen-based agents, custom shells,
and manual terminals can all read and write the same project memory through one
simple interface:

```sh
gg status
gg search "auth" --compact
gg context "checkout" --compact
gg impact path/to/file.go --compact
```

No hosted service. No gg daemon. No agent-specific SDK. If an agent can run a
terminal command, it can use gg.

gg does not own or run the agent's workflow. Agents keep their native flow;
gg stores the durable shared memory and evidence that future agents need.

> **Status:** stable (1.0) and actively dogfooded. As of 1.0.0 the stability
> guarantees below are in effect: the stable command surface is frozen within a
> major version, storage is forward-only readable, and breaking changes follow
> SemVer with a deprecation cycle.
>
> **Stability & versioning:** see [docs/stability.md](docs/stability.md) for the
> 1.0 contract (SemVer, command tiers, forward-only storage, deprecation policy)
> and [docs/1.0-readiness.md](docs/1.0-readiness.md) for the readiness audit.

---

## Why gg exists

Multi-agent development is powerful, but the default experience is messy:

- every agent starts from a blank slate
- rejected ideas keep coming back
- one agent changes code without knowing what another agent already decided
- fixes land without impact analysis
- “done” is claimed without an independent review or live smoke test

gg gives that work a shared memory layer.

| What usually breaks | What gg gives you |
|---|---|
| Agents re-discover the same context | Shared decisions, tasks, bugs, messages, notes, and compact context bundles |
| Rejected approaches get proposed again | Rejections are first-class memory and appear in search/context |
| Parallel agents collide | Task ownership, leases, inboxes, and agent broadcasts |
| Fixes are impact-blind | CodeGraph impact queries across files, symbols, bugs, tasks, and decisions |
| “Done” means “the agent thinks it works” | Review gates, ready-for-live handoff, hooks, and smoke-test discipline |
| Each project drifts differently | Cross-project sync, health checks, and portable brain snapshots |

---

## What you gain

- **One shared memory for every agent**
  Decisions, tasks, bugs, handoffs, rejections, discussions, and notes live in
  one project brain instead of scattered chat windows.

- **Consistent, durable memory under concurrency**
  Mutations are JSONL-first with optimistic version/CAS, so state survives a
  Qdrant rebuild and parallel writers don't silently clobber each other. Inbox
  read-state is per-recipient — one agent reading never consumes another's
  message — and concurrent Claude tabs get a unique per-session identity.

- **Evidence-aware records**
  Attach how a decision was verified with `gg record --evidence "…"`; unproven
  records surface as `[unverified]`, and every note carries its author — so a
  checked fact and a guess aren't stored with equal weight.

- **Model-agnostic collaboration**
  gg does not care whether the caller is Claude, Codex, Cursor, Aider, a
  DeepSeek/Qwen agent, or a human shell. The contract is the CLI.

- **Less context waste**
  `--compact` output gives agents dense one-line summaries first. They hydrate
  full records only when needed.

- **Fewer repeated mistakes**
  Rejected approaches are stored beside decisions, so future agents see why an
  idea was not chosen.

- **Safer edits**
  `gg impact` shows what a file or task touches before an agent changes it.

- **Optional task ownership**
  When a project uses gg-managed tasks, agents can claim tasks with leases,
  renew work they still own, release work they abandon, and hand off review
  explicitly.

- **Reviewer-verified completion where configured**
  Implementers can mark work ready for live verification, while a separate
  reviewer closes it in projects that enforce verifier separation.

- **Local-first privacy**
  Data stays on your machine. gg uses local Docker services for search,
  embeddings, and graph queries.

- **Portable project brain**
  Brain snapshots can be exported, checked, and restored without depending on a
  hosted account.

- **Cross-project maintenance**
  A single command can refresh contracts, hooks, tracker collections, and health
  checks across all registered local projects.

---

## How it works

```text
Agent A        Agent B        Agent C        Human shell
  │              │              │              │
  └─────── runs `gg ...` as a subprocess ──────┘
                         │
                         ▼
              local shared project brain
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
  embedded SQLite     Ollama       embedded SQLite
  semantic memory   embeddings    CodeGraph (graph.db)
   (vectorstore.db)  (or Voyage)
```

gg is intentionally boring infrastructure:

- agents call a CLI command
- every record is namespaced by project ID
- an embedded SQLite vector store (`.gg/vectorstore.db`) powers semantic recall — no Docker
- native Ollama (or the opt-in Voyage cloud backend) provides embeddings
- an embedded SQLite graph store (`.gg/graph.db`) holds the code graph — no Docker
- project metadata is committed with the project
- runtime state stays outside the project repository

The vector and graph stores are embedded by default, so a fresh `gg init` needs
no Qdrant or Memgraph containers. Server backends remain selectable for users who
prefer them (`qdrant.backend: qdrant` / `memgraph.backend: memgraph`).

There is no central coordinator and no background gg daemon. CodeGraph freshness
is explicit: run a one-shot repair when needed, or start an opt-in foreground
watcher that stops when you stop it.

---

## Install

Prerequisites:

- Go matching the version in [`go.mod`](go.mod)
- An embedding provider: native Ollama (`brew install ollama` + `ollama serve` +
  `ollama pull nomic-embed-text`) — or the opt-in Voyage cloud backend
- Docker is NOT required (the vector and graph stores are embedded SQLite by
  default). It is only needed if you opt into the Qdrant/Memgraph server backends.

Install the CLI:

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@latest
```

Initialize gg inside a project:

```sh
cd /path/to/your/project
gg init
gg doctor
gg doctor --install-agent-hooks
```

`gg init` creates everything locally with no Docker:

- an embedded SQLite vector store (`.gg/vectorstore.db`) for semantic search
- an embedded SQLite graph store (`.gg/graph.db`) for CodeGraph queries
- embeddings via native Ollama (`nomic-embed-text`); if Ollama is unreachable,
  init WARNS with install guidance rather than failing — set Ollama up (or the
  Voyage backend), then run `gg reembed` to populate the vector store.

Existing project switching to the embedded stores? Run `gg reembed` once to build
`.gg/vectorstore.db` from `.gg/brain/*.jsonl`, and `gg index --lang <lang>` to
build the code graph. The canonical data is always the committed JSONL brain.

No cloud account or API key is required for normal use. Installing the CLI and
checking for updates can still use the network when you explicitly run those
commands. Only the opt-in server backends (`qdrant.backend: qdrant` /
`memgraph.backend: memgraph`) need Docker.

For brownfield projects, or after a gg upgrade, refresh agent instructions and
hooks explicitly:

```sh
gg doctor --install-agent-hooks
gg doctor --install-task-hooks
```

Optional but recommended for impact analysis:

```sh
gg doctor --install-indexers
gg doctor --fix-index
```

Update later with:

```sh
gg update check
gg update
```

For a local gg-cli checkout with dogfood changes:

```sh
make install
# or
gg update --from-source --skip-sync
```

---

## Quick start for agents

At the start of an agent session, orient with gg when shell access is available:

```sh
export GG_AGENT="codex-1"          # unique runtime instance
export GG_ROLE="implementer"       # role/authority for this session

gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```

These commands do not replace the agent's native workflow. Use BMAD, GSD, OMO
Slim, Antigravity, Codex, Claude Code, Cursor, Aider, or a manual shell as
appropriate. Sync durable outputs into gg. For per-agent capture examples, see
[`docs/native-workflow-capture.md`](docs/native-workflow-capture.md).

Before changing important behavior:

```sh
gg search "topic or feature" --compact
gg context "topic or feature" --compact
```

Before editing source files where impact matters:

```sh
gg impact path/to/file.go --compact
```

When a durable output exists:

```sh
gg record "decision text" --reason "why"
gg record "rejected approach" --decision-status rejected --reason "why not"
gg tell reviewer \
  "TASK-123 handoff. Evidence: commands run: go test ./... -count=1; live smoke: not applicable; impacted files: cmd/foo.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-diff.txt" \
  --from "$GG_ROLE" --task TASK-123
```

Minimal evidence packet: commands run, live smoke result, impacted files, known
gaps, and artifact paths. Keep bulky logs/screenshots/traces in their native
location; store the summary and reference in gg.

If the project uses gg-managed tasks:

```sh
gg task list --ready --compact
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg tell all "TASK-123 started by $GG_AGENT ($GG_ROLE)" \
  --from "$GG_ROLE" --audience agents --task TASK-123
```

When implementation is locally verified and configured gates require review:

```sh
gg task ready-for-live TASK-123 \
  --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=cmd/foo.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE"

gg tell reviewer \
  "TASK-123 ready. Evidence: commands run: go test ./... -count=1; live smoke: CLI smoke passed; impacted files: cmd/foo.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE" --task TASK-123
```

The implementer should not close their own task in projects that enforce
reviewer separation.

---

## Daily workflow

```sh
# Capture durable decisions
gg record "use JWT for auth" \
  --reason "stateless, simple to deploy" \
  --tags "auth,api"

# Attach evidence when the decision is something you verified
gg record "switch to connection pooling" \
  --reason "cuts p99 latency" \
  --evidence "load test 320ms->90ms p99; live smoke passed" \
  --tags "perf,db"

# Capture rejected approaches too
gg record "store sessions in Redis" \
  --decision-status rejected \
  --reason "adds infra we do not need yet" \
  --tags "auth,api"

# Create and inspect work
gg task create "add auth middleware" \
  --detail "protect API routes" \
  --priority high \
  --requester user

gg task list --ready --compact
gg task get TASK-001

# Ask the shared brain before editing
gg search "authentication" --compact
gg context "authentication" --compact
gg impact path/to/file.go --compact

# Check project health
gg status
gg check --strict
gg doctor
```

Use compact output for scanning. Hydrate the full task, bug, or decision before
changing state.

---

## Agent and model compatibility

gg works at the process boundary, not the model boundary.

| Runtime/model setup | Works with gg? | Requirement |
|---|---:|---|
| Claude Code | Yes | Shell/tool command access |
| Codex-style CLI agent | Yes | Shell/tool command access |
| Cursor agent | Yes | Project rules + shell/tool command access |
| Aider/manual terminal | Yes | Run `gg` commands directly |
| DeepSeek/Qwen-based agent | Yes | The host agent runtime must expose shell/tool execution |
| Plain chat with no tools | Indirectly | A human or wrapper must run the commands |

This is why gg can support new models without rewriting integrations: the
stable interface is the command line.

---

## Core commands

| Area | Commands |
|---|---|
| Orientation | `gg session-start`, `gg status`, `gg search`, `gg context`, `gg inbox` |
| Decisions | `gg record`, `gg record --decision-status rejected` |
| Tasks | `gg task create/list/get/start/renew/release/block/ready-for-live/done` |
| Bugs | `gg bug report/triage/start/fix/wontfix/reopen` |
| Impact | `gg index`, `gg index status`, `gg impact` |
| Sync | `gg system sync`, `gg system brain status` |
| Brain snapshots | `gg brain export`, `gg brain import`, `gg brain status` |
| Health | `gg doctor`, `gg doctor --reconcile`, `gg reconcile`, `gg check` |
| Maintenance | `gg update check`, `gg update`, `gg reembed` |

Full command reference:

- [`docs/commands.md`](docs/commands.md)
- [`docs/cli/`](docs/cli/)

---

## Safety model

gg does not ask you to trust agents more. It gives agents fewer excuses to act
without evidence.

Important guardrails:

- search previous decisions before proposing a new direction
- record rejected approaches so they do not come back later
- run impact checks before editing source files
- use gg-managed task ownership and leases when the project needs collision avoidance
- use role-scoped inbox reads for assignments and review handoffs
- keep broadcasts agent-only unless the human needs to see them
- run tests and live-shaped smoke checks before recording ready-for-live evidence
- let a different reviewer close the task when verifier separation is enabled
- reconcile after crashes or suspected store drift

The result is not “the AI is always right.” The result is: every claim has a
trail, every decision has a reason, and every agent can inspect the same facts.

---

## Cross-project operations

gg can manage every registered local project from one place:

```sh
gg system sync
gg system brain status
```

Use this after installing a new gg release to propagate:

- managed agent instructions
- Claude/Cursor/Codex/GSD contract blocks where detected
- task and verification hooks
- tracker collection self-healing
- brain snapshot health checks
- CodeGraph readiness reports

This is what keeps a multi-project workstation from drifting into nine slightly
different agent protocols.

---

## Data and privacy

gg is local-first:

- project identity and rules are stored with the project
- runtime cache and telemetry stay under your local `~/.gg/` directory
- semantic search is an embedded local SQLite vector store (no Docker)
- embeddings are local Ollama (or the opt-in Voyage cloud backend, which sends text off-machine)
- CodeGraph is an embedded local SQLite graph store (no Docker)
- records are namespaced by project ID
- telemetry is local-only and can be disabled

Disable local telemetry with:

```sh
export GG_TELEMETRY=0
```

See [`docs/architecture.md`](docs/architecture.md) for isolation, storage,
crash-safety, and indexing details.

---

## Documentation

| Doc | Purpose |
|---|---|
| [`docs/getting-started.md`](docs/getting-started.md) | first install and first project |
| [`docs/commands.md`](docs/commands.md) | command overview |
| [`docs/cli/`](docs/cli/) | generated per-command help |
| [`docs/agent-protocol-v1.md`](docs/agent-protocol-v1.md) | multi-agent operating protocol |
| [`docs/native-workflow-capture.md`](docs/native-workflow-capture.md) | capture points for BMAD, GSD2, OMO Slim, Antigravity, Codex, Claude Code, Cursor, and Aider |
| [`docs/architecture.md`](docs/architecture.md) | internals and data model |
| [`docs/adapters.md`](docs/adapters.md) | agent integrations and hooks |
| [`docs/verify-gate.md`](docs/verify-gate.md) | task-close verification gates |
| [`docs/roadmap.md`](docs/roadmap.md) | historical plan and future direction |

---

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md).

For security issues, see [`SECURITY.md`](SECURITY.md). Please do not open a
public issue for vulnerabilities.

## License

MIT — see [`LICENSE`](LICENSE).
