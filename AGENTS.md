---
agents_schema: "2.1"
---

# Agent Guidance

## Project Context — gg-cli

**What this project does:** A CLI (`gg`) that gives multiple AI agents a
shared brain. Every agent (Claude Code, Codex, Cursor, Aider, …) reads
the same decisions, tasks, rejections, discussions, notes, and code graph
through gg — no agent starts from a blank slate.

> **Note on GSD:** GSD is a workflow/skill (`mcp__gsd-workflow__*` MCP tools,
> the `bmad-quick-dev` style, `.gsd/` state), NOT a separate agent. When this
> repo's user says "route it to GSD" they mean "do it yourself, you are the
> GSD implementer in this session." Never spawn a nested GSD session against
> your own project dir — execute directly via Edit/Write/Bash.

**Who it's for:** Developers running 2+ AI agents in parallel terminals who
keep hitting the same three pains:
1. Each agent re-derives context from scratch
2. Impact-blind fixes create fix loops
3. Rejected approaches keep getting re-proposed

**Key constraints (non-negotiable):**
- **No daemon, no network.** gg is a CLI + local Docker (Qdrant + Ollama +
  Memgraph). No background process, no telemetry that leaves the machine.
- **Agent-native.** Agents call gg as a subprocess, not via REST/MCP/RPC.
- **Multi-project isolation.** Single shared infra at `~/.gg/`, but every
  project's data is namespaced by a UUID `project_id`.
- **Forward-only memory.** Append-only Qdrant; soft-filter resolved/done
  items from default retrieval, never delete.

**Architecture in one paragraph:** The CLI binary lives at
`cmd/gg/main.go` and dispatches Cobra subcommands in `cmd/`. State lives
in two stores: Qdrant (decisions, tasks, messages, rejections, discussions,
notes, bugs — vector + payload) and Memgraph (Symbol/File/Package code
graph for `gg impact`). Local infra is provisioned by `gg init` via
docker-compose at `~/.gg/`. Each project's `.gg/` holds only committed
metadata (config.yaml, sequence files, RULES.md, .gitignore). Runtime state
(telemetry, cache) lives at `~/.gg/projects/<project_id>/` and is never
committed. Outbox pattern in
`internal/outbox/` guards dual-store consistency for index operations.

**Look these up before suggesting changes:**
- `README.md` — public-facing overview + install
- `docs/architecture.md` — detailed package layout, isolation, crash safety
- `gg search "<topic>" --compact` — past decisions and rejections on the subject
- `gg context "<topic>" --compact` — unified bundle (decisions + tasks + code impact)

---

## How agents work in this project

This project uses a shared knowledge base CLI: **gg** (the project itself).
All decisions, tasks, inter-agent messages, and rejected approaches are
recorded via the `gg` command.

Every agent runs independently in its own terminal but they all write to the
same Qdrant + Ollama backend. A decision made by one agent is immediately
visible to the others through shared memory.

> The user will NEVER ask you to run `gg`. You detect the intent and invoke it
> automatically.

---

# GG RULES

## PROCESS RULES — non-negotiable

These rules exist because every single brain-sprint bug (BUG-005 through
BUG-014) traces back to one pattern: **"seen correct" was assumed equal to
"actually correct"**. Test output looked green but tests were skipping. A
task was marked done but the code wasn't committed. Spec promised X but
code delivered Y. Isolation was trusted but paths were absolute.

