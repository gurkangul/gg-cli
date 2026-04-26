## gg spawn queue

Manage the parallel task queue for multi-agent orchestration

### Synopsis

Control the parallel queue runner that drains pending tasks by spawning workers.

Subcommands:
  start   — begin a new queue run (drains pending tasks in parallel)
  pause   — suspend the running queue after the current worker finishes
  resume  — resume a paused queue from where it stopped
  status  — show queue state, current task, completed/skipped counts
  cancel  — abort the queue run and remove queue.json
  skip    — mark the current (or named) task as skipped and advance
  check   — verify queue health (heartbeat freshness, panes liveness)

### Options

```
  -h, --help   help for queue
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness
* [gg spawn queue cancel](gg_spawn_queue_cancel.md)	 - Abort the queue run and remove queue.json
* [gg spawn queue check](gg_spawn_queue_check.md)	 - Verify queue health: heartbeat freshness and pane liveness
* [gg spawn queue pause](gg_spawn_queue_pause.md)	 - Suspend the queue after the current worker finishes
* [gg spawn queue resume](gg_spawn_queue_resume.md)	 - Resume a paused queue run from where it stopped
* [gg spawn queue skip](gg_spawn_queue_skip.md)	 - Mark the current (or named) task as skipped and advance the queue
* [gg spawn queue start](gg_spawn_queue_start.md)	 - Begin a new parallel queue run
* [gg spawn queue status](gg_spawn_queue_status.md)	 - Show queue state: current task, completed/skipped counts, panes

