# Getting Started with gg

## Prerequisites

1. **Docker with Compose v2** — `gg init` starts Qdrant, Ollama, and
   Memgraph through the shared `~/.gg/docker-compose.yaml`.

2. **Go** — used to install the CLI:
   ```sh
   go install github.com/gurkangul/gg-cli/cmd/gg@latest
   ```

   For private alpha access:
   ```sh
   gh auth login
   go env -w GOPRIVATE=github.com/gurkangul/gg-cli
   go install github.com/gurkangul/gg-cli/cmd/gg@latest
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

Install agent hooks so Claude/Codex/Cursor sessions automatically load the
session-start briefing:

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

`gg doctor --install-agent-hooks` installs the managed agent instructions for
detected tools. Each agent should run `gg session-start` at the start of every
session.

Set `GG_AGENT` / `GG_ROLE` in each agent's environment so messages are attributed:

```sh
export GG_AGENT=codex
export GG_ROLE=developer   # or architect, reviewer, etc.
gg session-start --agent=codex
```

For master/worker flows:

```sh
gg become master
GG_ROLE=master gg spawn heartbeat --watch --poll 90 &
```

## Index your codebase (optional)

For impact analysis (`gg impact <file>`) and code context, index your code:

```sh
# Install the SCIP indexer for your language
gg doctor --install-indexers

# Index the codebase
gg index --lang go

# After changes, do an incremental update
gg index --lang go --changed
```

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
