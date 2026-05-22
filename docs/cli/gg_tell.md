## gg tell

Send a message to one or more agent roles

### Synopsis

Send a message to one or more agent roles.

Targets can be comma-separated for fanout:
  gg tell qa,reviewer "TASK-042 ready for review"

@role mentions in the message body are auto-routed in addition to the primary target:
  gg tell all "@qa please review before merging"

Use --audience to control inbox visibility:
  gg tell all "TASK-016 picked up" --from developer --audience agents
  gg tell human "deploy is blocked, need approval" --from developer --audience human

```
gg tell "role[,role2,...]" "message" [flags]
```

### Options

```
      --audience string   visibility: all | human | agents (agents = filtered from human inbox by default) (default "all")
      --from string       sender role (defaults to $GG_ROLE, then 'user')
  -h, --help              help for tell
      --task string       related task ID
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
