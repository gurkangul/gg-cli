---
agents_schema: "2.1"
---

# Agent Guidance

> The one rule: **durable memory sync.** Anything a future agent must know goes
> into gg (`gg record` / `gg task` / `gg bug` / `gg tell`). The user will NEVER
> ask you to run `gg` — you detect the durable-memory moment and invoke it.
> Everything below is *how* to do that well. gg does not own your workflow
> (BMAD, GSD, Codex, Cursor, Aider, a plain shell are all fine).

## What gg is

A CLI (`gg`) that gives parallel AI agents a shared brain: every agent reads the
same decisions, tasks, rejections, discussions, notes, and code graph — no agent
starts blank. It solves three recurring pains: (1) each agent re-derives context,
(2) impact-blind fixes create fix loops, (3) rejected approaches get re-proposed.

**Non-negotiable constraints:**
- **No daemon, no Docker.** CLI with embedded SQLite stores (vectors + graph);
  embeddings via native Ollama or opt-in Voyage. The embedded SQLite stores are
  the only backend — the former Qdrant/Memgraph server backends were removed and
  there is no config key to point gg back at a server. No background indexer:
  `gg doctor --fix-index` is the one-shot repair; `gg index --watch` /
  `gg watch --index` are explicit foreground watchers.
- **Agent-native.** Agents call gg as a subprocess, not REST/MCP/RPC.
- **Multi-project isolation.** Per-project metadata under `.gg/`; each project
  namespaced by a UUID `project_id`.
- **Forward-only memory.** Append-only vector store; soft-filter resolved/done
  items, never delete.

**Architecture (one paragraph):** Binary at `cmd/gg/main.go` dispatches Cobra
subcommands in `cmd/`. Two stores behind pluggable interfaces: the vector store
(`internal/store`, embedded SQLite `.gg/vectorstore.db`) holds
decisions/tasks/messages/rejections/discussions/notes/bugs, and the graph store
(`internal/graph`, embedded SQLite `.gg/graph.db`) holds the Symbol/File/Package
graph for `gg impact`. Both are embedded-only — the former Qdrant/Memgraph
server backends were removed. `gg init` needs no Docker — the embedded DBs are
created on first use. Each project's `.gg/` holds
committed metadata (config.yaml, the canonical `brain/*.jsonl` ledger, RULES.md)
plus the embedded DBs; runtime state lives at `~/.gg/projects/<project_id>/` and
is never committed. The outbox pattern in `internal/outbox/` guards dual-store
consistency.

**Look these up before suggesting changes:**
- `gg search "<topic>" --compact` — past decisions + rejections on the subject
- `gg context "<topic>" --compact` — unified bundle (decisions + tasks + impact)
- `README.md` · `docs/architecture.md` · `docs/hook-env-vars.md` · `docs/agent-protocol-v1.md`

**GSD note:** GSD may run as a native scratchpad, but `.gsd/gsd.db` is NOT shared
memory — other agents can't read it. Never use `gsd_plan_*` as the durable source;
mirror durable GSD outcomes into gg.

# GG RULES

## PROCESS RULES — non-negotiable

Every brain-sprint bug (BUG-005…014) traces to one pattern: **"seen correct"
assumed equal to "actually correct."** Tests looked green but were skipping; a
task was marked done but uncommitted; spec promised X, code delivered Y.

