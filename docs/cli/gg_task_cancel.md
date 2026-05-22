## gg task cancel

Permanently remove an accidental or probe task

### Synopsis

Remove a task that should never have existed — probes, accidents, duplicates.

DISTINCT FROM task done: cancel is "this task should not have existed", not
"this work is complete". It bypasses the verifier-separation gate because
there is no work to verify.

Removes the Qdrant point and the Memgraph Task node + all its edges.

--reason is required to prevent accidental use.

```
gg task cancel TASK-ID [flags]
```

### Options

```
  -h, --help            help for cancel
      --reason string   why this task is being cancelled (required)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
