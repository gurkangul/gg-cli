# GG RULES

This project uses gg as durable shared memory. gg does not own the agent's
workflow; use the native workflow that fits the work. The mandatory rule is
durable memory sync: anything future agents must know goes into gg.

Record durable outputs:

- decisions and reasons
- rejected approaches and reasons
- project work items or story outputs future agents need
- bugs, root causes, repros, and fix evidence
- test/diff/evidence summaries and important artifact references
- blockers and handoffs

Minimal evidence packet for review or handoff: commands run, live smoke result,
impacted files, known gaps, and artifact paths. Keep bulky logs/screenshots/traces
in their native location and write only the compact summary/reference into gg.

The user will NEVER ask you to run `gg` — you detect the durable-memory moment
and run it.

## RECOMMENDED ORIENTATION

At session start, orient yourself when shell access is available:
Set `GG_AGENT` to the runtime actually executing gg commands. Do not copy an
example from another runtime; a real GSD shell should use a unique `gsd-*` id.

```sh
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```
Summarize open tasks, unread messages, and recent decisions for the user when
useful. Use unique `GG_AGENT` values when two agents share the same role. If a
host runtime mirrors GSD output into gg, keep the host runtime's id; if GSD
itself runs the shell, use `gsd-*`. Do not run role-less
`gg inbox --advance-cursor`; role-scoped `--peek` is the safe default.

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

If the open question creates durable follow-up work, create a task instead of
leaving it implicit:
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

## REJECTED APPROACHES

When an approach is considered but not chosen:
```
gg record "approach" --decision-status rejected --reason "why not"
```

## DURABLE WORK ITEMS

When a unit of work, story output, or follow-up must be visible outside one
agent session:
```
gg task create "title" --detail "description" --priority high --requester user --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

Do not create gg tasks for private scratchpad steps that do not matter to future
agents.

### Evidence and handoffs

When a reviewer or next agent needs proof without raw logs, include:
commands run, live smoke result, impacted files, known gaps, and artifact paths.
Put that compact packet in `gg task ready-for-live --plan` or `gg tell --task`.
Use `gg bug` for bug/root-cause/fix evidence.

## WHEN USING GG-MANAGED TASKS

User says "continue"/"devam et"/"keep going" and the project uses gg-managed
tasks:

1. `gg inbox --role "$GG_ROLE" --peek`
2. `gg status` — see open tasks/inbox/recent decisions
3. List runnable tasks: `gg task list --ready --compact` (`gg task ready` is not a current subcommand)
4. Skip tasks already claimed in recent agent broadcasts: `gg inbox --include-agents --since 2h --peek`
5. Pick highest-priority unclaimed pending task unless the user named one
6. Claim: `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m`
7. Broadcast: `gg tell "all" "TASK-XXX started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-XXX`
8. Hydrate: `gg task get TASK-XXX` and `gg context --for-task TASK-XXX`
9. Before editing files: `gg impact <file> --compact`
10. Work in the native tool of choice and test; renew long leases with `gg task renew TASK-XXX --owner "$GG_AGENT" --lease 30m`
11. If configured review gates require handoff, mark ready rather than done: `gg task ready-for-live TASK-XXX --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=<cmds run>; live=<smoke result>; impact=<files checked with gg impact>; gaps=<none|known gap>; artifacts=<paths>" --from "$GG_ROLE"`
12. Notify reviewer: `gg tell reviewer "TASK-XXX ready. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <paths>" --from "$GG_ROLE" --task TASK-XXX`

Release only when abandoning/handoff unfinished `in_progress` work:
`gg task release TASK-XXX --owner "$GG_AGENT"`. Do not release after
`ready-for-live`; the current CLI only releases `in_progress` tasks.

If another native tool owns local planning, keep using it and mirror durable
outputs into gg instead of forcing all scratchpad steps into `gg task`.

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

Broadcast only on: task pick-up, approach-selection moments, blockers, and
handoffs/completion that affect other agents. Do not broadcast routine progress
or compile errors. Rule: if another agent doesn't need it to avoid collision or
duplicate work, skip.

## BLOCKERS

When a task cannot proceed:
```
gg task block TASK-XXX "reason"
```

## NEVER

- Make durable decisions without `gg`
- Re-propose a rejected approach (search first)
- Say "we'll do that later" for durable work without opening a task
- Ask the user to run `gg` commands you can run yourself
