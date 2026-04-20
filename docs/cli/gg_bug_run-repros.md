## gg bug run-repros

Run all registered repro scripts for fixed bugs

### Synopsis

Runs every repro script attached to a fixed bug.
Exits 0 if all pass, exits 7 if any fail (regression detected).
Used by .gg/hooks/pre-task-done.d/90-bug-repros.sh.

```
gg bug run-repros [flags]
```

### Options

```
      --budget int   total timeout in seconds for all repros (default 14)
  -h, --help         help for run-repros
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle

