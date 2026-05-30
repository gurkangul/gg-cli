# Agent Protocol v1 — Native workflow + durable memory

Status: RC protocol for multiple AI agents sharing one gg-cli project.

This document describes how agents use gg as shared project memory. It does
not replace the agent's native workflow and does not add new features. When a
command detail matters, the supported current CLI form is listed explicitly.

## Core rule

gg-cli does not own the agent's workflow. Use the native workflow that fits the
work: BMAD, GSD, OMO Slim, Antigravity, Codex, Claude Code, Cursor, Aider, a
manual shell, or another local process.

The mandatory rule is durable memory sync: anything future agents must know goes
into gg.

Record durable outputs:

- decisions and why they were made
- rejected approaches and why they were rejected
- project work items or story outputs that should be visible outside one agent
  session
- bugs, root causes, and fix evidence
- blockers and handoffs
- test, diff, live-smoke, or artifact summaries
- important artifact references

Large artifacts can stay in the repository or local filesystem. Store the
summary and path/reference in gg so another agent can find it later.

## Minimal evidence packet

When a native workflow finishes something that another agent may review or
continue, write a compact evidence packet into an existing gg verb. Use it in
`gg task ready-for-live --plan`, `gg tell --task`, `gg bug fix`, or a related
`gg record` summary as appropriate:

- Commands run: `<command> → <exit/result>`
- Live smoke: `<what was exercised> → <result or not applicable>`
- Impacted files: `<files changed>; impact checked with <gg impact commands>`
- Known gaps: `<none or explicit gap>`
- Artifacts: `<paths/references only; do not paste bulky logs>`

The packet is a summary, not a ritual. Keep raw logs, screenshots, traces, and
large diffs in their native artifact location; record only the durable result
and path/reference in gg.

## What gg is and is not

gg is a local shared ledger for decisions, rejections, tasks, bugs, evidence,
artifacts, blockers, handoffs, messages, and code impact context.

gg is not an agent runtime. It does not choose your planning method, run your
agent, or require one universal execution ritual.

The CLI gives agents common verbs:

- `gg search` / `gg context` for shared memory lookup
- `gg record` for decisions and rejected approaches
- `gg task` for durable shared work items when the project uses gg-managed tasks
- `gg bug` for bug reports, root causes, and fix evidence
- `gg tell` / `gg inbox` for handoffs and role-targeted messages
- `gg impact` for blast-radius lookup before source edits
- `gg session-start` / `gg next` for orientation without changing workflow state

## Current CLI audit

- Task ownership is available through `gg task start`, `gg task renew`, and
  `gg task release` for projects that use gg-managed tasks.
  - `start` moves `pending` or expired/owned `in_progress` tasks to
    `in_progress`, sets `owner`, `claimed_at`, and `lease_until`, and refuses
    another active owner.
  - `renew` only works for the current owner.
  - `release` only works for the current owner while the task is `in_progress`;
    it returns the task to `pending` and clears owner/lease fields.
- Session identity: `gg session-start --agent <agent_id> --role <role>`
  validates and prints identity plus a role-scoped briefing. It cannot export
  environment variables for later commands, so shell sessions may still export
  `GG_AGENT`/`GG_ROLE`; one-off agents can pass explicit `--role`, `--owner`,
  `--from`, and `--verifier` flags on later commands.
- Same-role inbox receipts: current messages have one global `read` flag, not
  per-agent receipts. The safe default is `gg inbox --role <role> --peek`.
  Role-less `gg inbox --advance-cursor` is rejected because it can hide
  role-targeted assignments.
- Ready task discovery: there is no `gg task ready` subcommand in the current
  CLI. Use `gg task list --ready --compact`.
- Ready-for-live/review/release: `gg task ready-for-live`, `gg task review`,
  and `gg task done --verifier` exist. `release` after `ready-for-live` is not
  currently supported because `release` only accepts `in_progress` tasks. Treat
  the owner on `ready_for_live` as audit metadata until reviewer closure.
