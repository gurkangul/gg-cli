## gg task create

Create a new task

```
gg task create "title" [flags]
```

### Options

```
      --depends-on string   comma-separated task IDs this task depends on (e.g. TASK-001,TASK-002)
      --detail string       task description
      --from string         author/role recording this (defaults to $GG_ROLE)
  -h, --help                help for create
      --priority string     priority: high, medium, low (default "medium")
      --tags string         comma-separated tags
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

