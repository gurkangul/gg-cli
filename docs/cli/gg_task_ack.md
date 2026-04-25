## gg task ack

Record a worker acceptance-criteria paraphrase before coding

### Synopsis

Record the worker's acceptance-criteria paraphrase before implementation.

The ACK is stored as a task-linked decision, the task moves to in_progress, and
the paraphrase is sent to claude-code so master can reply ACK-OK or ACK-FIX.

```
gg task ack TASK-ID "AC-1: ...; AC-2: ..." [flags]
```

### Options

```
      --from string   author/role recording this (defaults to $GG_ROLE)
  -h, --help          help for ack
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

