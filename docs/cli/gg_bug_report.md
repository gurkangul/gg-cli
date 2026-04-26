## gg bug report

Report a new bug

```
gg bug report "title" [flags]
```

### Options

```
      --detail string     detailed description
      --files string      comma-separated source file paths this bug affects
      --force             skip duplicate-detection prompt and file anyway
      --from string       author/role recording this (defaults to $GG_ROLE)
  -h, --help              help for report
      --severity string   severity: critical, high, medium, low (default "medium")
      --symbols string    comma-separated symbol names this bug affects
      --tags string       comma-separated tags
      --task string       link to a task (e.g. TASK-042)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

