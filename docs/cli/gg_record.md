## gg record

Record a decision or rejected approach

### Synopsis

Record a decision (default) or a rejected approach.

  gg record "use JWT for auth"                         # accepted decision
  gg record --stance=reject "store sessions in Redis"  # rejected approach

This is the canonical verb. 'gg decide' and 'gg reject' still work but are
deprecated and will be removed in a future major release.

```
gg record "text" [flags]
```

### Options

```
      --from string     author/role recording this (defaults to $GG_ROLE)
  -h, --help            help for record
      --reason string   why this decision was made (or rejected)
      --stance string   stance: "accept" (decision) or "reject" (rejection) (default "accept")
      --tags string     comma-separated tags
      --task string     related task ID
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

