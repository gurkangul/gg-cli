## gg audit repeat-work

Surface multi-iteration patterns that likely indicate a bug loop

### Synopsis

Scan the knowledge base for hotspots where the same task, bug, file, or
tag cluster has been touched many times in a short window. Advisory only —
exits 0 even when hotspots are found. The goal is to surface 'we've patched
this N times' before a 9-iteration bug loop becomes obvious.

Signals are grouped into three tiers:

  Tier A (strong): same task with >= RECORDS records in RECORDS_WINDOW days,
                   or a bug reopened >= REOPENS times
  Tier B (medium): same source file appears in >= FILES bug reports within
                   WINDOW days
  Tier C (weak):   same non-trivial tag attached to >= TAG records within
                   WINDOW days

Thresholds are configurable under audit.repeat_work.* in .gg/config.yaml.

```
gg audit repeat-work [flags]
```

### Options

```
      --compact   one line per hotspot — drops tier headers and hint lines to preserve agent context window
  -h, --help      help for repeat-work
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - Session mutation audit (called by PostToolUse and Stop hooks)
