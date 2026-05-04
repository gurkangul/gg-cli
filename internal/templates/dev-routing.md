## Developer Routing

By default, this session reviews and coordinates; implementation is delegated
to the configured side-session developer in a separate pane.

### Default developer agent

- **Runtime:** configured developer agent selected in `.gg/config.yaml`
- **Spawn:** `gg spawn worker --task TASK-N`
- **Nudge:** `gg spawn nudge --surface <pane-id> "<prompt>"`

### Side-session spawn pattern

```
gg spawn worker --task TASK-N
```

This bootstraps the configured developer session in a new pane and sends the task
prompt automatically. The pane ID is registered in
`~/.gg/projects/<project_id>/spawn/panes.json`.

To send a follow-up prompt to an already-running pane:

```
gg spawn nudge --surface <pane-id> "<prompt text>"
```

### Worker availability and thread limits

Do not confuse an LLM subagent/thread limit with the absence of a side-session
developer. A live terminal pane registered in `gg spawn status` is the worker
surface even when in-process subagent spawning is unavailable.

When a worker pane exists, the master must continue by supervising that pane:
- inspect `gg spawn status` and the pane's task mapping,
- send concrete next instructions with `gg spawn nudge --surface <pane-id>`,
- poll/keepalive with `GG_ROLE=master gg spawn heartbeat --watch ...`,
- review the worker's commit when it reports ready.

The master does not take over non-trivial implementation just because a
parallel subagent/thread spawn failed. If no pane exists, first try
`gg spawn worker --task TASK-N`; if that fails, block or escalate the task with
the exact spawn failure instead of silently switching to local implementation.

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
GG_ROLE=master gg spawn heartbeat --watch --poll 90 --keepalive 200 &
```

This probes each worker pane via `cmux identify` (read-only, no input injected) every 200s
(below cmux's 5-min idle cutoff, above the 60s flood floor), polls advance sentinels each tick,
and auto-prunes stale pane entries only on definitive `Surface is not a terminal` probe result.

**Note on keepalive mechanism:** worker panes are agent REPLs, not bash
shells. The keepalive uses `cmux identify --surface <id> --no-caller` rather than SendKey/Send
to avoid injecting text into the agent conversation.

### Fallback — no developer configured

If no developer agent is configured and the change is non-trivial, stop and
open/block a setup task for developer routing rather than silently implementing
as master. Direct implementation in this session is allowed only for explicit
user requests or the trivial bypass rule below. When implementing directly:
- Read the task spec fully before writing any code.
- Run `go test ./... -count=1 -race` before closing the task.
- Record any spec pivots via `gg record` before deviating.

### Trivial bypass rule

Changes that are cosmetically obvious and touch ≤ 5 lines (typo fixes,
comment wording, import ordering) may be implemented directly in this session
without spawning a worker. Document via `gg record` if non-trivial judgment
was applied.
