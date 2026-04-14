# Adapters & Integrations

## AI agent adapters

`gg` works with any agent that can run shell commands. The integration is via `AGENTS.md` — drop it in your project root and agents will find it automatically.

### Claude Code

Add to your `CLAUDE.md` or include `AGENTS.md` at session start. Claude Code calls `gg` as a subprocess.

### BMAD / orchestrator agents

Point the agent's rules file at `AGENTS.md`. In party mode, each role (architect, developer, reviewer) gets a separate terminal with `GG_ROLE` set:

```sh
# Terminal 1 — architect
export GG_ROLE=architect
claude  # or your agent command

# Terminal 2 — developer
export GG_ROLE=developer
claude
```

### Custom agents

Any agent that can execute shell commands can use `gg`. Use `--json` flag for machine-readable output:

```sh
gg status --json
gg search "topic" --json
gg task list --json
```

## SCIP indexers

`gg index` uses SCIP indexers to build the code knowledge graph. Install them via `gg doctor --install-indexers` or manually:

| Language | Indexer | Install |
|---|---|---|
| Go | `scip-go` | `go install github.com/sourcegraph/scip-go/cmd/scip-go@latest` |
| TypeScript | `scip-typescript` | `npm install -g @sourcegraph/scip-typescript` |
| Python | `scip-python` | `pip install scip-python` |

Requires Go 1.21+, Node.js 18+, or Python 3.9+ respectively.

## Git hooks

Use `gg check` as a pre-push hook to gate pushes on open tasks or unresolved discussions:

```sh
# .git/hooks/pre-push
#!/bin/sh
gg check
```

```sh
chmod +x .git/hooks/pre-push
```

## Backend alternatives

The default backend is local Docker. For shared team setups, point `config.yaml` at a remote Qdrant/Memgraph instance.

> **Scope note:** REST API / hosted multi-tenant mode is not in scope for Phase 1 or 2. See [roadmap.md](roadmap.md) for future phases.
