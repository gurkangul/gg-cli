## gg spawn supervisor

Route gg messages to live worker panes

### Synopsis

Run an explicit foreground dispatcher that watches inbox messages and
forwards matching role-targeted instructions into worker panes.

No daemon is created. The command only runs while this foreground process is
active and stops immediately on Ctrl-C.

### Options

```
  -h, --help   help for supervisor
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness
* [gg spawn supervisor watch](gg_spawn_supervisor_watch.md)	 - Watch inbox and trigger matching worker panes

