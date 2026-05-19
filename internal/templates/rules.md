# GG RULES

This project uses a shared knowledge base. All decisions, tasks, and
inter-agent information exchange flow through the `gg` CLI.
The user will NEVER ask you to run `gg` — you detect the intent and run it.

## SESSION START

First thing in every conversation:
```
gg status
```
Summarize open tasks, unread messages, and recent decisions for the user.

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

1. `gg status` — see open tasks/inbox/recent decisions
2. Skip tasks already claimed in recent inbox broadcasts
3. Pick highest-priority unclaimed pending task
4. Claim: `gg tell "all" "TASK-XXX picked up" --from <role> --audience agents`
5. `gg task get TASK-XXX`
6. Write code, test, commit
7. `gg task done TASK-XXX "summary"` + broadcast: `gg tell "all" "TASK-XXX done: ..." --from <role> --audience agents`

User says "do TASK-XXX" specifically → skip selection, go to step 6.

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
