# gg Integration: Cursor

This document explains how to remind Cursor sessions to use gg as shared durable
memory. Cursor keeps its native workflow; gg stores the decisions, rejections,
bugs, evidence, and handoffs future agents need. For the compact Cursor capture
map, see [Native Workflow Capture Points](../native-workflow-capture.md#cursor).

## Rules file

Cursor reads project rules from `.cursor/rules/*.mdc` or from the **Rules for
AI** field in Cursor Settings.

**Recommended:** let gg create `.cursor/rules/gg-mandatory.mdc` in your project
root with `gg doctor --install-agent-hooks --agent cursor`.

## Inject snippet

Create `.cursor/rules/gg-mandatory.mdc` if you need to wire it manually:

```markdown
---
description: gg shared durable memory rules
globs: ["**/*"]
alwaysApply: true
---

## gg — Shared Durable Memory

This project uses gg as a durable shared memory and evidence ledger.
Use Cursor normally, and sync durable outputs into gg.

### Rules

1. **Orientation** — identify the runtime instance and role, then read the
   role-scoped inbox without consuming global read state:
   ```sh
   export GG_AGENT=cursor-1      # unique agent_id for this Cursor runtime
   export GG_ROLE=implementer    # or reviewer/planner/etc.
   gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
   gg inbox --role "$GG_ROLE" --peek
   gg context --compact
   ```

2. **Before changing important behavior** — run
   `gg search "<topic>" --compact` to check existing decisions or rejections.

3. **Decision**:
   ```sh
   gg record "decision text" --reason "why" --tags "..."
   ```

4. **Rejected approach**:
   ```sh
   gg record "approach" --decision-status=rejected --reason "why not"
   ```

5. **Durable work item**:
   ```sh
   gg task create "title" --detail "..." --priority high --requester user
   ```

6. **When using gg-managed tasks** — task `owner` is the unique `agent_id`, not
   the role. Claim runnable work with `gg task start TASK-ID --owner "$GG_AGENT" --lease 30m`.
   For review handoff, include compact evidence in `gg task ready-for-live --plan`
   or `gg tell --task`: commands run, live smoke result, impacted files, known
   gaps, and artifact paths.

See [AGENTS.md](../../AGENTS.md) for the full protocol.
```

## Verification

1. Open Cursor in the project directory.
2. In the chat panel, the agent should be able to run `gg session-start --agent ... --role ...`
   and `gg inbox --role ... --peek`.
3. Manually verify:
   ```sh
   GG_AGENT=cursor-1 GG_ROLE=implementer gg session-start --agent cursor-1 --role implementer
   ```
   You should see concise next steps and a link to `docs/agent-protocol-v1.md`.

## Version update

When the gg protocol changes, regenerate `.cursor/rules/gg-mandatory.mdc` to
match the new `AGENTS.md`. Pin the version in the frontmatter comment if needed:

```markdown
<!-- gg-version: 0.1.0 -->
```

Upgrade the CLI and regenerate managed files:

```sh
gg update check
gg update
```
