# Adapters & Integrations

## AI agent adapters

In gg docs, an adapter is an instruction snippet or shell-command convention. It
is not a daemon, RPC bridge, MCP server, hosted sync service, or runtime control
layer.

`gg` works with any agent that can run shell commands. The integration is via `AGENTS.md` — drop it in your project root and agents will find it automatically.

For lightweight guidance on when each supported agent/workflow should mirror
native outputs into gg, see [Native Workflow Capture Points](native-workflow-capture.md).

### Claude Code

Add to your `CLAUDE.md` or include `AGENTS.md` at session start. Claude Code calls `gg` as a subprocess.

### BMAD / host agents

Point the agent's rules file at `AGENTS.md`. In party mode, each role
(architect, developer, reviewer) can use a separate terminal with `GG_ROLE` set:

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

`gg index` uses SCIP indexers to build the code knowledge graph. Maintained
Go, TypeScript, and Python indexers can be installed with
`gg doctor --install-indexers` or manually:

| Language | Indexer | Install / setup |
|---|---|---|
| Go | `scip-go` | `go install github.com/sourcegraph/scip-go/cmd/scip-go@latest` |
| TypeScript | `scip-typescript` | `npm install -g @sourcegraph/scip-typescript` |
| Python | `scip-python` | `npm install -g @sourcegraph/scip-python` |
| Swift | `scip-swift` | External/custom binary required; gg does not auto-install one |

Requires Go 1.21+ for scip-go, Node.js 18+ for scip-typescript and scip-python.
Swift support is externally backed because there is no official maintained
Sourcegraph Swift SCIP indexer. A compatible `scip-swift` must accept:

```sh
scip-swift index --output <scip-file> <project-root>
```

It must exit non-zero on failure, write a valid SCIP file at `<scip-file>`, and
emit document paths either relative to `<project-root>` or absolute paths under
the gg project root.

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

## gg task-lifecycle hooks

Independent of git, `gg` runs its own lifecycle hooks from `.gg/hooks/`:

- **`.gg/hooks/pre-task-done.d/*.sh`** — blocking verify gate. Every
  script runs in lexicographic order before `gg task done` writes the
  new state. Any non-zero exit aborts the transition with exit code `7`
  and keeps the task in its current state. Install starter scripts with
  `gg doctor --install-task-hooks` (auto-detects Go / Node / Bun).
- **`.gg/hooks/task-done.d/*.sh`** — advisory post-hook. Runs after the
  store update succeeds; warnings only unless `hooks.strict: true` is
  set in `.gg/config.yaml`.

Hooks receive `GG_TASK_ID`, `GG_TASK_SUMMARY`, `GG_PROJECT_ID`, and
`GG_ACTOR` as env vars. Full contract and the NDJSON rejection envelope
are documented in [verify-gate.md](verify-gate.md).

## Backend alternatives

The vector and graph stores are embedded SQLite (`.gg/vectorstore.db`, `.gg/graph.db`) — there is no server backend and nothing to point at a remote instance. For shared team setups, use `gg brain export` / `gg brain import` to move a project's memory between machines.

> **Scope note:** REST API / hosted multi-tenant mode is not in scope for Phase 1 or 2. See [roadmap.md](roadmap.md) for future phases.
