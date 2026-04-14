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
   gg search "topic"
   ```
2. The same command surfaces rejections as well.
3. If any match, inform the user.

## DECISION POINT

When the user reaches a decision (explicit or implicit):

- "Let's use JWT" → decision
- "Yes, do that" → approval of prior suggestion = decision
- "Sounds good" → decision

Detect and record:
```
gg decide "short decision" --reason "why" --tags "tag1,tag2"
```
Tell the user: "Recorded that decision."

## TASK CREATION

When a unit of work becomes clear:
```
gg task create "title" --detail "description" --priority high --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

## WORKING TASKS

When the user says "work on the tasks" or "do TASK-XXX":

1. `gg task list --status pending`
2. For each task:
   1. `gg task get TASK-XXX`
   2. Write code, test, commit.
   3. `gg task done TASK-XXX "summary"`

## MESSAGING ANOTHER AGENT

When work should transfer to a different role:
```
gg tell "target-role" "message" --from your-role
```

## BLOCKERS

When a task cannot proceed:
```
gg task block TASK-XXX "reason"
```

## REJECTED APPROACHES

When an approach is considered but not chosen:
```
gg reject "approach" --reason "why not"
```

## NEVER

- Make decisions without `gg`
- Re-propose a rejected approach (search first)
- Say "we'll do that later" without opening a task
- Ask the user to run `gg` commands
