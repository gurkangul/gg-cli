## gg decisions

List or search decisions

### Synopsis

Surface project decisions.

With a query, runs semantic search over decisions (the same path as
'gg search', filtered to the decisions kind). With no query, lists the most
recent active decisions.

This is the top-level shortcut for the decisions kind. For the decisions
linked to a specific task, use 'gg task decisions TASK-ID'.

```
gg decisions [query] [flags]
```

### Options

```
      --compact              one line per decision — drops reasons/tags/author to preserve agent context window
  -h, --help                 help for decisions
      --include-superseded   include superseded/rejected decisions in results
      --limit uint           max decisions to return (default 10)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
