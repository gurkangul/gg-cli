## gg uses

Find which files use (reference) a symbol — symbol-exact reverse blast-radius

### Synopsis

List the files that reference a symbol, resolved from the code graph.

This is the grep-free, barrel-exact answer to "who uses symbol X". Because it
matches REFERENCES edges to the specific Symbol — not its file — a barrel that
re-exports the symbol (export * from './X') never makes a consumer of a sibling
symbol show up here, which is exactly where 2-hop file-level 'gg impact' over-
reports. For the live, type-aware variant use 'gg lsp refs'.

If the name is defined in more than one file, every definition is reported with
its own referencers; use --file to narrow to one.

REFERENCES edges are written only for the semantic (SCIP) tier, so an empty
result on an unbuilt or syntactic-only graph is not proof the symbol is unused —
run 'gg index' first.

Requires the code graph (gg index must have been run).

```
gg uses <symbol-name> [flags]
```

### Options

```
      --file string   defining source file, to disambiguate a name defined in multiple files
  -h, --help          help for uses
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
