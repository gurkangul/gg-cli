## Developer Routing

By default, this session reviews and coordinates; implementation is delegated
to a side-session developer (GSD + Sonnet 4.6 in a separate pane).

### Default developer agent

- **Runtime:** GSD workflow on Claude Sonnet 4.6
- **Spawn:** `gg spawn worker --task TASK-N`
- **Nudge:** `gg spawn nudge --surface <pane-id> "<prompt>"`

### Side-session spawn pattern

```
gg spawn worker --task TASK-N
```

This bootstraps a GSD+Sonnet-4.6 session in a new pane and sends the task
prompt automatically. The pane ID is registered in
`~/.gg/projects/<project_id>/spawn/panes.json`.

To send a follow-up prompt to an already-running pane:

```
gg spawn nudge --surface <pane-id> "<prompt text>"
```

### Fallback — no developer configured

If no developer agent is configured, fall back to implementing directly in
this session. When implementing directly:
- Read the task spec fully before writing any code.
- Run `go test ./... -count=1 -race` before closing the task.
- Record any spec pivots via `gg record` before deviating.

### Trivial bypass rule

Changes that are cosmetically obvious and touch ≤ 5 lines (typo fixes,
comment wording, import ordering) may be implemented directly in this session
without spawning a worker. Document via `gg record` if non-trivial judgment
was applied.
