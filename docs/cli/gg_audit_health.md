## gg audit health

Reopen rate + surface pressure metrics for quality trend analysis

### Synopsis

Compute two stability signals from bug history:

  reopen_rate_7d      = reopens / (reopens + fresh_closes)
  surface_pressure_p95 = p95 of distinct files touched per bug fix

Both are non-blocking observations. Thresholds are configurable in
.gg/config.yaml under audit.thresholds. Defaults:
  reopen_rate_warn:      0.20  (>20%  → "stabilize before adding")
  reopen_rate_freeze:    0.40  (>40%  → "freeze new features")
  surface_pressure_p95:  3     (>3    → "centralize common state")

```
gg audit health [flags]
```

### Options

```
      --days int   look-back window in days (default 7)
  -h, --help       help for health
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
