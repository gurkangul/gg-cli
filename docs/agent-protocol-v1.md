# Agent Protocol v1

Status: RC operational protocol for multiple AI agents sharing one gg-cli project.

This document describes the command flow that works with the current CLI. It does not add new features. When a desired command is missing, the supported replacement is listed explicitly.

## Current CLI audit

- Task ownership: supported by `gg task start`, `gg task renew`, and `gg task release`.
  - `start` moves `pending` or expired/owned `in_progress` tasks to `in_progress`, sets `owner`, `claimed_at`, and `lease_until`, and refuses another active owner.
  - `renew` only works for the current owner.
  - `release` only works for the current owner while the task is `in_progress`; it returns the task to `pending` and clears owner/lease fields.
- Session identity: `gg session-start --agent <agent_id> --role <role>` validates and prints the agent identity and a role-scoped happy path. It cannot export environment variables for later commands, so shell sessions may still export `GG_AGENT`/`GG_ROLE`; one-off agents can pass explicit `--role`, `--owner`, `--from`, and `--verifier` flags on later commands.
- Same-role inbox receipts: current messages have one global `read` flag, not per-agent receipts. The safe default is `gg inbox --role <role> --peek`. Role-less `gg inbox --advance-cursor` is rejected because it can hide role-targeted assignments.
- Ready task discovery: there is no `gg task ready` subcommand in the current CLI. Use `gg task list --ready --compact`.
- Ready-for-live/review/release: `gg task ready-for-live`, `gg task review`, and `gg task done --verifier` exist. `release` after `ready-for-live` is not currently supported because `release` only accepts `in_progress` tasks. Treat the owner on `ready_for_live` as audit metadata until reviewer closure.
- Stale lease cleanup: `gg reconcile` is read-only by default and detects projection drift, orphaned leases, and stale `in_progress` leases. `gg reconcile --apply` writes repairs for safe cases, including releasing stale `in_progress` leases back to `pending`; it intentionally does not treat `ready_for_live` owner fields as active stale leases.
- Known RC gaps to avoid in protocol text:
  - Do not document `gg task ready`; document `gg task list --ready --compact`.
  - Do not tell agents to run non-peek role inbox reads in same-role multi-agent sessions.
  - Do not tell agents to release after `ready-for-live` until the CLI supports that transition.
  - A rejected `ready_for_live` task has review metadata but no clean CLI transition back to `in_progress`; the reviewer should reject, notify the implementer, and create/assign follow-up work or request an explicit lifecycle transition from a maintainer.

## 1. Session start

Every agent must identify both runtime instance and authority role before work.

Standard terms:

- `agent_id`: unique agent instance name, for example `omo-slim`, `codex-1`, `claude-planner`, `hermes-reviewer`.
- `role`: work authority, for example `implementer`, `reviewer`, `planner`, `researcher`, `maintainer`.
- `owner`: the task lease holder; use the `agent_id`, not the role.
- `audience` / `role`: inbox routing target.
- `reviewer`: closure authority that approves/rejects and may run `gg task done`.

```bash
export GG_AGENT=omo-slim
export GG_ROLE=implementer
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
```

Rules:

- `GG_AGENT` / `--agent` must identify the actual runtime doing the command. Do not leave a stale value from another agent.
- If two agents have the same role, keep `GG_ROLE` the same but make `GG_AGENT` unique, for example `codex-1` and `codex-2`.
- `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"` is a briefing and validation command, not a shell environment mutator. Export variables yourself for multi-command shells, or pass explicit flags on later commands.
- Minimal Omo Slim bootstrap text: "You are working in a gg-cli managed project. Use gg-cli as the shared project brain. Start with: `gg session-start --agent omo-slim --role implementer`. Then read inbox, list ready tasks, claim one task, hydrate it, load context, work, test, mark ready-for-live. Do not bypass gg-cli task ownership."
- If `gg session-start` is unavailable in an older installed binary, use the fallback:

```bash
export GG_AGENT=codex
export GG_ROLE=implementer
gg status
gg inbox --role "$GG_ROLE" --peek
```

