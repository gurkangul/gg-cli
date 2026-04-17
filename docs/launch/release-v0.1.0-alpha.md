# gg v0.1.0-alpha — Release Notes

**Release date:** 2026-04-17
**Tag:** `v0.1.0-alpha`

## What is gg?

A CLI that gives multiple AI agents a shared brain. Every agent — Claude Code,
Cursor, Aider, Codex, GSD — reads the same decisions, tasks, rejections, and
code graph through `gg`. No agent starts from a blank slate.

Three pains it solves for developers running 2+ agents in parallel:
1. Each agent re-derives context from scratch
2. Impact-blind fixes create fix loops
3. Rejected approaches keep getting re-proposed

## What's working

- **Shared memory** — `gg record`, `gg search`, `gg context` across all agents
- **Task protocol** — `gg task create/get/done/block/list` with priority ordering
- **Multi-agent messaging** — `gg tell <agent> <message>`, `gg inbox`
- **Rejection tracking** — `gg record --stance=reject` prevents re-proposals
- **Discussion threads** — `gg discuss open/note/resolve/dismiss`
- **Code impact analysis** — `gg impact <symbol>` via Memgraph + SCIP indexing
- **Semantic search** — Qdrant + Ollama local embeddings (no data leaves machine)
- **Multi-project isolation** — shared infra at `~/.gg/`, per-project UUID namespace
- **Trace/debug tooling** — `GG_TRACE=1`, `gg trace show/summary/clear`
- **OSS-ready** — MIT license, CONTRIBUTING.md, SECURITY.md, GitHub Actions CI/release

## Known limitations (alpha)

- Ollama must be running locally for embeddings (`gg doctor` checks this)
- Memgraph requires Docker; graph features degrade gracefully if unavailable
- `gg impact` requires a prior `gg index` run (SCIP-based — Go/Python/TS supported)
- Windows arm64 not yet built (goreleaser ignore rule)
- No auth/encryption on local Docker services — treat as developer-local only

## Install

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@v0.1.0-alpha
```

Or download the binary for your platform from the [GitHub Release assets](https://github.com/gurkangul/gg-cli/releases/tag/v0.1.0-alpha).

## Quickstart

```sh
gg init                          # provision ~/.gg/ infra (Docker)
cd your-project && gg init       # create .gg/config.yaml
gg record "use Qdrant for semantic storage" --reason "local, no egress" --tags "architecture"
gg search "storage"              # any agent can find this
gg task create "implement X" --priority high
gg status                        # unified view of tasks, messages, decisions
```

## What's next

- `gg export` — portable bundle for project handoffs
- Homebrew tap + apt/yum packages
- Cursor / Windsurf adapter docs
- v0.2.0: real-time cross-agent event streaming
