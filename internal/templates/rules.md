# GG RULES

This project uses a shared knowledge base. All decisions, tasks, and
inter-agent information exchange flow through the `gg` CLI.
The user will NEVER ask you to run `gg` — you detect the intent and run it.

## SESSION START

First thing in every conversation:
```
export GG_AGENT=omo-slim     # unique agent_id (omo-slim, codex-1, claude-planner, ...)
export GG_ROLE=implementer   # role (implementer, reviewer, planner, ...)
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
```
Summarize open tasks, unread messages, and recent decisions for the user.
Use unique `GG_AGENT` values when two agents share the same role. Do not run
role-less `gg inbox --advance-cursor`; role-scoped `--peek` is the safe default.

## DURING DISCUSSION

While discussing a topic with the user:

1. Check for prior decisions on the topic:
   ```
   gg search "topic" --compact
   ```
2. The same command surfaces rejections as well.
3. If any match, inform the user. Drop `--compact` (or run `gg task get`)
   when you need the full reason/detail body.

## CONTEXT HYGIENE

Use `--compact` by default on survey-style calls: `gg context`,
`gg search`, `gg task get`, `gg impact`. Drops Reason/Detail/Tags
bodies to preserve the context window (60-85% reduction).

Drop the flag only when you need the full body — e.g. reading a
decision's reason, or working on a task and needing its detail.

`gg status` shows `Compact  N calls, X KB saved`.

## OPEN QUESTIONS

When a topic is raised but no conclusion emerges, preserve the context without
inventing a decision:
```
gg record "Open question: <question>" --reason "context and what is unknown" --tags "question"
```

If the open question creates follow-up work, create a task instead of leaving it
implicit:
```
gg task create "Resolve <question>" --detail "context" --priority medium --requester user --tags "question"
```

## DECISION POINT

When the user reaches a decision (explicit or implicit):

- "Let's use JWT" → decision
- "Yes, do that" → approval of prior suggestion = decision
- "Sounds good" → decision

Detect and record:
```
gg record "short decision" --reason "why" --tags "tag1,tag2"
```
Tell the user: "Recorded that decision."

## TASK CREATION

When a unit of work becomes clear:
```
gg task create "title" --detail "description" --priority high --requester user --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

## WORKING TASKS

User says "continue"/"devam et"/"keep going" → pick next work autonomously:

1. `gg inbox --role "$GG_ROLE" --peek`
2. `gg status` — see open tasks/inbox/recent decisions
3. List runnable tasks: `gg task list --ready --compact` (`gg task ready` is not a current subcommand)
4. Skip tasks already claimed in recent agent broadcasts: `gg inbox --include-agents --since 2h --peek`
5. Pick highest-priority unclaimed pending task
6. Claim: `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m`
7. Broadcast: `gg tell "all" "TASK-XXX started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-XXX`
8. Hydrate: `gg task get TASK-XXX` and `gg context --for-task TASK-XXX`
9. Before editing files: `gg impact <file> --compact`
10. Write code and test; renew long leases with `gg task renew TASK-XXX --owner "$GG_AGENT" --lease 30m`
11. Implementers mark ready, not done: run `gg task get TASK-XXX` first (required hydration), then `gg task ready-for-live TASK-XXX "reviewer verify plan" --from "$GG_ROLE"`
12. Notify reviewer: `gg tell reviewer "TASK-XXX ready for review" --from "$GG_ROLE" --task TASK-XXX`

Release only when abandoning/handoff unfinished `in_progress` work:
`gg task release TASK-XXX --owner "$GG_AGENT"`. Do not release after
`ready-for-live`; the current CLI only releases `in_progress` tasks.

User says "do TASK-XXX" specifically → skip selection and claim that task
with `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m` before work.

## MESSAGING ANOTHER AGENT

When work should transfer to a different role:
```
gg tell "target-role" "message" --from your-role
```

## BROADCAST (selective)

For cross-agent visibility during parallel work, use:
```
gg tell "all" "short status" --from your-role --audience agents
```

Broadcast only on: task pick-up, approach-selection moments, blockers,
task completion. Do not broadcast routine progress or compile errors.
Rule: if another agent doesn't need it to avoid collision or duplicate
work, skip.

## BLOCKERS

When a task cannot proceed:
```
gg task block TASK-XXX "reason"
```

## REJECTED APPROACHES

When an approach is considered but not chosen:
```
gg record "approach" --decision-status rejected --reason "why not"
```

## NEVER

- Make decisions without `gg`
- Re-propose a rejected approach (search first)
- Say "we'll do that later" without opening a task
- Ask the user to run `gg` commands
