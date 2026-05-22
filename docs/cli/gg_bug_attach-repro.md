## gg bug attach-repro

Attach a repro script to an already-fixed bug

### Synopsis

Backfill the repro_script field on an existing bug. Path must be executable or a *_test.go file.

```
gg bug attach-repro BUG-ID <repro-path> [flags]
```

### Options

```
  -h, --help   help for attach-repro
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle
