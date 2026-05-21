# gg Integration: Cursor

This document explains how to inject `gg` agent rules into Cursor so that the AI automatically calls `gg` during sessions.

## Rules file

Cursor reads project rules from `.cursor/rules/*.mdc` or from the **Rules for AI** field in Cursor Settings.

**Recommended:** let `gg` create `.cursor/rules/gg-mandatory.mdc` in your
project root with `gg doctor --install-agent-hooks --agent cursor`.

## Inject snippet

Create `.cursor/rules/gg-mandatory.mdc` if you need to wire it manually:

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

1. **Session start** — identify the runtime instance and role, then read the
   role-scoped inbox without consuming global read state:
   ```sh
   export GG_AGENT=cursor-1      # unique agent_id for this Cursor runtime
   export GG_ROLE=implementer    # or reviewer/planner/etc.
   gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
   gg inbox --role "$GG_ROLE" --peek
   ```

2. **During discussion** — before proposing any approach, run
   `gg search "<topic>" --compact` to check for existing decisions or
   rejections.

3. **Decision** — when the user reaches a decision:
   ```sh
   gg record "decision text" --reason "why" --tags "..."
   ```

4. **Rejected approach**:
   ```sh
   gg record "approach" --decision-status=rejected --reason "why not"
   ```

5. **Task**:
   ```sh
   gg task create "title" --detail "..." --priority high --requester user
   ```

6. **Ownership** — task `owner` is the unique `agent_id`, not the role. Claim
   runnable work with `gg task start TASK-ID --owner "$GG_AGENT" --lease 30m`.

See [AGENTS.md](../../AGENTS.md) for the full protocol.
```

## Verification

1. Open Cursor in the project directory.
2. In the chat panel, the agent should begin with `gg session-start --agent ... --role ...`
   and `gg inbox --role ... --peek`.
3. Manually verify:
   ```sh
   GG_AGENT=cursor-1 GG_ROLE=implementer gg session-start --agent cursor-1 --role implementer
   ```
   You should see the concise Next steps and a link to `docs/agent-protocol-v1.md`.

## Version update

When the `gg` protocol changes, regenerate `.cursor/rules/gg-mandatory.mdc` to match the new `AGENTS.md`. Pin the version in the frontmatter comment if needed:

```markdown
<!-- gg-version: 0.1.0 -->
```

Upgrade the CLI and regenerate managed files:
```sh
gg update check
gg update
```
