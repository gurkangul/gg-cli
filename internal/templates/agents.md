---
agents_schema: "2.1"
---

# Agent Guidance

## Project Context

> **Replace this section with your project's elevator pitch.** Agents read
> this at session start to ground every decision they make. Without it,
> agents have no project context and can suggest things misaligned with
> your goals.

**What this project does:** [one sentence — what users get from it]

**Who it's for:** [primary user / use case]

**Key constraints:** [non-negotiables — e.g. "no network calls", "stay
under 1MB binary", "must work offline"]

**Architecture in one paragraph:** [main components, how they fit]

**Look these up before suggesting changes:**
- `README.md` — public-facing overview + install
- `gg search "<topic>" --compact` — past decisions and rejections on a subject
- `gg context "<topic>" --compact` — unified bundle (decisions + tasks + code impact)

---

## Durable Memory Rules

**gg is the canonical shared memory for this project.** It does not own the
agent's workflow. Use the native workflow that fits the work: BMAD, GSD, OMO
Slim, Antigravity, Codex, Claude Code, Cursor, Aider, a manual shell, or
another local process.

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

Minimal evidence packet for review or handoff: commands run, live smoke result,
impacted files, known gaps, and artifact paths. Keep bulky logs/screenshots/traces
in their native location and write the compact summary/reference into gg.

GSD may run manually in its own terminal when useful, but `.gsd/gsd.db` is not
canonical shared memory for other agents. Mirror durable GSD outcomes back into
gg. Do not use `gsd_plan_*` state as the durable memory source in a gg project.

Use these gg verbs for durable capture:

- `gg record` for decisions and rejected approaches
- `gg task` for durable shared work items or gg-managed tasks
- `gg bug` for bugs, root causes, repros, and fix evidence
- `gg tell` for cross-agent handoffs, blockers, compact evidence summaries, and artifact references
- `gg search`, `gg context`, and `gg impact` before changing important behavior

> Run `gg doctor --install-agents-md` to inject these rules into an existing
> project's AGENTS.md if they are missing.

---

## How agents work in this project

This project uses a shared knowledge base CLI: **gg**. Every agent runs in its
own native environment, but durable decisions, tasks, inter-agent messages,
rejections, bugs, evidence summaries, and handoffs are recorded via `gg`.

A decision made by one agent is immediately visible to the others through shared
memory. The workflow that produced the decision can be BMAD, GSD, OMO Slim,
Antigravity, Codex, Claude Code, Cursor, Aider, a manual shell, or anything else
that can run a command.

> The user will NEVER ask you to run `gg`. You detect the durable-memory moment
> and invoke it automatically.

---

# GG RULES

## RECOMMENDED ORIENTATION

At the start of a session, orient yourself with gg when shell access is
available:

Set `GG_AGENT` to the runtime that is actually executing gg commands. Do not
copy an example from another runtime. Examples: `gsd-myproject-1` for a real
GSD shell, `codex-1`, `claude-planner`, `cursor-1`, `omo-slim-1`.

```sh
export GG_AGENT="${GG_AGENT:?set GG_AGENT to this runtime, e.g. gsd-myproject-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```

The `GG_AGENT` export tags subsequent gg calls as agent-initiated in telemetry.
Set it once per shell and do not leave a stale value from a different runtime.
If a host agent is relaying GSD output into gg, use the host runtime's
`GG_AGENT`; if GSD itself is running the shell, use a `gsd-*` agent id.

Terms: `agent_id` is the unique runtime instance name; `role` is the authority
for the current work; task `owner` is the `agent_id` holding a gg-managed task
lease; inbox `role` / `audience` route messages. Use a unique `GG_AGENT` per
runtime, even when two agents share one `GG_ROLE`.

Role inbox reads should use `--role "$GG_ROLE" --peek`. Do not run role-less
`gg inbox --advance-cursor`; the CLI rejects it because it can hide role-targeted
assignments from a future agent.

After orientation, summarize the open tasks, unread messages, and recent
decisions for the user when useful.

## DURING DISCUSSION

When discussing a topic with the user:

1. Check if the topic already has a decision:
   ```
   gg search "topic" --compact
   ```
2. The same command also returns prior rejections for that topic.
3. If any exist, surface them: "X was already decided" / "Y was previously rejected".
4. If an entry looks relevant, fetch its full body: drop `--compact`, or
   run `gg task get TASK-XX` for detail.

## CONTEXT HYGIENE (--compact by default)

`gg context`, `gg search`, `gg task get`, and `gg impact` all accept
`--compact` — one line per item (ID, date, title), no Reason/Detail/Tags
bodies. Typical 60-85% byte reduction on populated bundles.

**Default to `--compact` when scanning:**
```
gg context "auth" --compact      # what do we know about auth?
gg search "jwt" --compact        # has JWT come up before?
gg impact src/auth.go --compact  # what breaks if I change this?
```

**Drop `--compact` only when you need the body:**
- Reading a decision's *reason* before citing or contradicting it
- Fetching the *full detail* of a task you're about to work on:
  `gg task get TASK-042` (no flag → full output)

`gg status` surfaces compact adoption (`Compact  N calls, X KB saved`).
Skipping `--compact` on scans inflates the agent's context spend and shows up in
the dogfood metric — use it on every survey-style call.

## DURABLE CAPTURE POINTS

### Decisions

When the user reaches a decision (explicit or implicit):

- "Let's use JWT" → decision
- "Yes, do that" → approval of prior suggestion = decision
- "Sounds good" → decision

Detect and record:
```
gg record "short decision" --reason "why" --tags "tag1,tag2"
```
Tell the user: "Recorded that decision."

### Rejected approaches

When an approach is considered but not chosen:
```
gg record "approach" --decision-status rejected --reason "why not"
```
This prevents other agents from re-proposing the same rejected path.

### Durable work items

When a work item, story output, or follow-up must be visible outside one agent
session:
```
gg task create "title" --detail "description" --priority high --requester user --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

Do not create gg tasks for private scratchpad steps that do not matter to future
agents.

### Bugs and evidence

Use `gg bug` for bug reports, repros, root causes, affected files/symbols, and
fix evidence. Use `gg tell` for blockers and handoffs that another role must
see.

### Evidence and handoffs

Use this minimal packet when a reviewer or next agent needs proof without raw
logs:

- Commands run: `<command> → <exit/result>`
- Live smoke: `<what was exercised> → <result or not applicable>`
- Impacted files: `<files changed>; impact checked with <gg impact commands>`
- Known gaps: `<none or explicit gap>`
- Artifacts: `<paths/references only>`

Put the packet in `gg task ready-for-live --plan` or `gg tell --task`. Use
`gg bug` for bug/root-cause/fix evidence. Keep bulky artifacts outside gg and
record only the summary plus path/reference.

## WHEN USING GG-MANAGED TASKS

When the user says "work on the tasks", "continue", "keep going", "devam et",
or asks for a specific `TASK-XXX`, use the gg task lifecycle if this project is
using gg-managed tasks:

1. `gg inbox --role "$GG_ROLE" --peek` — handle role-targeted assignments.
2. `gg status` — see pending tasks + open discussions + inbox.
3. List runnable tasks with `gg task list --ready --compact` (there is no
   `gg task ready` subcommand in the current CLI).
4. Check recent agent broadcasts with `gg inbox --include-agents --since 2h --peek` —
   has another agent already claimed a task? If yes, skip those.
5. Pick the highest-priority unclaimed pending task unless the user named one.
6. Claim it with an owner lease and status broadcast:
   `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m`
   `gg tell "all" "TASK-XXX started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-XXX`
7. Hydrate before work: `gg task get TASK-XXX` and
   `gg context --for-task TASK-XXX`.
8. Before editing each source file, run `gg impact <file> --compact`.
9. Work in the native tool of choice and test. Renew long leases with
   `gg task renew TASK-XXX --owner "$GG_AGENT" --lease 30m`.
10. If configured review gates require handoff, mark ready rather than done:
    `gg task ready-for-live TASK-XXX --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=<cmds run>; live=<smoke result>; impact=<files checked with gg impact>; gaps=<none|known gap>; artifacts=<paths>" --from "$GG_ROLE"`
    and `gg tell reviewer "TASK-XXX ready. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <paths>" --from "$GG_ROLE" --task TASK-XXX`.
11. Release only when abandoning or handing off unfinished `in_progress` work:
    `gg task release TASK-XXX --owner "$GG_AGENT"`. Do not release after
    `ready-for-live`; the current CLI only releases `in_progress` tasks.

If another native tool owns local planning, keep using it and mirror durable
outputs into gg instead of forcing all scratchpad steps into `gg task`.

## BUG FIX PRE-FLIGHT

Bugs regress when agents edit code without knowing the blast radius. These
queries take under a minute and preserve durable fix context:

1. `gg bug triage BUG-NNN --compact`
   — surfaces prior decisions, rejections, and tasks semantically near this
   bug. Read the output; if a prior fix is cited, use its approach or
   record a rejection before diverging.
2. For every file you intend to edit before touching it:
   ```
   gg impact <file> --compact
   ```
   — lists 1-hop dependents, exported symbols, and related decisions.
3. `gg search --compact "<bug keywords>"`
   — final sanity check for prior fixes or rejected approaches on this exact
   symptom.

Commit messages for bug fixes should include a one-line impact summary per
edited source file so future triage recovers the blast radius you saw.

After the fix lands:
```
gg bug fix BUG-NNN --root-cause "<one line>" "<fix summary>"
```

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

Broadcast only on task pick-up, approach-selection moments, blockers, and
handoffs/completion that affect other agents. Do not broadcast routine progress
or compile errors. Rule: if another agent doesn't need it to avoid collision or
duplicate work, skip.

## BLOCKERS

When a task cannot proceed:
```
gg task block TASK-XXX "reason"
```

## SUBAGENTS AND MULTI-AGENT ROUNDS

When you spawn subagents (BMAD party mode, Task-type subagents, role simulations
like Winston/Amelia/John, etc.), those subagents usually cannot invoke `gg`
themselves — they run in isolated prompts that don't read AGENTS.md.

The host agent is responsible for extracting durable outputs from their result
and executing the `gg` calls as soon as the round completes. Concretely:

- A subagent says "we should reject X because Y" → `gg record "X" --decision-status rejected --reason "Y"`
- A subagent proposes durable project work / a punch list → `gg task create` for each item that future agents need
- A subagent reaches a conclusion the user accepts → `gg record "conclusion" --reason "why"`

Do this before moving on; otherwise the knowledge stays trapped in one prompt.

## NEVER

- Make durable decisions without `gg`
- Re-propose a rejected approach (search first)
- Say "we'll do that later" for durable work without opening a task
- Ask the user to run `gg` commands you can run yourself
- Finish a subagent round without persisting its durable decisions/tasks/rejections to `gg`
- Broadcast every step — only broadcast moments other agents genuinely need
