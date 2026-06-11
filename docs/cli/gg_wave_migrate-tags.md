## gg wave migrate-tags

Dry-run tag-to-wave migration (--apply to execute)

### Synopsis

Scan tasks with wave* tags (e.g. wave1, wave2) and report which would be
assigned to Wave nodes. By default this is a dry-run — no Memgraph writes.
Pass --apply to create Wave nodes and write IN_WAVE edges.

```
gg wave migrate-tags [flags]
```

### Options

```
      --apply   write Wave nodes and IN_WAVE edges (default: dry-run report only)
  -h, --help    help for migrate-tags
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg wave](gg_wave.md)	 - Manage optional wave/milestone buckets — sprints (Memgraph only)
