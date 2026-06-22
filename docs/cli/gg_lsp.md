## gg lsp

Live, type-aware code intelligence via a language server

### Synopsis

Query a running language server for EXACT, type-aware, never-stale code
intelligence — references, definitions, and hover — for a symbol at a precise
file position. Unlike gg's indexed code graph (only as fresh as the last
index), lsp answers from a language server launched for this one invocation.

  gg lsp refs  <file> <line> <col>   callers/usages of the symbol
  gg lsp defn  <file> <line> <col>   the symbol's definition location(s)
  gg lsp hover <file> <line> <col>   the symbol's signature / documentation

line and col are 1-based (editor convention). The language server is resolved
by file extension (.go → gopls); per-invocation only — no daemon.

### Options

```
  -h, --help   help for lsp
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg lsp defn](gg_lsp_defn.md)	 - Jump to the definition of the symbol at a position
* [gg lsp hover](gg_lsp_hover.md)	 - Show the signature/documentation of the symbol at a position
* [gg lsp refs](gg_lsp_refs.md)	 - Find references (callers/usages) of the symbol at a position
