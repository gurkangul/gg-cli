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

  --role=ROLE is optional. When provided (or GG_ROLE is set), the briefing
  prints role-scoped next steps for the current agent instance.

Output layout:
  Line 1:   gg:session-start:v1     (stable marker for tooling)
  Then:     agent + project metadata
  Then:     4-line protocol summary
  Then:     current gg status output

CodeGraph notices use the shared freshness contract. session-start never runs
background graph refresh; repair is explicit via gg doctor --fix-index, and
foreground active mode is gg index --watch / gg watch --index.

Examples:
  gg session-start --agent=<agent-id> --role=implementer
  GG_AGENT=cursor GG_ROLE=reviewer gg session-start

```
gg session-start [flags]
```

### Options

```
      --agent string   agent_id for this agent instance (for example omo-slim, codex-1, claude-planner) — overrides $GG_AGENT
      --bench          print timing for the managed-block resync step to stderr
  -h, --help           help for session-start
      --role string    agent role for this session (for example implementer, reviewer, planner) — overrides $GG_ROLE in briefing output
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
