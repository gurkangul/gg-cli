## gg reembed

Migrate all Qdrant collections to the currently configured embedding model

### Synopsis

Drops and recreates all project Qdrant collections at the dimension of
the currently configured embedding model, then re-embeds every stored point.

Use this when you change the embedding model in .gg/config.yaml. Without
re-embedding, vectors from different models will be mixed in the same
collections, which breaks semantic search recall.

WARNING: This operation drops all collections before recreating them.
If the process is interrupted after the drop, stored knowledge (decisions,
tasks, notes, etc.) will be lost. Back up your Qdrant data if it matters.

Requires --confirm to proceed.

```
gg reembed [flags]
```

### Options

```
      --confirm   required: confirms you understand existing vector data will be dropped and rebuilt
  -h, --help      help for reembed
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
