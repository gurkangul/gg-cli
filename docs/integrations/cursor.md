# gg Integration: Cursor

This document explains how to inject `gg` agent rules into Cursor so that the AI automatically calls `gg` during sessions.

## Rules file

Cursor reads project rules from `.cursor/rules/*.mdc` or from the **Rules for AI** field in Cursor Settings.

**Recommended:** create `.cursor/rules/gg.mdc` in your project root.

## Inject snippet

Create `.cursor/rules/gg.mdc`:

```markdown
---
description: gg shared agent knowledge base rules
globs: ["**/*"]
alwaysApply: true
---

## gg — Shared Agent Knowledge Base

This project uses `gg` as a shared knowledge base CLI.
All decisions, tasks, messages, and rejected approaches are recorded via `gg`.

### Rules

1. **Session start** — always run `gg status` first and summarize open tasks,
   unread inbox, and recent decisions for the user.

2. **During discussion** — before proposing any approach, run
   `gg search "<topic>"` to check for existing decisions or rejections.

3. **Decision** — when the user reaches a decision:
   ```sh
   gg record "decision text" --reason "why" --tags "..."
   ```

4. **Rejected approach**:
   ```sh
   gg record --stance=reject "approach" --reason "why not"
   ```

5. **Task**:
   ```sh
   gg task create "title" --detail "..." --priority high
   ```

6. **Role** — set in the terminal before starting a session:
   ```sh
   export GG_ROLE=cursor
   ```

See [AGENTS.md](../../AGENTS.md) for the full protocol.
```

## Verification

1. Open Cursor in the project directory.
2. In the chat panel, the agent should begin with a `gg status` call visible in its reasoning.
3. Manually verify:
   ```sh
   gg status
   ```
   You should see any agent-created tasks or decisions in the output.

## Version update

When the `gg` protocol changes, update `.cursor/rules/gg.mdc` to match the new `AGENTS.md`. Pin the version in the frontmatter comment if needed:

```markdown
<!-- gg-version: 0.1.0 -->
```

Upgrade the CLI and regenerate `AGENTS.md`:
```sh
go install github.com/gurkangul/gg-cli/cmd/gg@latest
gg init  # updates AGENTS.md in place
```
