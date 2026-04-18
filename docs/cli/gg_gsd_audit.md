## gg gsd audit

Audit GSD ↔ gg task mirrors — report missing per-T-task gg mirrors

### Synopsis

Scans .gsd/gsd.db for T-level tasks and compares them against gg tasks
tagged "gsd". Each GSD task should have exactly one gg task mirror whose
title contains [GSD:<milestone_id>-<slice_id>-<task_id>].

Exit codes:
  0  all GSD tasks are mirrored in gg
  1  drift found (missing mirrors)


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

