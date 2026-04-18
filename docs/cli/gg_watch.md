## gg watch

Tail inbox messages and event stream

### Synopsis

Stream new inbox messages and telemetry events to stdout.

Designed as a hook source for external pipes: tmux status bars, desktop
notification scripts, or other agents polling for activity.

Does not require a long-running server — reads the existing NDJSON telemetry
file and polls the inbox DB at ~1 second cadence. Safe to Ctrl-C at any time;
messages are not marked as read.

Examples:
  gg watch                             # stream everything, pretty format
  gg watch --role qa                   # only messages addressed to 'qa'
  gg watch --event tell                # only 'tell' telemetry events
  gg watch --since 5m                  # replay last 5 min, then tail
  gg watch --format ndjson             # machine-readable NDJSON output
  gg watch --no-inbox                  # telemetry events only
  gg watch --no-telemetry              # inbox messages only

```
gg watch [flags]
```

### Options

```
      --event string    filter telemetry events by verb (e.g. tell, task)
      --format string   output format: pretty or ndjson (default "pretty")
  -h, --help            help for watch
      --no-inbox        skip inbox messages, show telemetry events only
      --no-telemetry    skip telemetry events, show inbox messages only
      --role string     filter inbox messages by recipient role
      --since string    replay events from this duration back on startup (e.g. 5m, 2h)
      --tag string      filter messages whose content contains this tag/string
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

