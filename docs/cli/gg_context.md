## gg context

Fetch a unified context bundle for a topic or task

### Synopsis

Searches decisions, rejections, tasks, and discussions for the given topic
using semantic similarity and returns a bundled context package for agent consumption.

Use --for-task TASK-NNN to get a task-scoped context bundle: the task itself,
its dependencies, and semantically related decisions/rejections, useful for
resuming work after a conversation compaction.

```
gg context [topic] [flags]
```

### Options

```
      --budget int         token budget: emit P1 items first, drop lower-priority tiers when over budget (0 = no limit; implies --compact)
      --compact            emit one line per item — drops reasons/details/transcripts to preserve agent context window
      --for-task string    task-scoped rehydration: fetch the task, its dependencies, and related decisions/rejections
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

