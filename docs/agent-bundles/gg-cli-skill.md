---
name: gg-cli
description: Use gg as the shared durable memory and evidence ledger for agent sessions through the CLI/subprocess interface.
---

# gg-cli shared memory protocol

Use this skill when working in a repository that has `AGENTS.md` or `.gg/`.
The project-local instructions are authoritative.

gg does not own the agent's workflow. Use the native workflow that fits the
work, and sync durable outputs into gg so future agents can find them.

## Recommended orientation

```sh
export GG_AGENT="${GG_AGENT:-codex}"
export GG_ROLE="${GG_ROLE:-developer}"
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```

Summarize open tasks, unread messages, and recent decisions before changing
important project behavior.

## Before proposing or coding

```sh
gg search "topic" --compact
gg context "topic" --compact
```

If editing source files where impact matters, check blast radius first:

```sh
gg impact path/to/file --compact
```

Do not re-propose rejected approaches returned by `gg search`.

## Durable capture points

Record accepted decisions immediately:

```sh
gg record "decision text" --reason "why" --tags "topic,TASK-123"
```

Record rejected approaches:

```sh
gg record "approach text" --decision-status=rejected --reason "why not" --tags "topic,TASK-123"
```

Create gg tasks only for durable shared work items or story outputs that future
agents need:

```sh
gg task create "short title" --detail "acceptance criteria" --priority medium
```

Record bugs, root causes, and fix evidence with `gg bug`. Use `gg tell` for
blockers, artifact references, and handoffs.

Minimal evidence packet for handoff/review:

- Commands run: `<command> → <exit/result>`
- Live smoke: `<what was exercised> → <result or not applicable>`
- Impacted files: `<files changed>; impact checked with <gg impact commands>`
- Known gaps: `<none or explicit gap>`
- Artifacts: `<paths/references only; keep bulky artifacts outside gg>`

Use that packet in existing verbs such as `gg tell --task`,
`gg task ready-for-live --plan`, `gg bug fix`, or `gg record`.

For agent-specific examples, see `docs/native-workflow-capture.md` in gg-cli.

## When using gg-managed tasks

```sh
gg task list --ready --compact
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg task get TASK-123
```

Broadcast only substantive coordination events: pickup, chosen approach, shared
blocker, and handoff/completion.

## Review handoff

When configured review gates require it:

```sh
gg task ready-for-live TASK-123 \
  --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=cmd/foo.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE"
gg tell reviewer \
  "TASK-123 ready. Evidence: commands run: go test ./... -count=1; live smoke: CLI smoke passed; impacted files: cmd/foo.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE" --task TASK-123
```

Reviewers close after independent verification with `gg task done ... --verifier`.

The implementer should not self-certify when verifier separation is enabled.

## Constraints

- gg remains the canonical durable memory for the project.
- Call gg as a subprocess; do not introduce REST, RPC, MCP, daemon state, or background sync.
- Keep project data local and project-scoped.
- Prefer `--compact` when scanning and fetch full records only when needed.
