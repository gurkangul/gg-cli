## gg impact

Show downstream impact of changing a file, or blast radius of a bug or task

### Synopsis

Show what a change to the given source file affects, or what a bug/task touches.

File mode (default):
  - Files that directly import it (1-hop dependents from the code graph)
  - Symbols the file exports (boundary symbols)
  - Decisions, tasks, and rejections related to the file (semantic search)
  - Historical bugs that have affected this file (Bug→File graph edges)

Bug mode (BUG-NNN argument):
  - Files and symbols the bug affects (Bug→File/Symbol graph edges)
  - Decisions, tasks, and rejections related to the bug (semantic search)

Task mode (TASK-NNN argument):
  - Downstream dependents (tasks that DEPENDS_ON this one, or are BLOCKED by it)
  - Decisions and related tasks from the knowledge store (semantic search)

Requires Memgraph (gg index must have been run). The knowledge-store search
works even without Memgraph.

```
gg impact <file|BUG-NNN|TASK-NNN> [flags]
```

### Options

```
      --compact         one line per item — drops symbol kinds and reasons to preserve agent context window
      --depth int       alias for --hops (default 1)
      --file string     source file used to disambiguate --symbol or --flow
      --flow string     render bounded forward CALLS flow for a symbol name; use --depth N and --file if ambiguous
  -h, --help            help for impact
      --hops int        max downstream dependency hops to traverse in file mode (default 1)
      --kb-limit uint   max results per knowledge-base collection (default 5)
      --symbol string   show impact for a symbol name; use --file to disambiguate duplicates
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
