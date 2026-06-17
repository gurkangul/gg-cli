## gg brain reindex-decisions

Replay Decision nodes into the code graph from the decision store

### Synopsis

Rebuild Decision nodes in the code graph from the decision store.

Symmetric with `gg task reindex` and `gg bug reindex`. Lists every
decision in the store and upserts a matching Decision node in the code graph
so historical decisions (created before TASK-228 shipped, or when the graph
was unavailable) participate in graph traversal and `gg impact` queries.

Only node identity (id + text) is mirrored. Status, author,
tags, and reasons remain in the decision store — code-graph Decision nodes
exist to anchor DECIDES / REJECTS / IMPLEMENTS edges.

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
