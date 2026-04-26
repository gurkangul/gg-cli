## gg spawn advance

Write a worker-ready sentinel for a task

### Synopsis

Signal the master heartbeat loop that this worker has committed and is
awaiting review. Writes (or overwrites) a JSON sentinel at:

  ~/.gg/projects/<project_id>/spawn/advance/TASK-NNN.done

The sentinel records {task_id, surface_id, commit_sha, written_at}. The
master's heartbeat --watch loop polls this directory and transitions the
pane to state=ready when it finds the sentinel.

Idempotent: safe to call on amend — the sentinel is simply overwritten with
the new commit SHA.

Typical usage after commit:

  git commit -m "..." && gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)

```
gg spawn advance --task TASK-NNN [--commit <sha>] [flags]
```

### Options

```
      --commit string   commit SHA to record in the sentinel (optional)
  -h, --help            help for advance
      --task string     task ID this worker has completed (e.g. TASK-042)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness

