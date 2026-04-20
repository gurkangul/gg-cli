## gg bug reopen

Reopen a fixed or wontfix bug

### Synopsis

Transition a fixed or wontfix bug back to "reopened" status.

The reason is appended to the bug's reopen_reasons history and the
reopen_count is incremented. Use this when a fix regressed or a wontfix
decision is reversed.

The auto-reopen trigger (scan-refs) calls this automatically when a commit
message references a fixed bug.

```
gg bug reopen BUG-ID "reason" [flags]
```

### Options

```
  -h, --help   help for reopen
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

