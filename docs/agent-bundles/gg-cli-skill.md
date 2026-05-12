---
name: gg-cli
description: Use gg as the shared project brain for agent sessions, task workflow, review handoff, and context lookup through the CLI/subprocess interface.
---

# gg-cli shared brain protocol

Use this skill when working in a repository that has `AGENTS.md` or `.gg/`.
The project-local instructions are authoritative.

## Session start

```sh
export GG_AGENT="${GG_AGENT:-codex}"
gg status
gg inbox --role "${GG_ROLE:-developer}" --since-cursor
```

Summarize open tasks, unread messages, and recent decisions before starting
new work.

## Before proposing or coding

```sh
gg search "topic" --compact
gg context "topic" --compact
```

If editing source files, check blast radius first:

```sh
gg impact path/to/file --compact
```

Do not re-propose rejected approaches returned by `gg search`.

## Task workflow

When work is needed:

```sh
gg task create "short title" --detail "acceptance criteria" --priority medium
```

When picking up work:

```sh
gg tell all "TASK-123 picked up" --from "$GG_ROLE" --audience agents
gg task get TASK-123
```

Broadcast only substantive coordination events: pickup, chosen approach,
shared blocker, and completion.

## Decisions and rejections

Record accepted decisions immediately:

```sh
gg record "decision text" --reason "why" --tags "topic,TASK-123"
```

Record rejected approaches:

```sh
gg record "approach text" --decision-status=rejected --reason "why not" --tags "topic,TASK-123"
```

## Review handoff

The implementer should not self-certify. When ready-for-live is enabled:

```sh
gg task ready-for-live TASK-123 "verification plan" --from developer
```

A separate verifier closes:

```sh
gg task done TASK-123 "verified summary" --verifier reviewer
```

## Constraints

- gg remains the canonical tracker.
- Call gg as a subprocess; do not introduce REST, RPC, MCP, or daemon state.
- Keep project data local and project-scoped.
- Prefer `--compact` when scanning and fetch full records only when needed.
