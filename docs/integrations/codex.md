# gg Integration: OpenAI Codex / Codex CLI

This document explains how to inject `gg` agent rules into the Codex CLI so that the agent automatically calls `gg` during sessions.

## Rules file

Codex CLI reads agent instructions from:
- `~/.codex/instructions.md` — global instructions (all projects)
- `AGENTS.md` in the project root — project-level instructions (auto-detected)

`gg init` already creates `AGENTS.md` at your project root. No additional file is required.

## Inject snippet

If you need a global snippet (applies to all Codex projects), add the following to `~/.codex/instructions.md`:

```markdown
## gg — Shared Agent Knowledge Base

When a project has a `gg.yaml` or `AGENTS.md` that references `gg`, treat it
as a shared knowledge base session and follow these rules:

1. **Session start** — run `gg status` and summarize open tasks and decisions.
2. **Before proposing an approach** — run `gg search "<topic>"`.
3. **On decision** — run `gg record "text" --reason "..." --tags "..."`.
4. **On rejected approach** — run `gg record --stance=reject "approach" --reason "..."`.
5. **On new work** — run `gg task create "title" --priority high`.
6. Set `GG_ROLE=codex` so messages are attributed correctly.

The full protocol is in `AGENTS.md` at the project root.
```

For project-level use, `gg init` generates `AGENTS.md` with the full protocol already included.

## Verification

1. Start a Codex CLI session in your project:
   ```sh
   codex
   ```
2. The agent should call `gg status` as its first tool invocation.
3. Confirm from the shell:
   ```sh
   gg status
   # Should show the session's activity
   ```

## Version update

The canonical source of truth is `AGENTS.md` in the project root. To update after a `gg` upgrade:

```sh
go install github.com/gurkangul/gg@latest
gg init  # regenerates AGENTS.md with current rules
```

If you maintain a global `~/.codex/instructions.md` snippet, update it to match the new `AGENTS.md` protocol.
