## gg bug fix

Mark a bug as fixed

```
gg bug fix BUG-ID "summary" [flags]
```

### Options

```
      --files string              comma-separated source file paths affected by this fix
      --from string               author/role recording this (defaults to $GG_ROLE)
  -h, --help                      help for fix
      --repro string              path to repro script or *_test.go that guards against regression (required)
      --repro-broken-ref string   git SHA where the repro MUST fail — proves the bug existed before the fix
      --root-cause string         root cause identified during fix
      --symbols string            comma-separated symbol names affected by this fix
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg bug](gg_bug.md)	 - Manage bug lifecycle