**Meta-rule: "Sanma, doğrula." (Don't assume — verify.)**
Every claim — "done", "CI passing", "spec compliant", "works across
machines", "data is isolated" — is invalid until proven by automation
output or a human-readable transcript. The only question is: *how was
this verified?* No answer = claim is treated as false.

The seven enforceable rules derived from that meta-rule:

1. **Done = reviewer-verified.** The agent that writes the code cannot
   also mark its task `done`. A separate reviewer measures acceptance
   criteria against the commit diff and closes the task. Unilateral
   `gg task done` / `gg bug fix` from the implementer is forbidden.

2. **Integration tests mandatory for I/O.** Any code path that reads or
   writes Qdrant, Memgraph, HTTP, or the filesystem needs at least one
   integration test against real services (not mocks). Gate it behind
   `GG_INTEGRATION_TEST=1`, but the test must actually run somewhere
   before the task can close.

3. **Live smoke test at every Wave/sprint end.** Unit + integration
   green ≠ ship-ready. Run the real CLI commands against the real
   project, read the output, query the stores directly, verify the
   claims byte-for-byte. BUG-010/011/012/013/014 all escaped unit
   coverage; only the live run exposed them.

4. **Error swallow forbidden.** `if err != nil { return nil, nil }`
   is banned. Every error either (a) is propagated with a wrapped
   message, or (b) is discriminated by an explicit predicate and
   handled with a short stderr warning. No silent empty results.

5. **Interactive-only prompts forbidden.** Every destructive or
   state-changing command accepts `--yes` or `GG_YES=1` so it can
   run from CI or a script. Interactive confirmation is the opt-in
   path, not the default.

6. **Spec ↔ code traceability enforced at review.** Every claim in a
   spec doc must be localisable to a file:line in code. If the spec
   says "byte-deterministic output" but the code uses Memgraph
   internal element IDs, the review fails. Update the spec OR fix
   the code — silent drift is banned.

7. **Verify the CI/test pipeline actually runs.** A `PASS` line isn't
   proof. Confirm for each gate: (a) is it enabled, (b) is it
   triggered, (c) did the expected assertions execute, (d) does the
   exit code reflect real failures. TestBrainRoundTrip sat as SKIP
   for weeks while people assumed it was green.

These rules are individually recorded via `gg record` (tags:
`process`, `discipline`) so they appear in `gg search --compact
"process discipline"` and cannot be quietly ignored.

## SESSION START

The very first thing to do at the start of any conversation:
```
export GG_AGENT="claude-code"   # or "codex", "cursor", "gsd", etc.
gg status
```

The `GG_AGENT` export tags every subsequent gg call as agent-initiated in
telemetry — without it the dogfood metric undercounts and gives false signals
about adoption. Set it once per shell.

**Do not set `GG_AGENT=gsd`:** GSD is a workflow layered on top of Claude Code
in this repo, not a peer agent. Use `claude-code` as your agent tag even when
following a GSD milestone/slice — telemetry should reflect the runtime, not
the workflow.

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
gg task create "title" --detail "description" --priority high --tags "tag1,tag2"
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
5. Claim it with a broadcast so other agents don't collide:
   `gg tell "all" "TASK-XXX picked up" --from <your-role>`
6. `gg task get TASK-XXX` — read the detail.
7. Write code, test, commit.
8. `gg task done TASK-XXX "summary"` — and broadcast completion:
   `gg tell "all" "TASK-XXX done: key outcome" --from <your-role>`

When the user says "do TASK-XXX" specifically, skip selection and go to step 6.

## PRE-DONE VERIFY GATE

`gg task done` runs `.gg/hooks/pre-task-done.d/*.sh` **before** writing the new
state to the store. If any script exits non-zero the command aborts with exit
code `7` (`ExitVerifyFailed`) and the task stays in its current state — agents
should treat this as "fix the failure and retry", not as a normal error.

- Pre-hooks are **always strict**. A gate that passes on failure is not a gate.
  The `hooks.strict` config in `.gg/config.yaml` only governs the post-done
  `task-done.d` hooks (quality telemetry, advisory by default).
- Pre-hook contract: executable `*.sh` in `.gg/hooks/pre-task-done.d/`, run in
  lexicographic order. Env vars available: `GG_TASK_ID`, `GG_TASK_SUMMARY`,
  `GG_PROJECT_ID`, `GG_ACTOR` (your role or agent tag).
- Runtime is the hook directory; any future `verify.yml` schema is a
  *compile-to-hooks* generator — it writes scripts into the same `.d/` dir and
  the executor stays unchanged. Same env contract is used for future task
  gates (`pre-review-approve.d`, etc.) so scripts stay portable across stages.
- Install the ready-made verify gate with `gg doctor --install-task-hooks` —
  it auto-detects Go (`go.mod`) and/or Node/Bun (`package.json`) and drops the
  matching starter script into `.gg/hooks/pre-task-done.d/` (and the Go lint
  post-hook where applicable). Existing files are preserved; re-running the
  installer never overwrites your edits. Commit the generated scripts so
  every agent inherits the gate. Walk behaviour: monorepo subdirectories are
  scanned up to `doctor.hook_install.max_depth` (default 3); `node_modules`,
  `.git`, `.gg`, `.gsd`, `vendor`, `dist`, `build`, `_bmad*` are pruned by
  default. Symbolic links are not followed — write a manual hook if your
  layout depends on symlinked subprojects.
- Manual override — any executable `*.sh` in the same directory works. The
  simplest example that blocks `done` when the build is broken:
  `.gg/hooks/pre-task-done.d/10-build.sh` containing `#!/bin/sh` + your build
  command.

**Rejection contract (for every agent, not just Claude):**

On rejection the CLI emits three signals in parallel, so naive callers and
structured parsers can both react:

1. **Exit code `7`** and a human line on stderr — the fallback for any agent
   that only knows how to read text.
2. **NDJSON event line on stderr**, shape:
   ```
   {"event":"verify_failed","gate":"pre-task-done","task":"TASK-042","hook":"10-build.sh","exit":1,"ts":"2026-04-18T09:12:33Z","detail":"..."}
   ```
   Keys are stable — program against them. The `gate` field identifies which
   gate fired (today `pre-task-done`; future gates like `pre-review-approve`
   share the same envelope). `detail` is the trimmed tail of the hook's
   combined stdout+stderr; use it to show the agent *why* the gate blocked so
   the next attempt can be targeted.
3. **Auto-broadcast**: the CLI sends an internal `gg tell` from `verify-gate`
   to `all`, carrying the same summary and linked to the task. Parallel
   sessions see it in `gg status` immediately — no per-agent wiring needed.
   Best-effort: a store-down failure is silently swallowed so the notification
   never masks the verify failure. Set `GG_NO_AUTO_NOTIFY=1` to suppress the
   broadcast (CI, reentrant hook scripts, tests).

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
Until Bug→File graph edges ship (TASK-200), include each affected file
path inside `--root-cause` so semantic triage can recover them later.

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

**Use `--audience agents` for all agent-status broadcasts.** These are
routed away from the human inbox by default (`gg inbox` hides them unless
the human passes `--include-agents`). Only omit `--audience` (defaults to
`all`) when the human explicitly needs to see the message.

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
gg record "approach" --stance=reject --reason "why not"
```
This prevents other agents from re-proposing the same rejected path.

## TRACING

When debugging or profiling is needed — GG_TRACE=1 enables span recording:
```
GG_TRACE=1 gg <command>        # records spans to .gg/traces/YYYY-MM-DD.jsonl
gg trace show                   # display recent spans
gg trace summary                # summary by operation
gg trace clear --older-than 7d  # clean up old trace files
```

## SUBAGENTS AND MULTI-AGENT ROUNDS

When you spawn subagents (BMAD party mode, Task-type subagents, role simulations
like Winston/Amelia/John, etc.), those subagents usually cannot invoke `gg`
themselves — they run in isolated prompts that don't read AGENTS.md.

You, as the orchestrator, are responsible for **extracting gg-relevant actions
from their output and executing the `gg` calls yourself** as soon as the round
completes. Concretely:

- A subagent says "we should reject X because Y" → you run `gg record "X" --stance=reject --reason "Y"`
- A subagent proposes action items / a punch list → you run `gg task create` for each
- A subagent reaches a conclusion the user accepts → you run `gg record "conclusion" --reason "why"`

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

<!-- gg-managed:start -->
## Mandatory gg-cli Protocol (managed by gg — do not edit this block)

Before acting in this project:

1. Read this entire AGENTS.md file.
2. Run `gg search --compact <topic>` before proposing anything new.
3. Record every decision/task/rejection with gg — no exceptions.
4. Broadcast substantive work via `gg tell all --from <role>`.

### GSD ↔ gg mirror (if this project uses GSD)

GSD is a planning workflow with its own SQLite state in `.gsd/gsd.db`.
Other agents (Claude Code, Cursor, Aider) **cannot read GSD state** — they
only see what's in gg. Without a gg mirror, GSD work is invisible to the
rest of the team.

Rules when GSD is in use:

- **Every GSD task (T-level, not milestone or slice) MUST have a gg task
  mirror.** One GSD task = one `gg task create`. Slice/milestone summaries
  are not substitutes — per-task mirroring is the contract.
- Mirror at pickup (`gsd_execute` → `gg task create` first), not at
  completion. Invisible work in flight is the drift class this rule
  prevents.
- Reference the GSD ID in the gg title: `"[GSD:M001-S02-T05] implement foo"`
  so anyone in gg can trace back if needed.
- Close the gg mirror with `gg task done` when the GSD task completes.
  Never self-close as implementer — reviewer authority applies here too.
- If you must pick between the two stores, **gg is canonical**. GSD state
  is a planning scratchpad; gg is the shared brain every agent reads.

**Tracker rule — gg is canonical:** never call
`mcp__gsd-workflow__gsd_plan_milestone`, `gsd_plan_slice`, or `gsd_plan_task`
in a project that uses gg. Those tools write to `.gsd/gsd.db`; gg reads
none of that, so the two stores diverge silently. Use `gg task create` for
every work item and `gg record` for decisions.

### Gate workflow (check `.gg/config.yaml` for which are active)

**Task close — when `tasks.require_ready_for_live: true`:**
`gg task done` is refused until the task first transitions via
`gg task ready-for-live TASK-NNN "verify plan sentence" --from <your-role>`.
Then close with `gg task done TASK-NNN "summary" --verifier <different-role>`.
When `verifier_separation: true`, the verifier role MUST differ from the
actor that set ready_for_live — this blocks single-agent self-certification
(the TASK-200→207 class of premature-close bugs).

**Bug fix — when `bugs.require_broken_ref: true`:**
`gg bug fix BUG-NNN --repro <path> --repro-broken-ref <SHA>` is mandatory.
The CLI creates a worktree at <SHA> and asserts the repro exits non-zero
there (bug existed), then asserts it exits 0 at HEAD (fix works). A repro
that passes at the broken ref is rejected — it means the test never
exercised the failing path.

**Before editing any file (mandatory pre-flight):** run `gg impact <file>`
to see historical bugs that have touched it + 1-hop code dependents. Paste
a one-line summary per file into the commit footer.

**`gg impact` accepts three argument types:**
`<file-path>` → file blast radius; `BUG-NNN` → affected files/symbols;
`TASK-NNN` → downstream dependents via DEPENDS_ON/BLOCKS edges.

**When reporting a bug, always pass `--affected-files` + `--affected-symbols`**
so the Bug→File AFFECTS edges land in Memgraph. Without them the bug node
exists but is invisible to `gg impact <file>` queries.

<!-- gg-managed:end -->

<!-- gg-bmad:start -->
## BMAD Skill Agents — gg Protocol Relay

BMAD agents (Mary, John, Winston, Amelia, Paige, Sally, and others) run
inside Claude Code sessions. They cannot call gg directly. As the
orchestrating agent you MUST:

- After each BMAD round: extract any decisions, task proposals, or
  rejected approaches and persist them with gg immediately.
- Do NOT wait for the user to ask — capture before moving on.
- If a BMAD agent says 'reject X' → `gg record "X" --stance=reject --reason "why"`
- If a BMAD agent proposes a task → `gg task create "title" ...`
- If a BMAD agent reaches a conclusion the user accepts → `gg record "conclusion" --reason "..."``

<!-- gg-bmad:end -->
