## gg task start

Claim a task and move it to in_progress

### Synopsis

Claim a task for one agent and attach a time-bounded lease.

WHEN TO USE: an agent is actively taking ownership of a pending task. The
claim is stored on the task and visible in task list/get so other agents avoid
colliding with the same work.

Existing active claims are refused unless the lease has expired.

```
gg task start TASK-ID [flags]
```

### Options

```
  -h, --help             help for start
      --lease duration   claim lease duration (for example 30m, 2h) (default 30m0s)
      --owner string     agent taking the claim (defaults to $GG_AGENT / $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