**Meta-rule: "Sanma, doğrula." (Don't assume — verify.)** Every claim — "done",
"CI passing", "spec compliant", "works across machines" — is false until proven
by automation output or a readable transcript. The only question: *how was this
verified?* No answer = claim is false.

1. **Done = reviewer-verified.** The implementer cannot mark its own task `done`
   / `bug fix`. A separate reviewer measures ACs against the diff and closes it.
2. **Integration tests mandatory for I/O.** Any path touching Qdrant/Memgraph/
   HTTP/filesystem needs ≥1 real-service test behind `GG_INTEGRATION_TEST=1`,
   and it must actually run before close.
3. **Live smoke at every Wave/sprint end.** Unit+integration green ≠ ship-ready.
   Run the real CLI against the real project, read output, query stores directly.
   BUG-010…014 all escaped unit coverage; only the live run exposed them.
4. **Error swallow forbidden.** `if err != nil { return nil, nil }` is banned.
   Propagate wrapped, or discriminate with an explicit predicate + stderr warning.
5. **Interactive-only prompts forbidden.** Every destructive/state-changing
   command accepts `--yes` / `GG_YES=1`. Interactive confirm is opt-in, not default.
6. **Spec ↔ code traceability.** Every spec claim must localise to a file:line.
   On drift, fix the spec OR the code — silent drift is banned.
7. **Verify the pipeline actually runs.** A `PASS` line isn't proof. Confirm each
   gate is (a) enabled, (b) triggered, (c) executed its assertions, (d) exits
   non-zero on real failure. TestBrainRoundTrip sat SKIP for weeks unnoticed.
8. **Review convergence before done.** Before claiming done, run the convergence
   matrix yourself: behavior matrix, negative path, legacy compatibility,
   stale-string sweep, docs/templates/generated artifacts, live smoke, test/diff
   evidence. Put the result in the commit body as `Review-Convergence: ...`. The
   `70-review-convergence.sh` gate blocks close when missing; bypass only with
   `GG_ALLOW_INCOMPLETE_REVIEW="<reason>"` (audited via `gg record`).

Each rule is recorded via `gg record` (tags `process`, `discipline`) so it
surfaces in `gg search --compact "process discipline"`.

## ORIENTATION (start of session) — MANDATORY, do this FIRST

**Before reading or changing anything in this project, you MUST orient yourself
with gg.** Run the block below as your first action in every fresh session (and
after any `/clear`). Skipping it means working blind to prior decisions,
rejections, and open work — which this project treats as an error, not a shortcut.

Set `GG_AGENT` to the runtime *actually* executing gg (don't copy another
runtime's example): `gsd-gg-cli-1`, `codex-1`, `claude-planner`, `cursor-1`.
Use `GG_ROLE` for authority (`master`, `developer`, `reviewer`, …). One unique
`GG_AGENT` per runtime, even when two share a `GG_ROLE`.

```sh
export GG_AGENT="${GG_AGENT:?set to this runtime, e.g. gsd-gg-cli-1 or codex-1}"
export GG_ROLE="${GG_ROLE:-implementer}"
gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"
gg inbox --role "$GG_ROLE" --peek
gg context --compact
```

- Setting `GG_AGENT` tags calls as agent-initiated; a stale/missing value skews
  the dogfood adoption metric.
- Under Claude Code, an unset/generic `GG_AGENT` auto-derives a unique
  `claude-code-<session>` from `CLAUDE_SESSION_ID` — a safety net, not a substitute
  for an explicit export on non-Claude runtimes.
- Inbox read-state is per-recipient; `--peek` when inspecting an inbox you don't
  own. Never run role-less `gg inbox --advance-cursor` (the CLI rejects it).
- **Glossary:** `agent_id` = unique runtime instance; `role` = current authority;
  task `owner` = the `agent_id` holding a task lease; inbox `role`/`audience` =
  message routing.

## CORE MOVES

**During discussion** — before changing important behavior, `gg search "<topic>"
--compact` (also returns prior rejections). Surface hits: "X was already decided"
/ "Y was previously rejected." Drop `--compact` to read a body.

**Context hygiene** — `gg context`, `gg search`, `gg task get`, `gg impact`,
`gg inbox`, `gg task list` all take `--compact` (one line per item, 60–85%
smaller). Default to it on **every** survey/scan call — `inbox` and `list` are
the two most-missed verbs (telemetry); running them full wastes ~350 KB/wk with
zero added signal. Skipping `--compact` on scans inflates context spend and the
dogfood metric.

Drop `--compact` only to read a decision's *reason* before citing it. For a task
you're about to act on, go **straight to `gg task get TASK-NN --full` once** —
don't compact-preview then re-fetch full (that double-read is the bulk of the
net-negative hydration in `gg status`). `--full` is required anyway: it records
the hydration proof that `ready-for-live`/`done`/`block` need. The mandated full
hydration reads are *correct* — they are the proof gate's price, not waste to
optimize away.

**Decision point** — "Let's use JWT" / "yes, sounds good" / "go ahead" = a
decision. Record it immediately, then tell the user "Recorded that decision.":
```
gg record "short decision" --reason "why" --tags "tag1,tag2"
```
For load-bearing claims (perf, security, "X works") attach proof — empty evidence
surfaces as `[unverified]`; skip it for pure preferences:
```
gg record "use connection pooling" --reason "cuts p99" \
  --evidence "load test 320ms→90ms p99; bench cmd/foo_bench_test.go; live smoke passed"
```

**Durable work items** — when work/follow-up must outlive one session:
```
gg task create "title" --detail "desc" --priority high --requester user --tags "..."
```
Don't open tasks for private scratchpad steps.

**Rejected approaches** — always record, so others don't re-propose:
```
gg record "approach" --decision-status rejected --reason "why not"
```

**Messaging / blockers / tracing**
```
gg tell "developer" "message" --from architect        # hand work to another role
gg task block TASK-XXX "reason"                        # can't complete
GG_TRACE=1 gg <cmd>; gg trace show|summary|clear       # debug/profile spans
```

## BROADCASTING STATUS (selective)

Parallel agents can't read your chat — only what you write to gg. Use
`--audience agents` for status broadcasts (routed away from the human inbox
unless they pass `--include-agents`); omit it only when the human must see it.

**Broadcast only at these moments:**
- Starting a substantial task (avoid duplicate pickup)
- Choosing an approach others might care about
- Hitting a blocker that affects shared assumptions
- Handing off for review after `gg task ready-for-live`
```
gg tell "all" "TASK-016 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-016
```
**Never broadcast** every change/read/thought, routine progress, or compile errors
you're about to fix. If another agent doesn't need it to avoid collision — skip it.

## GG-MANAGED TASK LIFECYCLE

On "work on the tasks" / "continue" / "devam et" / a named `TASK-XXX`:

1. `gg inbox --role "$GG_ROLE" --peek` — role-targeted assignments.
2. `gg status` — pending tasks + open discussions + inbox.
3. **Close open `DISC-NNN` first** (resolve/dismiss) — they may change which task matters.
4. `gg task list --ready --compact` (no `gg task ready` subcommand exists).
5. `gg inbox --include-agents --since 2h --peek` — skip tasks another agent claimed.
6. Pick the highest-priority unclaimed pending task unless the user named one.
7. Claim + broadcast:
   `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m`
   `gg tell "all" "TASK-XXX started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-XXX`
8. Hydrate: `gg task get TASK-XXX --full` + `gg context --for-task TASK-XXX`
   (`--full` required — a plain `gg task get` auto-compacts and won't record the proof).
9. Before editing each source file: `gg impact <file> --compact`.
10. Work in your native tool; renew long leases with `gg task renew TASK-XXX --owner "$GG_AGENT" --lease 30m`.
11. **Implementers do not close tasks.** After local verification, re-hydrate with
    `gg task get TASK-XXX --full`, then hand off:
    `gg task ready-for-live TASK-XXX --plan "Reviewer: inspect diff + rerun smoke. Evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>" --from "$GG_ROLE"`
    and `gg tell reviewer "TASK-XXX ready. <same evidence>" --from "$GG_ROLE" --task TASK-XXX`.
12. `gg task release TASK-XXX --owner "$GG_AGENT"` only when abandoning unfinished
    `in_progress` work — never after `ready-for-live`.

If another tool owns local planning, keep it and mirror durable outputs into gg.

## PRE-DONE VERIFY GATE

`gg task done` runs `.gg/hooks/pre-task-done.d/*.sh` **before** writing state. Any
non-zero exit aborts with code `7` (`ExitVerifyFailed`) and leaves the task
unchanged — treat it as "fix and retry," not a normal error. Pre-hooks are
**always strict** (the `hooks.strict` config only governs advisory post-done hooks).

- Canonical test command (CI + the gate, never `-short`):
  `go test ./... -count=1 -race -timeout=120s`
- Install the ready-made gate: `gg doctor --install-task-hooks` (auto-detects Go/
  Node, preserves existing files). Commit the generated scripts so every agent
  inherits the gate.
- The **AC attestation gate** (`50-ac-attestation.sh`) blocks `done` when an AC
  anchor in the task Detail isn't referenced in the commit. Cover each AC with any
  one of: `AC-N:` line, numbered `N.`/`N)`/`N:` line, `AC N` phrase, a `TestACN_*`
  in the diff, or an `// AC-N` comment / `func acN_impl`. Bypass (audited):
  `GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done …`.

**Full reference** — hook contract, env vars, NDJSON rejection schema,
auto-broadcast, exit codes, the AC parser's 5 passes + fixture matrix,
troubleshooting, symlinks: **`docs/verify-gate.md`**.

## BUG-FIX DURABLE CONTEXT PRE-FLIGHT

Bugs regress when agents edit without knowing the blast radius. Before any fix:

1. `gg bug triage BUG-NNN --compact` — prior decisions/rejections/tasks near this
   bug. If a prior fix is cited, use it or record a rejection before diverging.
2. `gg impact <file> --compact` for **every file** you'll edit — 1-hop dependents,
   exported symbols, related decisions. A related decision may constrain the fix.
3. `gg search --compact "<bug keywords>"` — final check for prior fixes/rejections.

**Required commit footer** — one line per `gg impact`, so triage recovers the
blast radius you saw (a bug commit without this fails review):
```
impact cmd/index.go:   4 deps, 12 symbols, 1 related decision (DEC-042)
```
**Close the loop:** `gg bug fix BUG-NNN --root-cause "<one line>" "<fix summary>"`.
Until Bug→File edges ship (TASK-200), put each affected file path inside
`--root-cause` so semantic triage can recover them.

## SUBAGENTS AND MULTI-AGENT ROUNDS

Subagents (BMAD party mode, Task-type agents, role sims like Winston/Amelia/John)
usually can't call gg — they run in isolated prompts that never read this file.
The **host agent** must extract durable outputs and run the gg calls the moment a
round ends:
- "reject X because Y" → `gg record "X" --decision-status rejected --reason "Y"`
- proposed durable work → `gg task create …` per item future agents need
- a conclusion the user accepts → `gg record "conclusion" --reason "why"`

Do this before moving on, or the knowledge stays trapped in one prompt.

## NEVER

- Make durable decisions without `gg`
- Re-propose a previously rejected approach (search first)
- Say "we'll do that later" for durable work without opening a task
- Ask the user to run `gg` — you run it
- Finish a subagent round without persisting its durable outputs to `gg`
- Broadcast every step — only moments other agents genuinely need
- Run `gg bug fix` without durable impact/root-cause evidence

<!-- gg-managed:start -->
## gg Durable Memory Protocol (managed by gg — do not edit this block)

gg-cli does not own the agent's workflow. Use the native workflow that fits
the work, and sync durable outputs into gg so future agents can retrieve them.

Durable outputs include decisions, rejected approaches, shared work items,
bugs/root causes, blockers, handoffs, evidence summaries, and artifact references.
Evidence summaries should stay compact: commands run, live smoke result, impacted files, known gaps, and artifact paths.

Mandatory orientation (run before your first action):

1. Read this entire AGENTS.md file.
2. Run `gg session-start --agent <agent_id> --role <role>` as your FIRST action (mandatory when a shell is available).
3. Run `gg search --compact <topic>` before changing important behavior.
4. Record durable decisions/rejections/evidence with `gg record`, `gg task`, `gg bug`, or `gg tell`.

### GSD usage (if this project uses GSD)

GSD is a native planning or execution workflow with its own SQLite state in `.gsd/gsd.db`.
Other agents (Claude Code, Cursor, Aider) cannot read that state; they only
see what is written to gg. Treat GSD as a local scratchpad/helper, not shared memory.

GSD itself is allowed. Run it when useful, and copy durable outcomes into gg.

Rules when GSD is in use:

- Create gg tasks only for durable work items that future agents need.
- Use `gg record` for decisions and rejected approaches.
- Use `gg tell` for cross-agent handoffs and blockers.
- Summarize useful GSD output back into gg; do not rely on `.gsd/gsd.db` for shared memory.

**Shared-memory rule:** do not use `mcp__gsd-workflow__gsd_plan_milestone`,
`gsd_plan_slice`, or `gsd_plan_task` as the durable memory source in a project that
uses gg. Those tools write to `.gsd/gsd.db`; gg reads none of that, so the
state is invisible to other agents unless durable outcomes are mirrored into gg.

### Configured evidence gates

Projects may configure review/evidence gates around `gg task done` and `gg bug fix`.
These gates protect durable memory quality; they do not require a particular native workflow.

**Task close — when `tasks.require_ready_for_live: true`:**
`gg task done` is refused until the task first transitions via
`gg task ready-for-live TASK-NNN --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>" --from <your-role>`.
Then close with `gg task done TASK-NNN "summary" --verifier <different-role>`.
When `verifier_separation: true`, closure also needs durable verification
evidence from a different role than the actor that set ready_for_live.

**Bug fix — when `bugs.require_broken_ref: true`:**
`gg bug fix BUG-NNN --repro <path> --repro-broken-ref <SHA>` verifies the
repro fails at the broken ref and passes at HEAD, proving the bug existed and the fix works.

**Before source edits where impact matters:** run `gg impact <file>` to see
historical bugs, dependents, exported symbols, and related decisions. Capture
the impact/evidence summary where future agents can find it.

<!-- gg-managed:end -->

<!-- gg-bmad:start -->
## BMAD Skill Agents — gg Durable Memory Relay

BMAD rounds may run normally. gg-cli does not own BMAD's workflow; it only
stores durable outputs that future agents need.

BMAD agents (Mary, John, Winston, Amelia, Paige, Sally, and others) run
inside Claude Code sessions. They usually cannot call gg directly. The host
agent must persist durable round outputs into gg:

- After each BMAD round: extract accepted decisions, task proposals, blockers,
  compact evidence summaries, artifact references, handoffs, and rejected approaches that future agents need.
- If a BMAD agent says 'reject X' → `gg record "X" --decision-status=rejected --reason "why"`
- If a BMAD agent proposes durable project work → `gg task create "title" ...`
- If a BMAD agent reaches a conclusion the user accepts → `gg record "conclusion" --reason "..."`

Do this before moving on; otherwise the knowledge stays trapped in one prompt.

<!-- gg-bmad:end -->

<!-- gg:contract:begin v1 -->
## GG DURABLE MEMORY CONTRACT

gg-cli does not own the agent's workflow. Use the native workflow that fits the work: BMAD, GSD, OMO Slim, Antigravity, Codex, Claude Code, Cursor, Aider, a manual shell, or another local process.

The mandatory rule is durable memory sync: anything future agents must know goes into gg.

- Record durable decisions, rejections, bugs, blockers, handoffs, evidence summaries, artifact references, and shared work items with `gg record`, `gg task`, `gg bug`, and `gg tell`. When a decision rests on something you verified, attach the proof with `gg record --evidence "…"` so future agents can tell a checked fact from an unverified claim (empty evidence surfaces as `[unverified]`).
- Use `gg search --compact <topic>` and `gg context --compact` before changing important project behavior so prior decisions and rejected approaches are visible.
- Before editing a shared, exported, or imported symbol, get its blast radius from the code graph — not grep. Run `gg impact <file> --compact` for the dependent files (it follows the real import graph and catches re-exports/barrels and aliased imports that text search silently misses), and `gg lsp refs <file> <line> <col>` for the exact callers of one symbol (live, type-aware). An empty result from a missing/stale graph is not proof of "no dependents" — heed the freshness notice or run `gg index` first. `gg def <name>` answers "where is this defined" without grep.
- `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"`, `gg inbox --role "$GG_ROLE" --peek`, and `gg next --agent "$GG_AGENT" --role "$GG_ROLE"` are orientation helpers. They do not replace the agent's native planning or execution workflow.
- If the project uses gg-managed tasks, use `gg task start/renew/release/ready-for-live/done` according to the configured ownership and review gates. When handing off for review, include a compact evidence packet in `gg task ready-for-live --plan` or `gg tell --task`: commands run, live smoke result, impacted files, known gaps, and artifact paths. If another tool owns local planning, mirror durable outcomes back into gg instead of creating a parallel hidden tracker.
- GSD may be a native scratchpad/helper, but `.gsd/gsd.db` is not canonical shared memory. Do not call `gsd_plan_*` as the durable memory source in projects using gg; record durable GSD outcomes in gg.
- Source files (.go/.ts/.js/.py/.rs/.java): max 500 lines. Test files (*_test.go, *.test.*, *.spec.*): max 800 lines. Oversized files must be split into cohesive modules — extract helpers, split by concern, no god-objects. The pre-task-done gate (30-file-size.sh) surfaces violations; GG_FILE_SIZE_GATE=block escalates to hard fail.
- If this project has a durable commit-message convention (a recorded decision or canon entry — e.g. a decision tagged `convention`/`policy`), enforce it mechanically instead of relying on memory: the commit-message gate ships installed-but-off in every project, so turn it on by writing `.gg/commit-msg.conf` with `GG_COMMIT_MSG_GATE=warn` (then `on` once clean) and `GG_COMMIT_MSG_PREFIX` set to match the convention (knobs: docs/hook-env-vars.md). A rule that lives only in a decision can be missed in review; the gate cannot. Conversely, if a project deliberately has no such convention, leave the gate off.
- Before claiming completion, capture evidence that a future agent can inspect: commands run, live smoke result, impacted files, known gaps, artifact paths, and any required behavior matrix / negative path / legacy compatibility / stale-string / docs/templates/generated artifact checks. When project hooks require it, put the evidence in the commit body as `Review-Convergence: ...`.
- No sycophancy. When the user asserts a factually wrong technical claim (API semantics, framework behavior, security, deployment model, etc.), verify via code/docs, state the correction directly with evidence (file:line, doc link, or runnable repro), propose the correct approach, then ask the user to confirm direction. If the user insists after seeing the evidence, proceed but `gg record` the disagreement so future sessions can trace the call. This rule is factual claims only — subjective preferences (style, naming, layout) are the user's call.

## ENGINEERING BASELINE

Optimize for software that is still correct and understandable years from now, not for finishing this turn fast. Applies to every task. On conflict: explicit user instruction > a decision recorded in gg > this baseline. If you override a line here, record why with `gg record`.

- Match ceremony to blast radius. A leaf-file one-liner needs no architecture review; a shared or exported symbol needs the impact check above. Read the code the change touches before writing it — not the whole repo, and never nothing.
- Reuse before writing — existing modules, utilities, and proven libraries first — and extend the existing pattern rather than introducing a new one. A new abstraction, dependency, framework, or config surface must buy something concrete; put that reason in the `gg record`. Consistency beats cleverness, and a dependency is permanent — the pre-task-done gate (45-dependency-justification.sh) checks that each new one is named in a decision linked to the task; GG_DEP_GATE=block escalates to hard fail.
- Simplest solution that fully solves the stated problem. Scope can be cut; quality inside the agreed scope cannot. Ship no TODO stubs, dead flags, or half-wired paths described as done; the pre-task-done gate (35-stub-scan.sh) surfaces stub markers this task adds, and GG_STUB_GATE=block escalates to hard fail.
- Stay inside your diff. Clean up dead code, duplication, and unclear logic in what you are already changing; anything larger becomes a `gg task`, not an unrequested refactor riding along in someone's bugfix.
- Fail loudly. Validate input, handle the error path, and surface failures. A silent failure is a bug, not a style choice.
- When two implementations both work, choose in this order: correctness → security → reliability → simplicity → maintainability → consistency → performance. Never trade away security or a correct error path for convenience. Measure before optimizing.
- Never invent an API, a flag, or framework behavior. Check the source, the installed version's docs, or a runnable probe — then state which one you used.
- Report what actually happened: the command you ran and its real result, plus what you skipped, could not verify, or assumed. Never round an unverified claim up to "done".
- Decide rather than defer. When a trade-off is yours to make, research it, pick, and record the reason. Ask the user only when either choice would be unsafe or would waste the work if wrong.
<!-- gg:contract:end -->