## 2. Inbox and tell protocol

Before selecting or starting work:

```bash
gg inbox --role "$GG_ROLE" --peek
```

Preferred daily shape:

```bash
gg inbox --role "$GG_ROLE" --peek
```

Why this shape:

- `--role` scopes assignments to the role.
- `--peek` avoids flipping the global message `read` flag and stealing the message from another same-role agent.
- Role-less `--advance-cursor` is unsafe and rejected. If you intentionally want cursor mode, use it only with explicit `--role` and without `--peek`.

Use `gg tell` for cross-agent handoff:

```bash
gg tell all "TASK-123 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-123
gg tell reviewer "TASK-123 ready for review" --from "$GG_ROLE" --task TASK-123
gg tell implementer "TASK-123 rejected: missing integration smoke" --from reviewer --task TASK-123
```

Guidelines:

- Use `--audience agents` for status broadcasts that should not clutter the human inbox.
- Use a role target such as `reviewer` for action-required handoff.
- Always include `--task TASK-ID` when the message is about a task.
- If an inbox message assigns a runnable implementation task, claim it with `gg task start ...`.
- If an inbox message assigns a `ready_for_live` review task and your role is reviewer/verifier, hydrate and review it; `gg task start` is not supported from `ready_for_live`.
- If an inbox message assigns work you will not take, reply with `gg tell <sender> "deferring because ..." --task TASK-ID`; do not silently skip.

## 3. Select and claim a task

Discover runnable tasks:

```bash
gg task list --ready --compact
```

There is currently no `gg task ready` subcommand. `gg task list --ready --compact` is the Agent Protocol v1 command.

For a specific runnable implementation task:

```bash
gg task get TASK-123
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg tell all "TASK-123 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-123
```

Claim rules:

- Start only one task at a time unless the human explicitly requests parallel work.
- Use the runtime identity as owner: `--owner "$GG_AGENT"`.
- If another owner has an active lease, do not bypass it. Read the task and inbox, then either pick another task or ask/notify the owner.
- If the lease expired, `gg task start TASK-ID --owner "$GG_AGENT" --lease 30m` can take it over. Broadcast the takeover.
- For long work, renew before expiry:

```bash
gg task renew TASK-123 --owner "$GG_AGENT" --lease 30m
```

## 4. Gather context before editing

After claiming, hydrate the task and related context:

```bash
gg task get TASK-123
gg context --for-task TASK-123
```

Use compact scans first, then hydrate full bodies only when needed:

```bash
gg search "topic keywords" --compact
gg context "topic keywords" --compact
```

Before editing any file, run impact for every intended path:

```bash
gg impact path/to/file.go --compact
```

Impact output must affect the plan. If it shows a relevant decision, bug, or rejection, read the full item before contradicting it.

## 5. Work and keep the lease healthy

During implementation:

```bash
gg task renew TASK-123 --owner "$GG_AGENT" --lease 30m   # when work continues near expiry
gg tell all "TASK-123 progress: meaningful shared status" --from "$GG_ROLE" --audience agents --task TASK-123
```

Do not use `gg task release` as a progress marker. Release means the current owner is abandoning or handing off unfinished `in_progress` work.

Release only in these cases:

```bash
gg task release TASK-123 --owner "$GG_AGENT"
gg tell all "TASK-123 released: reason" --from "$GG_ROLE" --audience agents --task TASK-123
```

Use release when:

- You cannot continue.
- The human reassigns the task.
- You claimed the wrong task.
- You are intentionally handing off before `ready-for-live`.

Do not release after `gg task ready-for-live`; current CLI rejects that transition because the task is no longer `in_progress`.

## 6. Verify before ready-for-live

Before saying implementation is ready, run the project’s relevant checks. For gg-cli Go work, the canonical gate is:

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
- If local memory limits break the exact race command, retry with constrained settings and report both the exact failure and the constrained pass.
- Do not run `gg reconcile --apply` as a routine check. It writes repairs.

## 7. Mark ready-for-live

Use `ready-for-live` when local implementation and local verification are complete, but an independent reviewer/verifier still needs to inspect and/or run live-shaped checks.

