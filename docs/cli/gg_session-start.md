## gg session-start

Print session bootstrap briefing (called by agent SessionStart hooks)

### Synopsis

Print the session-start briefing for an AI agent entering this project.

This is the canonical entrypoint used by agent SessionStart hooks installed
via `gg doctor --install-agent-hooks`. The output is a stable,
machine-parseable briefing followed by the current project state.

Enforcement:
  --agent=NAME must be provided, or GG_AGENT must be set in the environment.
  Otherwise the command exits with code 3 (config error) — a silent skip
  would defeat the point of enforcement.

Output layout:
  Line 1:   gg:session-start:v1     (stable marker for tooling)
  Then:     agent + project metadata
  Then:     4-line protocol summary
  Then:     current gg status output

Examples:
  gg session-start --agent=<agent-name>
  GG_AGENT=cursor gg session-start

```
gg session-start [flags]
```

### Options

```
      --agent string   agent name (codex, cursor, gsd, aider, ...) — overrides $GG_AGENT
      --bench          print timing for the managed-block resync step to stderr
  -h, --help           help for session-start
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
