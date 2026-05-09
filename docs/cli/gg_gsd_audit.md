## gg gsd audit

Audit GSD scratchpad items against gg durable work

### Synopsis

Scans .gsd/gsd.db for T-level tasks and compares them against gg tasks
tagged "gsd". GSD is allowed as a local scratchpad/helper; this audit is
advisory and highlights GSD tasks that may need a durable gg task.

Exit codes:
  0  audit completed


```
gg gsd audit [flags]
```

### Options

```
  -h, --help             help for audit
      --project string   path to project root (default: current directory) (default ".")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg gsd](gg_gsd.md)	 - GSD integration utilities

