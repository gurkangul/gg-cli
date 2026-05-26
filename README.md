# gg — One brain, any agent

[![CI](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/gurkangul/gg-cli/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/release/gurkangul/gg-cli)](https://github.com/gurkangul/gg-cli/releases/latest)
[![Go Version](https://img.shields.io/github/go-mod/go-version/gurkangul/gg-cli)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Status: Alpha](https://img.shields.io/badge/status-alpha-orange)](https://github.com/gurkangul/gg-cli/releases)

> **Status: Alpha.** API and storage format may change between releases.
> Good for personal projects and early multi-agent dogfooding; not yet a
> production multi-team coordination system.

**gg** is a local shared-brain CLI for developers running multiple AI agents
(Codex, Claude Code, Cursor, Aider, manual shells, etc.) in the same project.

It gives every agent the same project memory:

- decisions and rejected approaches
- tasks, bugs, handoffs, and inbox messages
- compact context bundles for agent prompts
- optional code-impact graph for safer edits

**One brain. Any agent. Local-first. Subprocess-only. No hosted service.**

---

## Why gg exists

| Multi-agent failure mode | What gg adds |
|---|---|
| Every agent re-derives context from scratch | `gg status`, `gg search`, and `gg context` surface prior project memory |
| Agents repeat rejected ideas | Rejections are first-class records returned by search/context |
| Fixes are impact-blind | `gg index` + `gg impact` show related files, symbols, tasks, bugs, and decisions |
| Parallel agents collide | `gg task`, `gg tell`, and `gg inbox` make claims and handoffs visible |
| “Done” is asserted without proof | Verify hooks and hydration gates make agents read context and run checks first |

---

## Install

Prerequisites:

- Go matching the version in [`go.mod`](go.mod)
- Docker with Compose v2

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

`gg init` starts local Docker services under `~/.gg/` when needed:

- Qdrant for semantic search
- Ollama with `nomic-embed-text` for local embeddings
- Memgraph for optional code-impact queries

No cloud account or API key is required.

Update later with:

```sh
gg update check
gg update
```

For unreleased development builds:

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@main
```

For a local gg-cli checkout with uncommitted dogfood changes:

```sh
make install
# or
gg update --from-source --skip-sync
```

---

## Agent setup

| Agent | Recommended path |
|---|---|
| Codex | Install `gg`, run `gg init`; Codex reads project `AGENTS.md`. Optional global reminder: `~/.codex/instructions.md`. |
| Claude Code | Install `gg`, run `gg init`, then `gg doctor --install-agent-hooks`; Claude reads generated `CLAUDE.md` / `AGENTS.md`. |
| Cursor | Install `gg`, run `gg init`, then `gg doctor --install-agent-hooks`; Cursor reads generated `.cursor/rules/gg.mdc`. |
| Manual shell | `export GG_AGENT=manual GG_ROLE=developer`, then run `gg status` and `gg inbox --role "$GG_ROLE" --since-cursor`. |

At the start of a session, agents should orient themselves with:

```sh
export GG_AGENT="${GG_AGENT:-agent}"  # codex, claude-code, cursor, aider, ...
gg status
gg inbox --role "${GG_ROLE:-developer}" --since-cursor
```

See [`docs/getting-started.md`](docs/getting-started.md) for the full agent
workflow.

---

## Quick workflow

```sh
# Record durable context
gg record "use JWT for auth" --reason "stateless, scales well" --tags "auth,api"
gg record "raw SQL rollback rejected" --decision-status rejected --reason "unsafe in prod"

# Create and inspect work
gg task create "add auth middleware" --detail "protect API routes" --priority high --requester user
gg task list --compact
gg task get TASK-001

# Search before editing
gg search "auth" --compact
gg context "authentication" --compact

# Optional code graph
gg doctor --install-indexers
gg index --lang go
gg index --watch --lang go  # optional foreground watcher; Ctrl-C stops it
gg impact internal/auth/middleware.go --compact

# Cross-agent handoff
gg tell "all" "TASK-001 picked up" --from developer --audience agents
gg inbox --role reviewer --since-cursor
```

Use compact output for scanning; hydrate the full record before changing state.
Compact rows are an index, not the source of truth.

CodeGraph freshness is explicit. gg does not start a background indexing daemon:
agent-facing commands warn when the graph is missing or stale, `gg doctor --fix-index`
is the canonical one-shot repair, and `gg index --watch` / `gg watch --index` are
operator-started foreground watchers that run only until Ctrl-C.
`gg session-start`, `gg next`, `gg impact`, `gg doctor`, and `gg index status`
use the same freshness notice contract: status/reason, repair command, and
foreground-watch hint all come from one shared model.
Freshness tracks source files plus selected module manifests, not dependency
lockfiles such as `go.sum`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`,
`poetry.lock`, or `uv.lock`.

---

## Core commands

| Area | Commands |
|---|---|
| Orientation | `gg status`, `gg search`, `gg context`, `gg inbox` |
| Decisions | `gg record`, `gg record --decision-status rejected` |
| Tasks | `gg task create/list/get/start/renew/release/done/block/ready-for-live` |
| Bugs | `gg bug report/triage/start/fix/wontfix/reopen` |
| Code impact | `gg index`, `gg index status`, `gg impact` |
| Brain snapshots | `gg brain export`, `gg brain status`, `gg system brain status` |
| Health | `gg doctor`, `gg doctor --reconcile`, `gg reconcile`, `gg check` |
| Maintenance | `gg update check`, `gg update`, `gg reembed` |

Full command reference:

- [`docs/commands.md`](docs/commands.md)
- [`docs/cli/`](docs/cli/)

---

## How data is stored

gg is local-first:

- project metadata lives in `.gg/config.yaml`
- durable brain records are JSONL-first
- Qdrant is a derived semantic-search index
- Memgraph is an optional derived code graph
- runtime telemetry/cache/brain exports live outside public source paths

Every record is namespaced by project ID, so multiple projects can share the
same local backend without mixing data.

See [`docs/architecture.md`](docs/architecture.md) for package layout,
crash-safety, indexing, and isolation details.

---

## Safety model

gg is useful because it makes project memory visible, but it is deliberately not
magic. Agents still need to verify before acting.

Important guardrails:

- use `--compact` for scans, then full `gg task get` / `gg bug get` before state changes
- run `gg impact <file>` before editing source files in gg-managed projects
- repair stale CodeGraph state with `gg doctor --fix-index`, or explicitly keep a
  foreground watcher open with `gg index --watch` / `gg watch --index`
- run project tests before `gg task done`
- use `--audience agents` for agent-status broadcasts so human inboxes stay clean
- run `gg doctor --reconcile` after crashes or suspected store drift

See [`docs/verify-gate.md`](docs/verify-gate.md) and
[`docs/adapters.md`](docs/adapters.md) for hooks and agent integration.

---

## Local telemetry

When enabled, gg writes local command telemetry to
`~/.gg/projects/<project_id>/telemetry.jsonl`. It is never sent to a hosted
service.

Disable it with:

```sh
export GG_TELEMETRY=0
```

---

## Documentation

| Doc | Purpose |
|---|---|
| [`docs/getting-started.md`](docs/getting-started.md) | install and first run |
| [`docs/commands.md`](docs/commands.md) | command overview |
| [`docs/cli/`](docs/cli/) | generated per-command help |
| [`docs/architecture.md`](docs/architecture.md) | internals and data model |
| [`docs/adapters.md`](docs/adapters.md) | agent integrations and hooks |
| [`docs/verify-gate.md`](docs/verify-gate.md) | task-close verification gates |
| [`docs/roadmap.md`](docs/roadmap.md) | historical plan and future direction |
| [`AGENTS.md`](AGENTS.md) | runtime agent contract for this repository |

---

## Contributing

Contributions are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md).

For security issues, see [`SECURITY.md`](SECURITY.md) — please do not open a
public issue.

## License

MIT — see [`LICENSE`](LICENSE).
