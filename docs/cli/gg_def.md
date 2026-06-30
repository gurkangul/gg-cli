## gg def

Find where a symbol is defined (code graph, offline)

### Synopsis

Resolve a symbol name to where it is defined, using the code graph.

This is the grep-free answer to "where is X defined": it returns the defining
file and kind for every Symbol node matching the name (a name can be defined in
more than one file). It reads the embedded graph (.gg/graph.db) — no language
server required — so it is the offline complement to 'gg lsp def', which is the
exact, live oracle when a server is running.

An empty result is reported explicitly, not as a silent "not found": when the
graph is missing or unbuilt the symbol may still exist, so run 'gg index' before
treating an empty result as proof the symbol does not exist.

Requires the code graph (gg index must have been run).

```
gg def <symbol-name> [flags]
```

### Options

```
  -h, --help   help for def
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
