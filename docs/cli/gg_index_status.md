## gg index status

Show code graph freshness and quality

### Synopsis

Show CodeGraph freshness using the shared agent-facing contract.

gg never runs a background index daemon. Repair is explicit with
gg doctor --fix-index; optional active foreground mode is gg index --watch or
gg watch --index.

```
gg index status [flags]
```

### Options

```
  -h, --help   help for status
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg index](gg_index.md)	 - Index the codebase into the embedded code graph (.gg/graph.db)
