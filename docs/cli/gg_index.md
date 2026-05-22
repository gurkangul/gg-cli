## gg index

Index the codebase into the Memgraph knowledge graph

### Synopsis

Runs a SCIP indexer on the project and writes the resulting code graph
(Symbol, File, Package nodes and DEFINES/IMPORTS edges) to Memgraph.
CALLS flow queries are supported when CALLS edges exist, but the built-in SCIP
parser currently materializes cross-file references as IMPORTS edges.

Without --changed: full re-index of the entire project.
With    --changed: incremental update — only files changed since the last
                   successful index are re-indexed (per CHANGED_CONTRACT.md).
With    --watch: explicit foreground watcher — debounce source/module changes
                   and run index updates until Ctrl-C. gg never starts a
                   background indexing daemon; use gg doctor --fix-index for
                   one-shot repair.

```
gg index [--changed] [--lang go|python|typescript] [flags]
```

### Options

```
      --changed                   incremental: re-index only files changed since last index
  -h, --help                      help for index
      --lang string               language to index: go, python, typescript (default "go")
      --watch                     foreground watch mode: debounce source changes and run incremental index updates
      --watch-debounce duration   foreground watch debounce before indexing (default 2s)
      --watch-poll duration       foreground watch poll interval (default 1s)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg index status](gg_index_status.md)	 - Show code graph freshness and quality
