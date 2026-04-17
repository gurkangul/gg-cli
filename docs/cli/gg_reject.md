## gg reject

Record a rejected approach (deprecated: use gg record --decision-status=rejected)

### Synopsis

Record a rejected approach.

DEPRECATED: use 'gg record --decision-status=rejected' instead.
Decision.status replaces the separate rejection primitive.
This command will be removed in a future release.

  gg record "approach" --decision-status=rejected --reason "why"
  gg record "use PostgreSQL" --rejected-alternatives "MySQL,SQLite" --reason "..."

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

