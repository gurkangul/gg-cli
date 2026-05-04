# Multi-Agent Orchestration

gg supports running multiple AI agents in parallel, each working on a
separate task in its own terminal pane. This document covers the parallel
worker lifecycle: how panes stay alive, how the master detects worker
readiness, and how stale panes are pruned automatically.

## Parallel worker lifecycle

### Problem

Worker panes die silently from cmux idle-timeout (~5 minutes) while
awaiting master review. The master only discovers this when it tries to
nudge the pane and gets `Surface is not a terminal`.

### Three-part fix (TASK-350)

#### 1. Worker advance sentinel

After a commit lands, the worker signals the master by writing a sentinel:

```
git commit -m "feat(TASK-NNN): ..." && \
  gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)
```

This writes a JSON file to:
```
~/.gg/projects/<project_id>/spawn/advance/TASK-NNN.done
```

Fields: `{task_id, surface_id, commit_sha, written_at}`. Idempotent —
safe to call again on amend; the sentinel is overwritten with the new SHA.

If the master heartbeat recorded a terminal surface, `gg spawn advance` also
sends a best-effort wake prompt to the master pane immediately. This is only
a notification path: the sentinel remains the durable source of truth, and
the master still reviews before closing the task.

#### 2. Master sentinel consumer (heartbeat watch)

The master runs a persistent heartbeat loop:

```
GG_ROLE=master gg spawn heartbeat --watch --poll 90 --keepalive 200 &
```

Each tick the loop:
- Writes the master heartbeat (liveness signal for worker guard hooks)
- Checks registered worker panes for activity
- **Polls the `advance/` directory** as a fallback — when a sentinel is found:
  - Renames it to `.consumed` (atomic, prevents double-fire)
  - Prints `⚡ worker ready: TASK-NNN at <sha> on <surface>`
  - Updates panes.json entry to `state=ready`
  - Does **not** auto-close the pane or call `gg task done` — master reviews first
- Sends noop keepalives to all registered panes (see below)

#### 3. Pane keepalive

To prevent cmux idle-timeout from culling panes that are awaiting master
review, the heartbeat watch loop sends `# gg-keepalive` (a bash comment,
silently eaten by the worker shell) to each registered pane every keepalive
interval (default 240s, minimum 60s floor):

```
--keepalive <seconds>       # flag on gg spawn heartbeat (min 60s)
GG_PANE_KEEPALIVE_SEC=200  # env override (clamped to floor if below 60)
```

This resets cmux's idle timer without printing stray characters in the worker shell.

#### 4. Stale-pane auto-prune

When a pane probe **definitively** fails, the watch loop automatically:
1. Removes the entry from panes.json
2. Removes the pane's lock file
3. Logs: `⚠ pruned stale pane <id> for TASK-NNN — was this an unsupervised
   death? consider increasing keepalive`

"Definitive failure" means `cmux identify --surface <id> --no-caller` returns
the exact string `Surface is not a terminal` within a 5-second deadline.
Timeouts and all other errors are treated as transient — the pane is kept and
the probe retried next tick. This conservative policy ensures a slow cmux
response never causes an accidental prune.

The master can then re-spawn the worker explicitly with
`gg spawn worker --task TASK-NNN`. The manual python-edit-panes.json pattern
is no longer needed.

## panes.json state machine

| State              | Meaning                                                  |
|--------------------|----------------------------------------------------------|
| `working`          | Worker pane is active and implementing                   |
| `idle`             | Worker pane open but no recent activity detected         |
| `waiting-on-master`| Worker sent a need-input signal                          |
| `ready`            | Worker committed + wrote advance sentinel; awaiting review |

## Sentinel file layout

```
~/.gg/projects/<project_id>/spawn/
  advance/
    TASK-042.done       ← pending sentinel (not yet consumed)
    TASK-041.consumed   ← already processed by heartbeat loop
  panes.json            ← registered worker panes with state
  heartbeat.json        ← master liveness
```

## Recommended master session startup

```sh
export GG_AGENT="${GG_AGENT:-agent}"
export GG_ROLE=master

# Start persistent heartbeat watch with keepalive
gg spawn heartbeat --watch --poll 90 --keepalive 200 &

# Start queue (spawns workers automatically)
gg spawn queue start --agent gsd
```

Workers write sentinels after commits; the heartbeat loop signals readiness
within the next poll interval; the master reviews and closes the lifecycle.

## Amend-rework re-detection

When master rejects a commit and the worker amends + re-commits, the worker
runs `gg spawn advance` again:

```
git commit --amend --no-edit && \
  gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)
```

This overwrites the previous `.done` sentinel with a new one carrying the
amended SHA. The heartbeat loop **always** calls `ConsumeAdvanceSentinel`
regardless of the pane's current `state` field — even when state is already
`ready` from the prior accept.

**Why:** the `state=ready` short-circuit was removed (TASK-351). Keeping it
would silently drop the rework signal because state stays `ready` between
the first accept and the second sentinel write. The atomic sentinel rename
(`.done` → `.consumed`) is the sole double-fire guard — state is
informational only and must not gate consumption.

**Worker contract on amend:**
1. Fix the code and commit (`git commit --amend` or a new commit).
2. Call `gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)`.
3. The heartbeat loop detects the new sentinel within the next poll tick and
   prints `⚡ worker ready: TASK-NNN at <new-sha> on <surface>` again.
