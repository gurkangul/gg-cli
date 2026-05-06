## gg spawn queue start

Begin a new parallel queue run

### Synopsis

Drain the pending task queue by spawning worker panes in parallel.

Up to --max-concurrent workers run simultaneously (default: GG_QUEUE_MAX env, else 3). For each
pending task the runner:
  1. Verifies master liveness (heartbeat must be fresh)
  2. Checks advisory file-lock collision (locks.json); blocks on conflict
     unless --force is passed
  3. Opens a worker pane via the terminal backend (GG_TERMINAL)
  4. Sends the task ID to the worker for context loading
  5. When the worker pane exits, advances to the next pending task

Queue state is persisted at ~/.gg/projects/<id>/spawn/queue.json.
Active workers cap: --max-concurrent flag > GG_QUEUE_MAX env var > default 3.

```
gg spawn queue start [flags]
```

### Options

```
      --agent string         agent command for worker panes (default: $GG_SPAWN_AGENT or developer.command)
      --force                override advisory file-lock collisions (logs override, continues spawn)
  -h, --help                 help for start
      --max-concurrent int   max simultaneous workers (default: GG_QUEUE_MAX or 3)
      --max-tasks int        stop after processing this many tasks (0 = no limit)
      --poll int             seconds between liveness checks while a worker is running (default 30)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn queue](gg_spawn_queue.md)	 - Manage the parallel task queue for multi-agent orchestration

