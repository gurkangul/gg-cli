# ADR: foreground supervisor over hidden daemon

Status: accepted for open-source alpha

## Context

gg has a hard project constraint: agents use a local CLI as a subprocess.
There is no network API, telemetry that leaves the machine, or hidden process
that owns canonical state. Recent stability work raised the question again:
should agent-to-agent triggering use a background daemon, or can foreground
supervision provide stable enough behavior?

## Options

| Option | Reliability | Crash recovery | UX | Security/local-only | Testability |
|---|---|---|---|---|---|
| Hidden daemon | Can react without a visible terminal, but failures are easy to miss and version skew is likely after upgrades. | Needs PID files, stale-lock handling, restart policy, schema migration, and log discovery. | Smooth when it works; confusing when it silently dies. | Larger trust surface: long-lived process, background permissions, possible accidental network/API creep. | Harder: tests must manage lifecycle, ports/sockets, and races. |
| Foreground `gg watch` / supervisor command | Stable while visible; agents and humans can see logs and stop it with Ctrl-C. | Process death is obvious; state remains in Qdrant/Memgraph/JSONL/outbox, not in the watcher. | Explicit terminal pane; less automatic but easier to reason about. | Preserves subprocess-only model and local-only guarantees. | Straightforward: run command under test, send messages, assert output/exit. |
| Launch-agent/login item | Starts automatically but inherits most daemon lifecycle problems and adds OS-specific setup. | OS restarts it, but gg must still solve stale version/config/log issues. | Good after setup; poor for first-run and cross-platform docs. | Local, but requires persistent background registration. | OS-specific and slow to exercise in CI. |
| Terminal pane supervisor | Visible like foreground watch and integrates with existing multi-pane agent workflows. | Pane death is visible; master can reopen or nudge. | Best for current dogfood flow where master manages worker panes. | No new service boundary. | Testable through existing terminal/pane fakes. |

## Decision

For open-source alpha, keep strict foreground-only supervision and invest in
explicit watch/pane supervisor behavior. Do not introduce a hidden daemon,
REST server, MCP server, launch-agent, or login item as part of the canonical
architecture.

## Why

Foreground supervision matches gg's current failure model: every durable fact
lives in the stores or append-only files, and every process can be killed
without losing canonical state. A watcher should only observe inbox/task
changes, route prompts, nudge panes, and emit logs. It should not own task
truth.

This gives stable trigger behavior without a daemon by making the trigger
loop explicit:

1. The master starts `gg watch` or a pane supervisor in a visible terminal.
2. The supervisor polls or follows local store changes.
3. On role-targeted work, it writes a `gg tell`/task state transition and
   routes a prompt to the chosen pane.
4. If the worker stalls or exits, the foreground log shows the failure and the
   master can restart or reroute.
5. Because tasks/messages stay in gg, a restarted supervisor can resume from
   cursors instead of reconstructing hidden process memory.

## Migration path

If foreground supervision proves insufficient after alpha, the next step is
not a hidden daemon. The migration path is:

1. Stabilize `gg watch` as a foreground command with durable cursors, visible
   logs, and deterministic resume behavior.
2. Add an optional `gg watch --supervise-pane <role>` mode for terminal pane
   routing.
3. Only after that, consider a user-started background wrapper that runs the
   same foreground command, with explicit logs and `gg doctor` visibility.

Any future background wrapper must remain optional and must not become the
canonical tracker or expose network control.

## Consequences

- Users must intentionally start the supervisor when they want automatic
  triggering.
- First-run behavior remains easy to debug because all activity is visible in
  a terminal.
- The project avoids cross-platform daemon packaging during alpha.
- Trigger reliability depends on making cursors, pane liveness checks, and
  nudges robust rather than hiding them behind a process manager.
