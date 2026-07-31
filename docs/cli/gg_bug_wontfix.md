## gg bug wontfix

Close a bug as won't-fix

```
gg bug wontfix BUG-ID "reason" [flags]
```

### Options

```
      --from string    author/role recording this (defaults to $GG_ROLE, then the agent identity)
  -h, --help           help for wontfix
      --repro string   path to repro script or *_test.go documenting the confirmed failure mode (required)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle
