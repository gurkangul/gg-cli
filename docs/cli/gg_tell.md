## gg tell

Send a message to one or more agent roles

### Synopsis

Send a message to one or more agent roles.

Targets can be comma-separated for fanout:
  gg tell qa,reviewer "TASK-042 ready for review"

@role mentions in the message body are auto-routed in addition to the primary target:
  gg tell all "@qa please review before merging"

```
gg tell "role[,role2,...]" "message" [flags]
```

### Options

```
      --from string   sender role (defaults to $GG_ROLE, then 'user')
  -h, --help          help for tell
      --task string   related task ID
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

