## gg reject

Record a rejected approach (deprecated: use gg record --stance=reject)

### Synopsis

Record a rejected approach.

DEPRECATED: use 'gg record --stance=reject' instead.
This command will be removed in a future release.

  gg record --stance=reject "approach" --reason "why"

```
gg reject "approach" [flags]
```

### Options

```
      --from string     author/role recording this (defaults to $GG_ROLE)
  -h, --help            help for reject
      --reason string   why this approach was rejected
      --tags string     comma-separated tags
      --task string     related task ID
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

