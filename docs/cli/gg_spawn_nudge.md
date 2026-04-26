## gg spawn nudge

Wake an idle worker pane and deliver a prompt

### Synopsis

Send a prompt to a worker pane, ensuring a new agent turn starts.

When the target pane has finished its current turn (agent REPL is idle),
a bare Enter is sent first to wake the stdin handler before the real text
is delivered.  When the pane is mid-turn (agent is actively working), the
prompt is sent directly and queued as a Steering message — no observable
difference for the running agent.

This is the canonical dual-write complement to 'gg tell': use 'gg tell'
to record the message in the knowledge base, then 'gg spawn nudge' to
actually trigger the worker.

Example:
  gg spawn nudge --surface surface:46 "Fix the gap in TASK-292: missing error wrap"

```
gg spawn nudge --surface <id> <text> [flags]
```

### Options

```
  -h, --help             help for nudge
      --surface string   target pane surface ID (required)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness

