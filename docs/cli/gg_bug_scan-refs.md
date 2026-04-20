## gg bug scan-refs

Scan text for BUG-NNN references and auto-reopen any that are fixed

### Synopsis

Parse text (e.g. a commit message) for BUG-NNN patterns. For each
referenced bug that is currently in "fixed" status, automatically reopen it.

Intended to be called from git commit-msg hooks or the pre-task-done hook so
that a commit touching a previously-fixed area triggers a reopen before the
task is marked done.

Exit code 0 even when bugs are reopened — check stdout for the list of
affected bug IDs.

```
gg bug scan-refs "text" [flags]
```

### Options

```
  -h, --help     help for scan-refs
      --silent   suppress output when no bugs are reopened
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

