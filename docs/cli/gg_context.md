## gg context

Fetch a unified context bundle for a topic

### Synopsis

Searches decisions, rejections, tasks, and discussions for the given topic
using semantic similarity and returns a bundled context package for agent consumption.

Phase 2 will add Memgraph structural queries (affected files/symbols).

```
gg context "topic" [flags]
```

### Options

```
      --full               print full deliberation transcript for each discussion
  -h, --help               help for context
      --include-resolved   include resolved/dismissed discussions and done/blocked tasks
      --limit uint         max results per collection (default 5)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

