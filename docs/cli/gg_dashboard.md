## gg dashboard

Live worker-pane dashboard (TASK-276 AC3)

### Synopsis

Watch active worker panes in real time.

Columns: pane-id | agent | task | state | elapsed | last-heartbeat | touched-files

Colors:
  green   — working
  yellow  — idle >5m (no heartbeat)
  red     — waiting-on-master (need-input signal)
  magenta — collision-risk (path overlap with another active task)

GG_BELL=1  — ring the terminal bell when a worker enters waiting-on-master.

Press Ctrl-C to exit.

```
gg dashboard [flags]
```

### Options

```
  -h, --help           help for dashboard
      --interval int   refresh interval in seconds (requires --watch) (default 2)
  -w, --watch          refresh every N seconds (default 2)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

