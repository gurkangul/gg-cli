## gg bug reindex

Replay bug AFFECTS edges into Memgraph

### Synopsis

Rebuild Bug→File and Bug→Symbol edges in Memgraph from the Qdrant store.

Use this to heal drift that occurs when Memgraph was unreachable during
gg bug report — Qdrant holds the authoritative affected_files /
affected_symbols lists; reindex replays them into the graph.

By default all bugs with at least one affected file or symbol are
replayed. Use --since to limit to bugs updated on or after a date
(RFC 3339, e.g. 2026-04-01T00:00:00Z).

```
gg bug reindex [flags]
```

### Options

```
  -h, --help           help for reindex
      --since string   only reindex bugs updated on or after this RFC 3339 timestamp
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

