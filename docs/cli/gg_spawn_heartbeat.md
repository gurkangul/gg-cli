## gg spawn heartbeat

Record master session liveness

### Synopsis

Write a heartbeat timestamp to the runtime dir so worker sessions can verify
the master is still alive before closing tasks.

Call this once to register liveness and check registered worker panes. For a
persistent master session, run with --watch from the master terminal; this keeps
refreshing liveness and re-checking worker panes until interrupted. Hook-driven
worker pings should keep the default one-shot mode.

The worker-liveness-check hook installed by 'gg doctor --install-task-hooks'
reads this file and blocks 'gg task done' when the master heartbeat is stale
(> 5 min old). Set GG_NO_MASTER_GUARD=1 in worker sessions to bypass the
liveness check.

The 46-worker-heartbeat.sh hook calls this with --worker to ping the master
from a worker pane at task-completion boundaries (best-effort).

```
gg spawn heartbeat [flags]
```

### Options

```
  -h, --help            help for heartbeat
      --keepalive int   seconds between noop keepalive sends to worker panes (default 240, or GG_PANE_KEEPALIVE_SEC)
      --poll int        seconds between checks when --watch is set (default 60)
      --watch           keep refreshing heartbeat and checking registered worker panes until interrupted
      --worker          ping originates from a worker pane (informational)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness

