## gg search

Find relevant context — semantic search across decisions, tasks, and messages

### Synopsis

Retrieve the most relevant brain records for a query using vector similarity.

WHEN TO USE: before starting work — ask 'has this been decided before?' or 'what context
exists around this area?'. Use --compact when passing results into an agent context window.

WHEN NOT TO USE: for exact-match lookups (task IDs, tag filters) use 'gg task list'.

See also: gg status (project overview), gg task get (task details)

```
gg search "query" [flags]
```

### Options

```
      --compact      one line per item — drops reasons/tags/author to preserve agent context window
  -h, --help         help for search
      --limit uint   max results to return (default 5)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

