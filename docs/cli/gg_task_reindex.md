## gg task reindex

Replay Task nodes into Memgraph from the Qdrant task store

### Synopsis

Rebuild Task nodes in Memgraph from the Qdrant task store.

Use this to heal drift that occurs when (a) Memgraph was unreachable during
gg task create, or (b) tasks were created before gg started dual-writing
(TASK-225). Qdrant holds the authoritative task list; reindex upserts a
matching Task node for each one, idempotently.

Only node identity (qdrant_id + title) is mirrored. Status, priority,
tags, and author remain in Qdrant — Memgraph Task nodes exist to
participate in graph traversal (DECIDES / IMPLEMENTS / IN_WAVE edges).

```
gg task reindex [flags]
```

### Options

```
  -h, --help   help for reindex
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

