# Getting Started with gg

## Prerequisites

1. **Qdrant** — the vector store. Run it locally:
   ```sh
   docker run -p 6334:6334 qdrant/qdrant
   ```

2. **Ollama** — local embedding model:
   ```sh
   # Install from https://ollama.ai
   ollama pull nomic-embed-text
   ```

3. **Memgraph** _(optional, for `gg index`)_ — the code knowledge graph:
   ```sh
   docker run -p 7687:7687 memgraph/memgraph
   ```

4. **gg** — install the CLI:
   ```sh
   go install github.com/gurkangul/gg@latest
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

## First use

```sh
# Record your first decision
gg record "use PostgreSQL for the user database" --reason "team familiarity, ACID compliance" --tags "database"

# Create a task
gg task create "set up database migrations" --priority high --detail "use golang-migrate"

# Search for context
gg search "database"

# Check status
gg status
```

## Setting up for multi-agent use

Add `AGENTS.md` (from [AGENTS.md](../AGENTS.md)) to your project root so agents know to use `gg`. Each agent should run `gg status` at the start of every session.

Set `GG_ROLE` in each agent's environment so messages are attributed:

```sh
export GG_ROLE=developer   # or architect, reviewer, etc.
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

## Next steps

- [Command reference](commands.md)
- [Architecture overview](architecture.md)
- [Project roadmap](roadmap.md)
