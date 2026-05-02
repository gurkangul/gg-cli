## gg spawn

Multi-agent orchestration: spawn worker panes, run queue, track liveness

### Synopsis

Orchestrate multiple agent sessions from a single master terminal.

The master session owns the queue and maintains a liveness heartbeat.
Worker sessions run individual tasks in isolated terminal panes.

Subcommands:
  worker     — open a new pane and run an agent against a specific task
  queue      — drain pending tasks by spawning sequential workers
  heartbeat  — record master liveness (call every ~60s from master session)
  status     — show active sessions, workers, and heartbeat age

Typical flow:
  # Master terminal — start heartbeat loop and queue runner
  export GG_AGENT=claude-code
  gg spawn heartbeat          # initial heartbeat
  gg spawn queue start --agent gsd  # drains pending tasks

  # Open an interactive GSD pane directly (no queue required)
  gg gsd open

  # Worker terminals are opened automatically by `gg spawn queue`.
  # Workers call `gg spawn heartbeat` via the master-guard hook to
  # ensure the master is still alive before closing tasks.

### Options

```
  -h, --help   help for spawn
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg spawn advance](gg_spawn_advance.md)	 - Write a worker-ready sentinel for a task
* [gg spawn heartbeat](gg_spawn_heartbeat.md)	 - Record master session liveness
* [gg spawn nudge](gg_spawn_nudge.md)	 - Wake an idle worker pane and deliver a prompt
* [gg spawn queue](gg_spawn_queue.md)	 - Manage the parallel task queue for multi-agent orchestration
* [gg spawn status](gg_spawn_status.md)	 - Show spawn session status: heartbeat age, active workers, queue progress
* [gg spawn worker](gg_spawn_worker.md)	 - Open a new terminal pane and run an agent against a task

