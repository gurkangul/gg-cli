## gg related

Walk the link graph outward from a task, bug, or decision

### Synopsis

Show what is CONNECTED to a reference, not merely what sounds similar.

Traverses the real link graph — task_id / depends_on / blocks plus [[wiki links]]
and TASK-NNN / BUG-NNN mentions in prose — outward from <ref>, nearest first.
Traversal is undirected: both what points at the anchor and what it points to are
part of the neighbourhood, with the direction shown per edge.

WHEN TO USE: orienting before a change — "what is entangled with BUG-084?" — or
when semantic search is degraded, since this reads the JSONL ledger directly and
needs neither the vector store nor the code graph.

See also: gg backlinks (one hop, reverse), gg impact (code blast radius)

```
gg related <ref> [flags]
```

### Options

```
      --compact    one line per node — preserves agent context window
  -h, --help       help for related
      --hops int   how many hops to walk outward (default 2)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