```bash
gg task ready-for-live TASK-123 "Reviewer: run diff review, tests, and live smoke for <specific behavior>" --from "$GG_ROLE"
gg tell reviewer "TASK-123 ready for review: <short summary>" --from "$GG_ROLE" --task TASK-123
```

Rules:

- Implementer roles may mark ready-for-live.
- Implementer roles must not call `gg task done` in projects that enforce reviewer/verifier separation.
- Do not release after ready-for-live in the current CLI. The owner field remains useful audit metadata.
- If you discover more work after ready-for-live, tell the reviewer and record the issue. Do not self-close.

## 8. Reviewer role

Reviewer startup:

```bash
export GG_AGENT=reviewer-codex
export GG_ROLE=reviewer
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
```

Reviewer checklist:

```bash
gg task get TASK-123
gg context --for-task TASK-123
git diff --stat
git diff --check
# run task-specific tests and live smoke
```

Approve and close only after verification:

```bash
gg task review TASK-123 --approve --by "$GG_ROLE" --notes "Reviewed diff, tests, and live smoke."
gg task done TASK-123 "Verified and closed: <one sentence>" --verifier "$GG_ROLE"
gg tell all "TASK-123 done by reviewer: <key outcome>" --from "$GG_ROLE" --audience agents --task TASK-123
```

Reject without closing:

```bash
gg task review TASK-123 --reject --by "$GG_ROLE" --notes "Rejected: <specific failing criterion>"
gg tell implementer "TASK-123 rejected: <specific failing criterion>" --from "$GG_ROLE" --task TASK-123
```

Known RC limitation: a rejected `ready_for_live` task does not currently have a clean `gg task start`/`gg task reopen` transition back to `in_progress`. Until that exists, use an explicit reviewer message plus a follow-up task or maintainer-directed lifecycle repair.

## 9. Who may call `gg task done`

- Implementer/developer/worker roles: do not call `gg task done`; call `gg task ready-for-live`.
- Reviewer/verifier/maintainer roles: may call `gg task done` after full task hydration, review, tests, and live smoke.
- When `tasks.verifier_separation: true`, `--verifier` must differ from the actor that set ready-for-live.
- `gg task done` runs pre-task-done hooks first. Exit code 7 means verification blocked the close and the task state is unchanged.

## 10. Lease expiry and reconcile

If your own lease is near expiry:

```bash
gg task renew TASK-123 --owner "$GG_AGENT" --lease 30m
```

If another owner’s lease is expired:

```bash
gg task get TASK-123
gg inbox --include-agents --since 2h --peek
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg tell all "TASK-123 taken over by $GG_AGENT after expired lease" --from "$GG_ROLE" --audience agents --task TASK-123
```

Run reconcile read-only:

```bash
gg reconcile
```

Run apply only when you have read the drift report and intend to repair projection state:

```bash
gg reconcile --apply
```

There is no `--dry-run` flag because default `gg reconcile` is already read-only.

## 11. Minimal end-to-end flow

Current executable v1 flow:

```bash
export GG_AGENT=codex
export GG_ROLE=implementer

gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg task list --ready --compact
gg task start TASK-123 --owner "$GG_AGENT" --lease 30m
gg task get TASK-123
gg context --for-task TASK-123
gg impact path/to/changed-file.go --compact
# work
git diff --check
go test ./... -count=1 -race -timeout=120s
gg doctor
gg reconcile
gg task ready-for-live TASK-123 "Reviewer: run diff review, tests, and live smoke." --from "$GG_ROLE"
gg tell reviewer "TASK-123 ready for review" --from "$GG_ROLE" --task TASK-123
```

Reviewer closure:

```bash
export GG_AGENT=reviewer-codex
export GG_ROLE=reviewer

gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg task get TASK-123
gg context --for-task TASK-123
# review diff + run verification
gg task review TASK-123 --approve --by "$GG_ROLE" --notes "Verified."
gg task done TASK-123 "Verified and closed." --verifier "$GG_ROLE"
```
