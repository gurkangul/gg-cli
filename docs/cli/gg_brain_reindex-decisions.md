## gg brain reindex-decisions

Replay Decision nodes into Memgraph from the Qdrant decision store

### Synopsis

Rebuild Decision nodes in Memgraph from the Qdrant decision store.

Symmetric with `gg task reindex` and `gg bug reindex`. Lists every
decision in Qdrant and upserts a matching Decision node in Memgraph so
historical decisions (created before TASK-228 shipped, or when Memgraph
was unreachable) participate in graph traversal and `gg impact` queries.

Only node identity (qdrant_id + text) is mirrored. Status, author,
tags, and reasons remain in Qdrant — Memgraph Decision nodes exist to
anchor DECIDES / REJECTS / IMPLEMENTS edges.

```
gg brain reindex-decisions [flags]
```

### Options

```
  -h, --help   help for reindex-decisions
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
