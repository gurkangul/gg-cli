## gg index

Index the codebase into the Memgraph knowledge graph

### Synopsis

Runs a SCIP indexer on the project and writes the resulting code graph
(Symbol, File, Package nodes and DEFINES/IMPORTS/CALLS edges) to Memgraph.

Without --changed: full re-index of the entire project.
With    --changed: incremental update — only files changed since the last
                   successful index are re-indexed (per CHANGED_CONTRACT.md).

```
gg index [--changed] [--lang go|python|typescript] [flags]
```

### Options

```
      --changed       incremental: re-index only files changed since last index
  -h, --help          help for index
      --lang string   language to index: go, python, typescript (default "go")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

