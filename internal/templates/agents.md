# Agent Guidance

This project uses a shared knowledge base CLI: **gg**.
All decisions, tasks, inter-agent messages, and rejected approaches are
recorded via the `gg` command.

Every agent runs independently in its own terminal but they all write to the
same Qdrant + Ollama backend. A decision made by one agent is immediately
visible to the others through shared memory.

> The user will NEVER ask you to run `gg`. You detect the intent and invoke it
> automatically.

---

# GG RULES

## SESSION START

The very first thing to do at the start of any conversation:
```
gg status
```
Summarize the open tasks, unread messages, and recent decisions for the user.

## DURING DISCUSSION

When discussing a topic with the user:

1. Check if the topic already has a decision:
   ```
   gg search "topic"
   ```
2. The same command also returns prior rejections for that topic.
3. If any exist, surface them: "X was already decided" / "Y was previously rejected".

## DECISION POINT

When the user reaches a decision (explicit or implicit):

- "Let's use JWT" → decision
- "Yes, that works" / "sounds good" → approval of the prior suggestion = decision
- "Go ahead with that" → decision

As soon as you detect it:
```
gg decide "short decision text" --reason "why" --tags "tag1,tag2"
```
Tell the user: "Recorded that decision."

## TASK CREATION

When a unit of work is clearly needed:
```
gg task create "title" --detail "description" --priority high --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

## WORKING TASKS

When the user says "work on the tasks" or "do TASK-XXX":

1. `gg task list --status pending`
2. For each task:
   1. `gg task get TASK-XXX`
   2. Write the code, test it, commit it.
   3. `gg task done TASK-XXX "summary"`

## MESSAGING ANOTHER AGENT

When some work belongs to a different role (e.g. architect decided, developer
implements):
```
gg tell "developer" "message" --from architect
```

Set your role once per shell: `export GG_ROLE=architect` (or developer, qa, etc.).

## BROADCASTING STATUS (selective)

Other agents running in parallel sessions cannot read your chat. They only see
what you write to `gg`. For cross-agent visibility during substantial work,
broadcast short status updates:
```
gg tell "all" "short status" --from <your-role>
```

**Broadcast at these moments — and only these:**
- Starting a substantial task (so another agent doesn't pick up the same one):
  `gg tell "all" "TASK-016 picked up, evaluating Memgraph Go drivers" --from developer`
- Choosing an approach among alternatives other agents might care about:
  `gg tell "all" "TASK-016: picked neo4j-go-driver over mgclient-go — Bolt support, active maintenance" --from developer`
- Hitting a blocker that affects shared assumptions:
  `gg tell "all" "TASK-016 blocked: Go 1.26 incompatibility in neo4j driver, investigating workaround" --from developer`
- Finishing a multi-step task (alongside `gg task done`):
  `gg tell "all" "TASK-016 done: Memgraph Go client live, internal/graph/ ready for TASK-007" --from developer`

**Do NOT broadcast:**
- Every code change, file read, or thought
- Routine progress ("still working on it")
- Compile errors you're about to fix
- Full discussion context — `gg search` surfaces that from decisions/rejections

Rule of thumb: if another agent doesn't need to know to avoid duplicate work,
collision, or confusion — skip the broadcast. Noise defeats the purpose.

## BLOCKERS

If a task cannot be completed:
```
gg task block TASK-XXX "reason"
```

## REJECTED APPROACHES

When an approach is considered but not chosen — always record it:
```
gg reject "approach" --reason "why not"
```
This prevents other agents from re-proposing the same rejected path.

## SUBAGENTS AND MULTI-AGENT ROUNDS

When you spawn subagents (BMAD party mode, Task-type subagents, role simulations
like Winston/Amelia/John, etc.), those subagents usually cannot invoke `gg`
themselves — they run in isolated prompts that don't read AGENTS.md.

You, as the orchestrator, are responsible for **extracting gg-relevant actions
from their output and executing the `gg` calls yourself** as soon as the round
completes. Concretely:

- A subagent says "we should reject X because Y" → you run `gg reject "X" --reason "Y"`
- A subagent proposes action items / a punch list → you run `gg task create` for each
- A subagent reaches a conclusion the user accepts → you run `gg decide`

Do this BEFORE asking the user "should I save these?" — the AGENTS.md rule is
to capture decisions automatically. Asking first violates the contract.

## NEVER

- Make decisions without `gg`
- Re-propose a previously rejected approach (search first)
- Say "we'll do that later" without opening a task
- Ask the user to run `gg` commands — you run them
- Finish a subagent round without persisting its decisions/tasks/rejections to `gg`
- Broadcast every step — only broadcast moments other agents genuinely need
