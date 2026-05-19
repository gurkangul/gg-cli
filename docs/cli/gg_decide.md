## gg decide

Record a decision or rejection (deprecated: use gg record)

### Synopsis

Record a decision (default) or a rejected approach (--stance=reject).

DEPRECATED: use 'gg record' instead.
This command will be removed in a future major release.

  gg record "use JWT"                                      # accepted decision
  gg record "use sessions" --decision-status=rejected      # rejected approach

```
gg decide "decision text" [flags]
```

### Options

```
      --from string     author/role recording this (defaults to $GG_ROLE)
  -h, --help            help for decide
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