- Stale lease cleanup: `gg reconcile` is read-only by default and detects
  projection drift, orphaned leases, and stale `in_progress` leases. `gg
  reconcile --apply` writes repairs for safe cases, including releasing stale
  `in_progress` leases back to `pending`; it intentionally does not treat
  `ready_for_live` owner fields as active stale leases.
- Known RC gaps to avoid in protocol text:
  - Do not document `gg task ready`; document `gg task list --ready --compact`.
  - Do not tell agents to run non-peek role inbox reads in same-role
    multi-agent sessions.
  - Do not tell agents to release after `ready-for-live` until the CLI supports
    that transition.
  - A rejected `ready_for_live` task has review metadata but no clean CLI
    transition back to `in_progress`; the reviewer should reject, notify the
    implementer, and create/assign follow-up work or request an explicit
    lifecycle transition from a maintainer.

## Terms

- `agent_id`: unique agent instance name for the runtime actually executing gg
  commands, for example `gsd-qrmenu-1`, `codex-1`, `claude-planner`,
  `omo-slim-1`.
- `role`: work authority, for example `implementer`, `reviewer`, `planner`,
  `researcher`, `maintainer`.
- `owner`: the gg-managed task lease holder; use the `agent_id`, not the role.
- `audience` / `role`: inbox routing target.
- `reviewer`: closure authority that approves/rejects and may run `gg task
  done` when configured gates require verifier separation.

## Recommended orientation

Use this at the start of an agent session when the agent can run shell commands:

Pick `GG_AGENT` for the runtime actually executing the shell. Do not copy an
example from another runtime: a real GSD shell should use a `gsd-*` id, while a
host agent relaying GSD output should keep the host agent id.

```bash
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```

These commands orient the agent and surface shared memory. They do not replace
BMAD, GSD, OMO Slim, Antigravity, Codex, Claude Code, Cursor, Aider, or a manual
shell workflow.

Guidelines:

- `GG_AGENT` / `--agent` should identify the actual runtime doing the command.
  Do not leave a stale value from another agent.
- If GSD itself runs the shell, use a unique GSD agent id such as
  `gsd-qrmenu-1`. If Claude, Codex, Cursor, OMO Slim, or another host runtime
  writes gg records based on GSD output, use the host runtime's id.
- If two agents have the same role, keep `GG_ROLE` the same but make
  `GG_AGENT` unique, for example `codex-1` and `codex-2`.
- `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"` is a briefing and
  validation command, not a shell environment mutator. Export variables yourself
  for multi-command shells, or pass explicit flags on later commands.
- If `gg session-start` is unavailable in an older installed binary, use the
  fallback:

```bash
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"
gg status
gg inbox --role "$GG_ROLE" --peek
```

## Shared memory lookup

Before proposing or changing important project behavior, check the existing
ledger:

```bash
gg search "topic keywords" --compact
gg context "topic keywords" --compact
gg context --compact                 # project-level onboarding bundle
```

Use compact scans first, then hydrate full bodies only when needed. If search or
context returns a relevant decision, rejection, bug, or task, read the full item
before contradicting it.

Before editing source files where impact matters:

```bash
gg impact path/to/file.go --compact
```

Impact output should affect the plan. If it shows a relevant decision, bug, or
rejection, read the full item before proceeding.

## Durable capture points

Native workflow can happen anywhere. The sync point is when a durable output is
created.

Use `gg record` for accepted decisions:

```bash
gg record "use JWT for auth" --reason "stateless, simple to deploy" --tags "auth,api"
```

Use `gg record --decision-status rejected` for approaches that should not be
re-proposed:

```bash
gg record "store sessions in Redis" \
  --decision-status rejected \
  --reason "adds infrastructure we do not need yet" \
  --tags "auth,api"
```

Use `gg task create` when a work item or story output must be visible outside
one agent session:

