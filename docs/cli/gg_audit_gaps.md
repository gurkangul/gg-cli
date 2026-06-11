## gg audit gaps

List files with recent git commits but no gg record/decision/task coverage

### Synopsis

Walk git log for the look-back window (default 7d) and report files
that were committed but never referenced in any gg task, decision, or record.

Use this as a weekly retrospective companion to gg audit report (which fires
live at session end). Helps maintainers spot knowledge-capture gaps without
reading every commit.

```
gg audit gaps [flags]
```

### Options

```
      --compact         one line per gap — no coverage details
  -h, --help            help for gaps
      --include-all     include generated, test fixture, and binary/coverage noise
      --include-noise   alias for --include-all
      --since string    look back window (e.g. 7d, 14d, 30d) (default "7d")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
