## gg impact

Show downstream impact of changing a source file

### Synopsis

Show what a change to the given source file affects.

Reports:
  - Files that directly import it (1-hop dependents from the code graph)
  - Symbols the file exports (boundary symbols)
  - Decisions, tasks, and rejections related to the file (semantic search)

Requires Memgraph (gg index must have been run). The knowledge-store search
works even without Memgraph.

```
gg impact <file> [flags]
```

### Options

```
      --compact         one line per item — drops symbol kinds and reasons to preserve agent context window
  -h, --help            help for impact
      --kb-limit uint   max results per knowledge-base collection (default 5)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

