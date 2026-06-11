## gg audit trends

Quality-signal metrics: bug reopen rate over a lookback window

### Synopsis

Summarise the project's recent stability signals without blocking any
workflow — this is a diagnostic, not a gate.

Metrics:
  reopen_rate = reopens / (reopens + fresh_closes), last --since window
    < 20% healthy; 20–40% stabilise before adding features; > 40% feature-freeze.

Surface pressure (distinct files touched per task close) is a planned metric
tracked by a follow-up task once the git-integration path is in.

Use: run weekly during dogfood reviews, or before deciding to add new CLI
surface to a project already in a bug treadmill (the signal that motivated
this command per TASK-223).

```
gg audit trends [flags]
```

### Options

```
  -h, --help           help for trends
      --since string   lookback window (e.g. 7d, 14d, 30d) (default "7d")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
