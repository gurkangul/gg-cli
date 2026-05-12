# gg Integration: GSD (pi / gsd-pi)

This document explains how to inject `gg` agent rules into GSD (the `pi` coding agent harness) so GSD can be used for structured execution while gg remains the shared project brain.

## Rules file

GSD reads project instructions from:
- `CLAUDE.md` — if running inside Claude Code via the GSD harness
- `.gsd/PROJECT.md` — project description used for context injection
- `AGENTS.md` — auto-detected if present at project root

`gg init` creates `AGENTS.md` automatically. GSD agents pick it up via the `prior_system_context` injection when Claude Code reads it.

## Inject snippet

If the GSD harness is not injecting `AGENTS.md` automatically, add the following to `CLAUDE.md` or a project-level CLAUDE.md:

```markdown
## gg — Shared Agent Knowledge Base

This project uses `gg` as a shared knowledge base CLI.
Follow the rules in `AGENTS.md` at the project root.

Key rules:
1. Run `gg status` at the start of every session.
2. Before proposing any approach, run `gg search "<topic>"`.
3. Record decisions: `gg record "text" --reason "why" --tags "..."`
4. Record rejections: `gg record "approach" --decision-status=rejected --reason "why not"`
5. Create gg tasks only for durable project work: `gg task create "title" --priority high --tags "gsd"`
6. Set `GG_ROLE` in the environment for message attribution.

The full protocol with all lifecycle rules is in `AGENTS.md`.
```

## GSD-specific notes

- GSD may run manually in its own terminal when useful, but gg owns durable tasks, decisions, messages, and review state.
- GSD's `.gsd/KNOWLEDGE.md` and `gg` are complementary: `KNOWLEDGE.md` can store local scratchpad guidance; `gg` stores cross-session decisions, tasks, and inter-agent messages.
- `gg gsd audit` is advisory. It can surface GSD tasks that may need durable gg records, but scratchpad-only GSD tasks are not failures.
- When using GSD party mode (multi-agent rounds), the orchestrator must extract subagent decisions and persist them via `gg record` / `gg task create` before the round closes.

## Verification

1. Start a GSD session in the project directory.
2. The agent should call `gg status` as its first action (visible in tool call log).
3. Confirm from the shell:
   ```sh
   gg status
   ```

## Version update

Upgrade the CLI and regenerate `AGENTS.md`:
```sh
gg update check
gg update
```

GSD context injection automatically picks up the updated `AGENTS.md` on the next session start.

## See Also

- [`docs/integrations/bmad-gsd.md`](bmad-gsd.md) — using GSD together with BMAD skills and gg-cli in the same project
