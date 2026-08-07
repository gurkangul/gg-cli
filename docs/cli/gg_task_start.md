## gg task start

Claim a task and move it to in_progress

### Synopsis

Claim a task for one agent and attach a time-bounded lease.

WHEN TO USE: an agent is actively taking ownership of a pending task. The
claim is stored on the task and visible in task list/get so other agents avoid
colliding with the same work.

Existing active claims are refused unless the lease has expired.

A successful claim also prints an === Related Context === block: the top-3
decisions, rejected approaches, and notes semantically related to this task.
Claiming is the moment the topic is known, so prior decisions are pushed here
rather than left to a flag the agent has to remember. The block is capped at
~800 tokens and never fails the claim — if the vector store or embedder is
unavailable it degrades to a one-line notice. Use --no-context to suppress it.

```
gg task start TASK-ID [flags]
```

### Options

```
  -h, --help             help for start
      --lease duration   claim lease duration (for example 30m, 2h) (default 30m0s)
      --no-context       suppress the === Related Context === block (for scripted/CI callers)
      --owner string     agent taking the claim (defaults to $GG_AGENT / $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