```bash
gg task create "add auth middleware" \
  --detail "protect API routes" \
  --priority high \
  --requester user \
  --tags "auth,api"
```

Use `gg bug` for bugs, root causes, repros, and fix evidence. Use `gg tell` for
handoffs and blockers that another agent role must see.

For supported-agent capture maps, see [`native-workflow-capture.md`](native-workflow-capture.md).

## Inbox and tell

Role-scoped inbox reads are useful for assignments and handoffs:

```bash
gg inbox --role "$GG_ROLE" --peek
```

Why this shape:

- `--role` scopes assignments to the role.
- `--peek` avoids flipping the global message `read` flag and stealing the
  message from another same-role agent.
- Role-less `--advance-cursor` is unsafe and rejected. If you intentionally want
  cursor mode, use it only with explicit `--role` and without `--peek`.

Use `gg tell` for cross-agent handoff:

```bash
gg tell all "TASK-123 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-123
gg tell reviewer "TASK-123 ready. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <paths>" --from "$GG_ROLE" --task TASK-123
gg tell implementer "TASK-123 rejected: missing integration smoke" --from reviewer --task TASK-123
```

Guidelines:

- Use `--audience agents` for status broadcasts that should not clutter the
  human inbox.
- Use a role target such as `reviewer` for action-required handoff.
- Include `--task TASK-ID` when the message is about a gg-managed task.
- If an inbox message assigns a runnable implementation task and you choose to
  take it, claim it with `gg task start ...`.
- If an inbox message assigns a `ready_for_live` review task and your role is
  reviewer/verifier, hydrate with `gg task get TASK-ID --review` and review it;
  `gg task start` is not supported from `ready_for_live`.
- If an inbox message assigns work you will not take, reply with `gg tell
  <sender> "deferring because ..." --task TASK-ID`; do not leave an action
  handoff silent.

## gg-managed task lifecycle

Use this section only when the project is using gg-managed tasks. If your native
workflow has its own planning surface, keep using it and mirror durable outputs
into gg.

Discover runnable gg tasks:

```bash
gg task list --ready --compact
```

There is currently no `gg task ready` subcommand. `gg task list --ready
--compact` is the current command.

For a specific runnable implementation task:

```bash
gg task get TASK-123
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg tell all "TASK-123 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-123
```

Claim guidelines:

- Start only one gg-managed task at a time unless the human explicitly requests
  parallel work.
- Use the runtime identity as owner: `--owner "$GG_AGENT"`.
- If another owner has an active lease, do not bypass it. Read the task and
  inbox, then either pick another task or ask/notify the owner.
- If the lease expired, `gg task start TASK-ID --owner "$GG_AGENT" --lease 30m`
  can take it over. Broadcast the takeover.
- For long work, renew before expiry:

```bash
gg task renew TASK-123 --owner "$GG_AGENT" --lease 30m
```

Release means the current owner is abandoning or handing off unfinished
`in_progress` work:

```bash
gg task release TASK-123 --owner "$GG_AGENT"
gg tell all "TASK-123 released: reason" --from "$GG_ROLE" --audience agents --task TASK-123
```

Do not release after `gg task ready-for-live`; current CLI rejects that
transition because the task is no longer `in_progress`.

## Evidence and review gates

Projects may configure evidence and reviewer gates around `gg task done` and
`gg bug fix`. These gates protect durable memory quality; they do not require
any specific native workflow.

For gg-cli Go work, the canonical local verification gate remains:

```bash
gofmt -w <changed-go-files>
git diff --check
go test ./... -count=1 -race -timeout=120s
go vet ./...
go build ./...
gg doctor
gg reconcile
```

Notes:

- `gg reconcile` without flags is read-only.
- If local memory limits break the exact race command, retry with constrained
  settings and report both the exact failure and the constrained pass.
- Do not run `gg reconcile --apply` as a routine check. It writes repairs.

