## gg task block

Mark a task blocked — state what specific dependency is missing

### Synopsis

Signal that work is stalled because of an external dependency or unresolved question.

WHEN TO USE: you cannot make progress without input from another agent or an external
resource. The reason is stored and shown in 'gg status --blocked'.

WHEN NOT TO USE: for long-term deprioritization, update priority instead.

```
gg task block TASK-ID "reason" [flags]
```

### Options

```
  -h, --help   help for block
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

