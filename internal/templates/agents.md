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

## Tracker Rules

**gg is the canonical tracker for this project.** Do not use a parallel
planning tool alongside it — two trackers create drift that no agent can
reconcile automatically.

- GSD may run manually in its own terminal when useful, but gg owns the task,
  decision, message, and review lifecycle. Mirror meaningful GSD outcomes back
  into gg.
- Use `gg task create` for every work item. **Never call**
  `mcp__gsd-workflow__gsd_plan_milestone`, `gsd_plan_slice`, or
  `gsd_plan_task` — these write to a separate `.gsd/gsd.db` that other
  agents cannot read, creating invisible state.
- Use `gg record` for decisions and `gg record --decision-status rejected` for rejected approaches.
- Use `gg tell` for cross-agent messages.
- Rationale: GSD milestone hierarchy writes state to `.gsd/gsd.db`. gg
  reads none of that. The two stores diverge silently and stay diverged.

> Run `gg doctor --install-agents-md` to inject these rules into an
> existing project's AGENTS.md if they are missing.

---

## How agents work in this project

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
export GG_AGENT="${GG_AGENT:-agent}"  # replace "agent" with this runtime's name
gg status
```

The `GG_AGENT` export tags every subsequent gg call as agent-initiated in
telemetry — without it the dogfood metric undercounts and gives false signals
about adoption. Set it once per shell.

After `gg status`, summarize the open tasks, unread messages, and recent
decisions for the user.

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
Skipping `--compact` on scans inflates the agent's context spend and
shows up in the dogfood metric — use it on every survey-style call.

## REVIEW CONVERGENCE BEFORE DONE

Before saying "done", "fixed", or "ready", run the convergence matrix yourself:

1. Behavior matrix — default, configured, invalid, and edge inputs.
2. Negative path — unconfigured state, missing env, bad args, store/tool failure.
3. Legacy compatibility — old config fields, old command names, migration path.
4. Stale-string sweep — `rg` old terms across source, tests, docs, templates, repros.
5. Docs/templates/generated artifacts — verify generated output and unrelated churn.
6. Live smoke — inspect real CLI/app output, not only unit tests.
7. Test evidence — targeted tests, full relevant suite, and diff/format checks.

Commit the evidence with:

```
Review-Convergence: behavior matrix + negative path + legacy compatibility + stale-string sweep + docs/templates + live smoke + tests verified
```

`gg task done` installs a blocking `70-review-convergence.sh` gate that refuses
task close when this trailer is missing. If the matrix is intentionally
incomplete, bypass only with `GG_ALLOW_INCOMPLETE_REVIEW="<reason>"`; the bypass
is audited via `gg record`.

## DECISION POINT

When the user reaches a decision (explicit or implicit):

- "Let's use JWT" → decision
- "Yes, that works" / "sounds good" → approval of the prior suggestion = decision
- "Go ahead with that" → decision

As soon as you detect it:
```
gg record "short decision text" --reason "why" --tags "tag1,tag2"
```
Tell the user: "Recorded that decision."

## TASK CREATION

When a unit of work is clearly needed:
```
gg task create "title" --detail "description" --priority high --requester user --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

## WORKING TASKS

When the user says "work on the tasks", "continue", "keep going", "devam et",
or gives no specific instruction but implies "do the next thing" — select
autonomously:

1. `gg status` — see pending tasks + open discussions + inbox.
2. **Open discussions first**: if any `DISC-NNN` is open, close it (resolve
   or dismiss) before picking work. Unresolved discussions block new work
   because the decision they represent may change which task matters.
3. Check inbox for recent `[... → all]` broadcasts — has another agent
   already claimed a task? If yes, skip those.
4. Pick the highest-priority unclaimed pending task (`high` before `medium`
   before `low`; among equal priority, lowest TASK-NNN wins).
5. Claim it with an agent-status broadcast so other agents don't collide:
   `gg tell "all" "TASK-XXX picked up" --from <your-role> --audience agents`
