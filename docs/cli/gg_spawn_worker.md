## gg spawn worker

Open a new terminal pane and run an agent against a task

### Synopsis

Spawn a worker agent in a new terminal pane.

The worker pane inherits the current environment (GG_AGENT, GG_ROLE, etc.)
plus any additional KEY=VALUE pairs supplied via --env. A startup command is
sent to the pane to orient the agent: it exports GG_AGENT, exports
GG_TASK_ID, and runs 'gg task get <task-id>' to load task context.

When --task is provided, gg checks task state before opening a pane. Blocked,
done, ready_for_live, and dependency-blocked tasks are refused so agents do not
start the wrong lifecycle step.

The spawned pane is registered in the runtime spawn directory so
'gg spawn status' can list active workers.

Requires a terminal backend (GG_TERMINAL=cmux is default when cmux is in PATH).

```
gg spawn worker [flags]
```

### Options

```
      --agent string      agent command to run in the new pane (default: $GG_SPAWN_AGENT or developer.command)
      --env stringArray   KEY=VALUE env vars to set in the worker pane (repeatable)
  -h, --help              help for worker
      --role string       role command to launch: developer or reviewer (default "developer")
      --split string      pane split direction: horizontal (below) or vertical (right, default) (default "vertical")
      --task string       task ID to assign to this worker (e.g. TASK-042)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness

