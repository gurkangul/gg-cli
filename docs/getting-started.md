# Getting Started with gg

## Prerequisites

1. **An embedding provider** — native Ollama is the default:
   ```sh
   brew install ollama          # or https://ollama.com/download
   ollama serve &
   ollama pull nomic-embed-text   # default; any embedding model works — override per-shell with GG_EMBED_MODEL
   ```
   Alternatively, opt into the Voyage cloud backend (set `embedding.backend: voyage`
   in `.gg/config.yaml` and export `VOYAGE_API_KEY`).

   **Docker is NOT required.** The vector store (`.gg/vectorstore.db`) and graph
   store (`.gg/graph.db`) are embedded SQLite — there is no server backend to run.

2. **Go** — used to install the CLI:
   ```sh
   go install github.com/gurkangul/gg-cli/cmd/gg@latest
   ```

   If the first tagged release is not available yet, install from the current
   main branch instead:
   ```sh
   go install github.com/gurkangul/gg-cli/cmd/gg@main
   ```

## Initialize a project

Navigate to your project root and run:

```sh
gg init
```

This creates `.gg/config.yaml` with default settings. Edit it if your services are on non-default ports.

Run `gg doctor` to verify everything is connected:

```sh
gg doctor
```

Install agent hooks so Claude/Codex/Cursor sessions can load the durable-memory
briefing:

```sh
gg doctor --install-agent-hooks
```

## First use

```sh
# Record your first decision
gg record "use PostgreSQL for the user database" --reason "team familiarity, ACID compliance" --tags "database"

# Create a task
gg task create "set up database migrations" \
  --priority high \
  --detail "use golang-migrate" \
  --requester user

# Search for context
gg search "database"

# Check status
gg status
```

## Setting up for multi-agent use

gg does not replace each agent's native workflow. Set `GG_AGENT` / `GG_ROLE` in
each agent's environment so durable records are attributed to the runtime that
actually writes them. Do not copy another runtime's example: a real GSD shell
should use a unique `gsd-*` id, while a host agent relaying GSD output should
keep the host agent id.

```sh
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-developer}"   # or architect, reviewer, etc.
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
```

Use `gg record`, `gg task`, `gg bug`, and `gg tell` when a durable decision,
rejection, shared work item, root cause, evidence summary, blocker, or handoff
must be visible to future agents.

## Index your codebase (optional)

For impact analysis (`gg impact <file>`) and code context, index your code:

```sh
# Install maintained SCIP indexers for Go, TypeScript, and Python
gg doctor --install-indexers

# Index the codebase
gg index --lang go

# After changes, do an incremental update
gg index --lang go --changed

# Or keep an explicit foreground watcher open until Ctrl-C
gg index --watch --lang go
```

Supported `--lang` values are `go`, `python`, `swift`, and `typescript`.
Swift is an externally-backed path: gg can detect SwiftPM and Xcode projects,
track `.swift`/manifest freshness, and ingest SCIP output, but it does not
bundle a Swift SCIP generator. Put a compatible `scip-swift` binary on `PATH`
or `~/.gg/bin` using this contract:

```sh
scip-swift index --output <scip-file> <project-root>
```

The converter must exit non-zero on failure, write a valid SCIP file at
`<scip-file>`, and emit document paths either relative to `<project-root>` or
absolute paths under the gg project root.

CodeGraph freshness is explicit: gg never runs a background index daemon.
Agent-facing commands (`gg session-start`, `gg next`, `gg impact`, `gg doctor`,
and `gg index status`) use the same notice contract and suggest
`gg doctor --fix-index` when repair is needed. `gg index --watch` / `gg watch --index`
are optional foreground active modes, not daemons.

Freshness is based on source files and selected module manifests, not dependency
lockfile churn. Go `go.sum`, npm/pnpm/yarn lockfiles, Poetry `poetry.lock`,
`uv.lock`, and similar lockfiles do not make CodeGraph stale by themselves;
run `gg doctor --fix-index` manually after dependency work if you need a fresh
graph projection.

## Install the verify gate

Once you start marking tasks done, have `gg` reject the transition when
the build or tests fail. Install the starter hooks with one command:

```
gg doctor --install-task-hooks
```

It auto-detects Go (`go.mod`) and Node / Bun (`package.json`), walks
monorepos up to depth 3, and never overwrites existing scripts. On
failure, `gg task done` exits `7`, emits a parseable NDJSON line on
stderr, and broadcasts to all agents — see
[verify-gate.md](verify-gate.md) for the full contract.

## Next steps

- [Command reference](commands.md)
- [Architecture overview](architecture.md)
- [Verify gate contract](verify-gate.md)
- [Project roadmap](roadmap.md)
