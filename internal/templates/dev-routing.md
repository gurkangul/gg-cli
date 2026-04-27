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

### Worker commit protocol (advance sentinel)

After committing, the worker **must** write an advance sentinel so the master heartbeat loop
detects readiness without polling screen content:

```
git commit -m "..." && gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)
```

The sentinel is written to `~/.gg/projects/<project_id>/spawn/advance/TASK-NNN.done`. The
master's `gg spawn heartbeat --watch` loop polls this directory and prints `⚡ worker ready`
within the next poll interval. The pane transitions to `state=ready` in panes.json. The master
still must review the commit before calling `gg task done`.

### Master keepalive loop

Start the keepalive + sentinel watch from the master session before spawning workers:

```
GG_AGENT=claude-code gg spawn heartbeat --watch --poll 90 --keepalive 200 &
```

This probes each worker pane via `cmux identify` (read-only, no input injected) every 200s
(below cmux's 5-min idle cutoff, above the 60s flood floor), polls advance sentinels each tick,
and auto-prunes stale pane entries only on definitive `Surface is not a terminal` probe result.

**Note on keepalive mechanism:** worker panes are agent REPLs (Claude Code, GSD), not bash
shells. The keepalive uses `cmux identify --surface <id> --no-caller` rather than SendKey/Send
to avoid injecting text into the agent conversation.

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
