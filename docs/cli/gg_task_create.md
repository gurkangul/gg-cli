## gg task create

Create a task to track a discrete unit of work

### Synopsis

Create a task in the shared brain. Tasks coordinate work across agents.

WHEN TO USE: you have a concrete action item — something that can be done, verified,
and marked done. Use --priority to signal urgency and --depends-on to declare ordering.

WHEN NOT TO USE: for open-ended exploration use 'gg record'; for async questions to
another agent use 'gg message send'.

See also: gg task list (view tasks), gg task done (close a task), gg task deps (check blockers)

```
gg task create "title" [flags]
```

### Options

```
      --blocks string       comma-separated task IDs that this task is blocking
      --deadline string     deadline date (YYYY-MM-DD)
      --depends-on string   comma-separated task IDs this task depends on (e.g. TASK-001,TASK-002)
      --detail string       task description
      --from string         author/role recording this (defaults to $GG_ROLE)
  -h, --help                help for create
      --priority string     priority: high, medium, low (omit to leave unset)
      --requester string    who initiated this task: user, agent, or system (required)
      --tags string         comma-separated tags
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