6. `gg task get TASK-XXX` — read the detail.
7. Write code, test, commit.
8. `gg task done TASK-XXX "summary"` — and broadcast completion:
   `gg tell "all" "TASK-XXX done: key outcome" --from <your-role> --audience agents`

When the user says "do TASK-XXX" specifically, skip selection and go to step 6.

## BUG FIX PRE-FLIGHT (mandatory before `gg bug fix`)

Bugs regress when agents edit code without knowing the blast radius. These
three queries take under a minute and prevent re-opening the same bug:

1. `gg bug triage BUG-NNN --compact`
   — surfaces prior decisions, rejections, and tasks semantically near this
   bug. Read the output; if a prior fix is cited, use its approach or
   record a rejection before diverging.
2. For **every file** you intend to edit, before touching it:
   ```
   gg impact <file> --compact
   ```
   — lists 1-hop dependents (who imports this), exported symbols, and
   related decisions. If a related decision constrains your approach,
   adjust the fix.
3. `gg search --compact "<bug keywords>"`
   — final sanity check for prior fixes or rejected approaches on this
   exact symptom.

**Commit message footer (required).** Paste a one-line summary of each
`gg impact` output so future triage recovers the blast radius you saw:
```
impact cmd/index.go:   4 deps, 12 symbols, 1 related decision (DEC-042)
impact internal/graph: 2 deps, 8 symbols, 0 related decisions
```
A commit that fixes a bug without this footer fails review.

**Close the loop.** After the fix lands:
```
gg bug fix BUG-NNN --root-cause "<one line>" "<fix summary>"
```
Until Bug→File graph edges ship, include each affected file path inside
`--root-cause` so semantic triage can recover them later.

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
gg tell "all" "short status" --from <your-role> --audience agents
```

**Broadcast at these moments — and only these:**
- Starting a substantial task (so another agent doesn't pick up the same one):
  `gg tell "all" "TASK-016 picked up, evaluating Memgraph Go drivers" --from developer --audience agents`
- Choosing an approach among alternatives other agents might care about:
  `gg tell "all" "TASK-016: picked neo4j-go-driver over mgclient-go — Bolt support, active maintenance" --from developer --audience agents`
- Hitting a blocker that affects shared assumptions:
  `gg tell "all" "TASK-016 blocked: Go 1.26 incompatibility in neo4j driver, investigating workaround" --from developer --audience agents`
- Finishing a multi-step task (alongside `gg task done`):
  `gg tell "all" "TASK-016 done: Memgraph Go client live, internal/graph/ ready for TASK-007" --from developer --audience agents`

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
gg record "approach" --decision-status rejected --reason "why not"
```
This prevents other agents from re-proposing the same rejected path.

## SUBAGENTS AND MULTI-AGENT ROUNDS

When you spawn subagents (BMAD party mode, Task-type subagents, role simulations
like Winston/Amelia/John, etc.), those subagents usually cannot invoke `gg`
themselves — they run in isolated prompts that don't read AGENTS.md.

You, as the orchestrator, are responsible for **extracting gg-relevant actions
from their output and executing the `gg` calls yourself** as soon as the round
completes. Concretely:

- A subagent says "we should reject X because Y" → you run `gg record "X" --decision-status rejected --reason "Y"`
- A subagent proposes action items / a punch list → you run `gg task create` for each
- A subagent reaches a conclusion the user accepts → you run `gg record`

Do this BEFORE asking the user "should I save these?" — the AGENTS.md rule is
to capture decisions automatically. Asking first violates the contract.

## NEVER

- Make decisions without `gg`
- Re-propose a previously rejected approach (search first)
- Say "we'll do that later" without opening a task
- Ask the user to run `gg` commands — you run them
- Finish a subagent round without persisting its decisions/tasks/rejections to `gg`
- Broadcast every step — only broadcast moments other agents genuinely need
- Run `gg bug fix` without the BUG FIX PRE-FLIGHT section output pasted in
  the commit footer — unobserved blast radius is how bugs keep regressing