Use `ready-for-live` when local implementation and local verification are
complete, but an independent reviewer/verifier still needs to inspect and/or run
live-shaped checks. Put the verify request and compact evidence packet in the
existing plan/message fields:

```bash
gg task ready-for-live TASK-123 \
  --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=cmd/foo.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE"

gg tell reviewer \
  "TASK-123 ready. Evidence: commands run: go test ./... -count=1; live smoke: CLI smoke passed; impacted files: cmd/foo.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE" --task TASK-123
```

Guidelines:

- Implementer roles may mark ready-for-live.
- If the verifier plan needs correction while the task is already
  `ready_for_live`, rerun `gg task ready-for-live TASK-123 --plan "<corrected
  plan>" --from "$GG_ROLE"`; the state stays `ready_for_live` and the plan is
  updated.
- Implementer roles should not call `gg task done` in projects that enforce
  reviewer/verifier separation.
- Do not release after ready-for-live in the current CLI. The owner field
  remains useful audit metadata.
- If you discover more work after ready-for-live, tell the reviewer and record
  the issue. Do not self-close.

Reviewer path when configured:

```bash
gg task get TASK-123 --review
gg context --for-task TASK-123
git diff --stat
git diff --check
# run task-specific tests and live smoke
gg task review TASK-123 --approve --by "$GG_ROLE" --notes "Reviewed diff, tests, and live smoke."
gg task done TASK-123 "Verified and closed: <one sentence>" --verifier "$GG_ROLE"
gg tell all "TASK-123 done by reviewer: <key outcome>" --from "$GG_ROLE" --audience agents --task TASK-123
```

Reject without closing:

```bash
gg task review TASK-123 --reject --by "$GG_ROLE" --notes "Rejected: <specific failing criterion>"
gg tell implementer "TASK-123 rejected: <specific failing criterion>" --from reviewer --task TASK-123
```

Known RC limitation: a rejected `ready_for_live` task does not currently have a
clean `gg task start`/`gg task reopen` transition back to `in_progress`. Until
that exists, use an explicit reviewer message plus a follow-up task or
maintainer-directed lifecycle repair.

## Native workflow capture points

GSD, BMAD, OMO Slim, Antigravity, Codex, Claude Code, Cursor, Aider, and manual
shell workflows may keep their native planning and execution style. The shared
contract is narrower: mirror durable outcomes into gg when future agents need to
retrieve them.

See [`native-workflow-capture.md`](native-workflow-capture.md) for concise
capture maps by supported agent/workflow: native strengths, native artifacts,
when to mirror into gg, which gg verbs to use, what not to force, and example
flows.

Do not introduce daemon, RPC, MCP, background sync, mode selection, or
agent-specific runtime state to make this happen. The sync surface is still the
gg CLI.

## Minimal flows

Native workflow with durable sync:

```bash
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"

gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
gg search "topic" --compact
# use the native workflow that fits the work
# when a durable output exists:
gg record "decision text" --reason "why"
gg record "rejected approach" --decision-status rejected --reason "why not"
gg tell reviewer \
  "TASK-123 handoff. Evidence: commands run: go test ./... -count=1; live smoke: not applicable; impacted files: cmd/foo.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-diff.txt" \
  --from "$GG_ROLE"
```

gg-managed task path:

```bash
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-<project>-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"

gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg task list --ready --compact
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg task get TASK-123
gg context --for-task TASK-123
gg impact path/to/changed-file.go --compact
# work in the native tool of choice
# verify and summarize evidence
# Evidence packet: commands run, live smoke result, impacted files, known gaps, artifact paths
gg task ready-for-live TASK-123 \
  --plan "Reviewer: run diff review, tests, and live smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=path/to/changed-file.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE"
gg tell reviewer \
  "TASK-123 ready. Evidence: commands run: go test ./... -count=1; live smoke: CLI smoke passed; impacted files: path/to/changed-file.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-smoke.txt" \
  --from "$GG_ROLE" --task TASK-123
```
