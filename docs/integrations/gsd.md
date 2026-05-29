# gg Integration: GSD (pi / gsd-pi)

This document explains how GSD can be used as a native planning/execution
scratchpad while gg remains the shared durable memory for the project.

gg does not own GSD's workflow. GSD may create specs, context, plans, and local
execution notes in its own way. The sync point is durable knowledge: anything
future agents need must be copied into gg. For the compact GSD2 capture map,
see [Native Workflow Capture Points](../native-workflow-capture.md#gsd2).

## Rules file

GSD reads project instructions from:

- `CLAUDE.md` — if running inside Claude Code via the GSD harness
- `.gsd/PROJECT.md` — project description used for context injection
- `AGENTS.md` — auto-detected if present at project root

`gg init` creates `AGENTS.md` automatically. GSD agents pick it up via the
`prior_system_context` injection when Claude Code reads it.

## Inject snippet

If the GSD harness is not injecting `AGENTS.md` automatically, add the following
to `CLAUDE.md` or a project-level CLAUDE.md:

```markdown
## gg — Shared Durable Memory

This project uses `gg` as a durable shared memory and evidence ledger.
Follow the rules in `AGENTS.md` at the project root.

Key rules:
1. Use GSD normally for local planning/execution.
2. Run `gg status` or `gg context --compact` to see what other agents recorded.
3. Before changing important behavior, run `gg search "<topic>" --compact`.
4. Record decisions: `gg record "text" --reason "why" --tags "..."`.
5. Record rejections: `gg record "approach" --decision-status=rejected --reason "why not"`.
6. Create gg tasks only for durable project work future agents need: `gg task create "title" --priority high --tags "gsd"`.
7. Record blockers, handoffs, and compact evidence summaries with `gg tell --task`
   or `gg task ready-for-live --plan`; include commands run, live smoke result,
   impacted files, known gaps, and artifact paths. Use `gg bug` for bug/root-cause/fix evidence.

The full native-workflow + durable-memory protocol is in `AGENTS.md`.
```

## GSD-specific notes

- GSD may run manually in its own terminal when useful.
- `.gsd/gsd.db` is not canonical shared memory; other agents cannot read it.
- GSD's `.gsd/KNOWLEDGE.md` and gg are complementary: `KNOWLEDGE.md` can store
  local scratchpad guidance; gg stores cross-session decisions, rejections,
  bugs, tasks, handoffs, and evidence summaries.
- `gg gsd audit` is advisory. It can surface GSD tasks that may need durable gg
  records, but scratchpad-only GSD tasks are not failures.
- When using GSD party mode or subagent rounds, the host agent extracts durable
  outputs and persists them via `gg record`, `gg task`, `gg bug`, or `gg tell`
  before moving on.

## Verification

1. Start a GSD session in the project directory.
2. The agent should be able to run `gg status` or `gg context --compact` and see
   the shared project memory.
3. Confirm from the shell:
   ```sh
   gg status
   ```

## Version update

Upgrade the CLI and regenerate managed instructions:

```sh
gg update check
gg update
```

GSD context injection automatically picks up the updated `AGENTS.md` on the next
session start.

## See Also

- [`docs/integrations/bmad-gsd.md`](bmad-gsd.md) — using GSD together with BMAD skills and gg-cli in the same project
