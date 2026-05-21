## gg next

Recommend the next safe agent command

### Synopsis

Recommend the next safe command for an agent without changing workflow state.

This command is read-only: it does not advance inbox cursors, claim tasks,
review work, or mark tasks done. It only inspects current task/inbox state and
prints explicit commands the agent may choose to run next.

```
gg next [flags]
```

### Options

```
      --agent string   agent id to plan for (defaults to $GG_AGENT)
  -h, --help           help for next
      --role string    role to plan for (defaults to $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
