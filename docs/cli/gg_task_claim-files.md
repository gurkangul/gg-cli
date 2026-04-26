## gg task claim-files

Advisorily claim file paths for a task (prevents parallel spawn collisions)

### Synopsis

Register an advisory file-lock claim for a task.

The queue runner reads locks.json before spawning each worker. If a new task
claims a path that is already claimed by an active task, the spawn is blocked
with a 'collision: TASK-X already claims <path>' error. Pass --force on
'gg spawn queue start' to override.

Calling claim-files replaces any prior claim for the same task.
Calling with no paths releases the task's claim (same as 'gg task release-files').

Examples:
  gg task claim-files TASK-042 internal/store/store.go cmd/status.go
  gg task claim-files TASK-042           # release all claims for TASK-042
  gg task release-files TASK-042         # alias

```
gg task claim-files <task-id> [path...] [flags]
```

### Options

```
      --force   override existing claims from other tasks (logs collision, writes anyway)
  -h, --help    help for claim-files
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

