## gg task unblock

Clear a task's blocked state and return it to in_progress

### Synopsis

Return a blocked task to active work — the non-destructive inverse of 'gg task block'.

WHEN TO USE: the dependency a task was blocked on has cleared and you want to
resume it. Moves the task from blocked back to in_progress under the caller with
a fresh lease and clears the stored block reason.

Refused if the task is not blocked, or if another agent holds an active lease.

```
gg task unblock TASK-ID [flags]
```

### Options

```
  -h, --help             help for unblock
      --lease duration   claim lease duration (for example 30m, 2h) (default 30m0s)
      --owner string     agent resuming the task (defaults to $GG_AGENT / $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
