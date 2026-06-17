## gg reembed

Rebuild the embedded vector index (.gg/vectorstore.db) from .gg/brain/*.jsonl

### Synopsis

Drops and recreates the local vector index (.gg/vectorstore.db) at the
dimension of the currently configured embedding model, then re-embeds every
record from the durable brain (.gg/brain/*.jsonl).

Use this when you change the embedding model in .gg/config.yaml. Without
re-embedding, vectors from different models will be mixed in the same
collections, which breaks semantic search recall.

This rebuilds the local vector index from .gg/brain/*.jsonl (the durable source
of truth, which is never modified). Safe to re-run.

Requires --confirm to proceed.

```
gg reembed [flags]
```

### Options

```
      --confirm   required: confirms you understand existing vector data will be dropped and rebuilt
  -h, --help      help for reembed
      --yes       alias for --confirm (canonical destructive-confirm flag)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
