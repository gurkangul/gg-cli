## gg task packet

Print a reviewer handoff packet for a task

### Synopsis

Print a read-only reviewer handoff packet for a task.

The packet gathers the current task projection, ready_for_live plan, linked
decisions, task-scoped inbox messages, local lifecycle events, and suggested
reviewer commands. It does not approve, reject, mark done, or advance inbox
cursors.

```
gg task packet TASK-ID [flags]
```

### Options

```
  -h, --help   help for packet
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
