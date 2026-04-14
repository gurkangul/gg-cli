## gg bug triage

Auto context bundle for fixing a bug

### Synopsis

Fetches the bug, then runs a parallel semantic search across all collections
(decisions, rejections, tasks, discussions, notes) using the bug title as the
query. The result is a bundled context package to prime an agent's fix.

```
gg bug triage BUG-ID [flags]
```

### Options

```
  -h, --help         help for triage
      --limit uint   max results per collection (default 5)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

