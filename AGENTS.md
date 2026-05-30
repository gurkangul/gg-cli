---
agents_schema: "2.1"
---

# Agent Guidance

## Project Context — gg-cli

**What this project does:** A CLI (`gg`) that gives multiple AI agents a
shared brain. Every agent (Claude Code, Codex, Cursor, Aider, …) reads
the same decisions, tasks, rejections, discussions, notes, and code graph
through gg — no agent starts from a blank slate.

> **Note on GSD:** GSD may be run manually by the human or by an agent in its
> own terminal, but gg remains the canonical shared memory/evidence ledger.
> What is forbidden is treating GSD's own planner state as durable shared
> memory: do not create shared project tasks with `gsd_plan_*` or treat
> `.gsd/gsd.db` as canonical. Mirror any meaningful GSD outcome into gg with
> `gg task`, `gg record`, `gg bug`, or `gg tell`.

**Who it's for:** Developers running 2+ AI agents in parallel terminals who
keep hitting the same three pains:
1. Each agent re-derives context from scratch
2. Impact-blind fixes create fix loops
3. Rejected approaches keep getting re-proposed

**Key constraints (non-negotiable):**
- **No daemon, no network.** gg is a CLI + local Docker (Qdrant + Ollama +
  Memgraph). No background process, no telemetry that leaves the machine.
  CodeGraph freshness follows the same contract: no automatic background indexer;
  `gg doctor --fix-index` is the canonical one-shot repair, while
  `gg index --watch` / `gg watch --index` are explicit foreground watchers that
  run only until Ctrl-C.
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
- `docs/hook-env-vars.md` — every `GG_*` env var a hook can read or set; propagation rules; bypass patterns
- `gg search "<topic>" --compact` — past decisions and rejections on the subject
- `gg context "<topic>" --compact` — unified bundle (decisions + tasks + code impact)

---

## How agents work in this project

This project uses a shared knowledge base CLI: **gg** (the project itself).
gg does not own the agent's workflow; agents may use BMAD, GSD, OMO Slim,
Antigravity, Codex, Claude Code, Cursor, Aider, a manual shell, or another
local process.

The mandatory rule is durable memory sync: anything future agents need to know
is recorded via `gg` — decisions, rejected approaches, task/story outputs,
bugs, root causes, evidence summaries, artifact references, blockers, and
handoffs.

Minimal evidence packet for review or handoff: commands run, live smoke result,
impacted files, known gaps, and artifact paths. Keep bulky logs/screenshots/traces
in their native location and write only the compact summary/reference into gg.

Every agent runs independently in its own terminal but they all write to the
same Qdrant + Ollama backend. A decision made by one agent is immediately
visible to the others through shared memory.

> The user will NEVER ask you to run `gg`. You detect the durable-memory moment
> and invoke it automatically.

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

The eight enforceable rules derived from that meta-rule:

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

8. **Review convergence before done.** The implementer must run the same
   convergence matrix before claiming done that a later "review et" pass
   would run: behavior matrix, negative path, legacy compatibility,
   stale-string sweep, docs/templates/generated artifacts, live smoke, and
   test/diff evidence. Put the result in the commit body as
   `Review-Convergence: ...`. The `70-review-convergence.sh`
   pre-task-done gate blocks task close when this attestation is missing.
   Bypass only with `GG_ALLOW_INCOMPLETE_REVIEW="<reason>"`, which is
   audited via `gg record`.

These rules are individually recorded via `gg record` (tags:
`process`, `discipline`) so they appear in `gg search --compact
"process discipline"` and cannot be quietly ignored.

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

The `GG_AGENT` export tags every subsequent gg call as agent-initiated in
telemetry — without it the dogfood metric undercounts and gives false signals
about adoption. Set it once per shell and do not leave a stale value from a
different runtime.

Set `GG_AGENT` to the runtime that is actually executing commands. If a side
pane is a real GSD runtime, use a unique `gsd-*` value such as
`GG_AGENT=gsd-gg-cli-1`. If Claude Code, Codex, Cursor, or another runtime is
driving GSD work, keep that runtime's identity in `GG_AGENT` and use `GG_ROLE`
for authority (`master`, `developer`, `reviewer`, etc.).

Terms: `agent_id` is the unique runtime instance name; `role` is the authority
for the current work; task `owner` is the `agent_id` holding a gg-managed task
lease; inbox `role` / `audience` route messages. Use a unique `GG_AGENT` per
runtime, even when two agents share one `GG_ROLE`.

Role inbox reads should use `--role "$GG_ROLE" --peek`. Do not run role-less
`gg inbox --advance-cursor`; the CLI rejects it because it can hide role-targeted
assignments from a future agent.

After orientation, summarize the open tasks, unread messages, and recent
decisions for the user when useful. See `docs/agent-protocol-v1.md` for the
full native-workflow + durable-memory protocol.

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

## DURABLE WORK ITEMS

When a unit of work, story output, or follow-up must be visible outside one
agent session:
```
gg task create "title" --detail "description" --priority high --requester user --tags "tag1,tag2"
```
Tell the user: "Opened task TASK-XXX."

Do not create gg tasks for private scratchpad steps that do not matter to future
agents.

## WHEN USING GG-MANAGED TASKS

When the user says "work on the tasks", "continue", "keep going", "devam et",
or names a specific `TASK-XXX`, use the gg task lifecycle because this project
uses gg-managed tasks:

1. `gg inbox --role "$GG_ROLE" --peek` — handle role-targeted assignments.
2. `gg status` — see pending tasks + open discussions + inbox.
3. **Open discussions first**: if any `DISC-NNN` is open, close it (resolve
   or dismiss) before picking work. Unresolved discussions block new work
   because the decision they represent may change which task matters.
4. List runnable tasks with `gg task list --ready --compact` (there is no
   `gg task ready` subcommand in the current CLI).
5. Check recent agent broadcasts with `gg inbox --include-agents --since 2h --peek` —
   has another agent already claimed a task? If yes, skip those.
6. Pick the highest-priority unclaimed pending task unless the user named one.
7. Claim it with an owner lease and status broadcast:
   `gg task start TASK-XXX --owner "$GG_AGENT" --lease 30m`
   `gg tell "all" "TASK-XXX started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-XXX`
8. Hydrate before work: `gg task get TASK-XXX` and
   `gg context --for-task TASK-XXX`.
9. Before editing each source file, run `gg impact <file> --compact`.
10. Work in the native tool of choice and test. Renew long leases with
    `gg task renew TASK-XXX --owner "$GG_AGENT" --lease 30m`.
11. Implementers do **not** close tasks in this project. After local verification, run
    `gg task get TASK-XXX` (required hydration; `gg context` alone is not enough), then
    `gg task ready-for-live TASK-XXX --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=<cmds run>; live=<smoke result>; impact=<files checked with gg impact>; gaps=<none|known gap>; artifacts=<paths>" --from "$GG_ROLE"`
    and `gg tell reviewer "TASK-XXX ready. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <paths>" --from "$GG_ROLE" --task TASK-XXX`.
12. Release only when abandoning or handing off unfinished `in_progress` work:
    `gg task release TASK-XXX --owner "$GG_AGENT"`. Do not release after
    `ready-for-live`; the current CLI only releases `in_progress` tasks.

If another native tool owns local planning, keep using it and mirror durable
outputs into gg instead of forcing every scratchpad step into `gg task`.

## PRE-DONE VERIFY GATE

`gg task done` runs `.gg/hooks/pre-task-done.d/*.sh` **before** writing the new
state to the store. If any script exits non-zero the command aborts with exit
code `7` (`ExitVerifyFailed`) and the task stays in its current state — agents
should treat this as "fix the failure and retry", not as a normal error.

The canonical test command — used by CI and the verify gate — is
`go test ./... -count=1 -race -timeout=120s`. CI never uses `-short`. See
`docs/testing.md` for the full rationale.

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

**AC attestation gate (`50-ac-attestation.sh`):**

The AC attestation hook catches *silent AC narrowing* — committing without
demonstrating coverage of each acceptance criterion in the task spec. It
blocks `gg task done` when any AC anchor found in the task Detail is not
referenced in the commit message.

How it works — parser (5 ordered passes over the entire Detail text):
1. Explicit `AC-N:` lines anywhere (e.g. `AC-1: something`)
2. `Gap A / Gap B / Gap N` style lines — in `ACCEPTANCE`, `ACS`, or `GAPS` sections, or using strict `Gap N:` colon form outside those sections (excludes `FIX`/`REWORK` sections)
3. Numbered items at line start: `1.`, `1)`, `1:` (in any section)
4. `- ` bullets under an `ACCEPTANCE` heading (fallback)
5. All remaining `- ` bullets (excluding `FIX`/`REWORK` section bullets — those are implementation steps, not ACs)

Anchors are deduplicated by text; the pass order is priority only. Each
found anchor is assigned a display label (AC-1, AC-2, …) for the
failure message.

Commit reference rules — the hook accepts **any one** of these per AC:
- **(a)** `AC-N:` anywhere in the commit body (preferred, e.g. `AC-1: implemented blocking logic`)
- **(b)** `N.` / `N)` / `N:` at the start of a commit line (numbered-list style)
- **(c)** `AC N` phrase in the commit body (e.g. `AC 1 is covered by Y`)
- **(d)** test name containing the AC number in the diff added lines or changed file paths: `func TestAC1_Something` or `TestAC1_something_test.go`; Gap items also match `TestGapA_*`
- **(e)** func/comment reference in diff added lines: `func ac1_impl`, `// AC-1 <note>`, `// Gap A`, or `# AC-1 <note>`

All five rules are fully enforced — any one that fires satisfies the AC.
Exits 7 with an enumeration of unmatched ACs if none fire for a given AC.

**Passing commit example:**
```
feat(TASK-042): implement AC attestation hook

AC-1: hook exits 7 when anchors not in commit; tested in cmd/hook_ac_attestation_test.go
AC-2: bypass GG_ALLOW_INCOMPLETE_AC audited via gg record
AC-3: integration test — 3 ACs + 2 refs blocks; 3 ACs + 3 refs passes
AC-4: documented in AGENTS.md
```

**Failing commit example** (two ACs unaccounted — hook blocks with exit 7):
```
feat(TASK-042): partial work

Only covered AC-1 here.
```
```
[pre-task-done] AC attestation FAILED for TASK-042
Unmatched ACs (2 of 4):
  AC-2: bypass GG_ALLOW_INCOMPLETE_AC audited via gg record
  AC-3: integration test — 3 ACs + 2 refs blocks; 3 ACs + 3 refs passes
To fix — pick any one format per AC:
  (a) AC-N: line in commit body (e.g. "AC-2: implemented via X")
       AC-2: <how this criterion was addressed>
       AC-3: <how this criterion was addressed>
  (b) numbered reference at line start (e.g. "2. addressed via X")
  (c) "AC N" phrase in commit body (e.g. "AC 2 is covered by Y")
  (d) test name in diff added lines or changed file paths: func TestAC2_YourTest
  (e) func/comment in diff added lines: func ac2_impl or // AC-2 <note>

To bypass (audited):
  GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done TASK-042 ...
```

Modes (env `GG_AC_ATTESTATION`): `on` (default, blocking) | `warn` | `off`

Bypass: `GG_ALLOW_INCOMPLETE_AC="<reason>" gg task done TASK-NNN ...` — the
bypass is audited via `gg record` so it appears in future `gg search` results.

## BUG FIX DURABLE CONTEXT PRE-FLIGHT

Bugs regress when agents edit code without knowing the blast radius. These
three queries take under a minute and preserve durable fix context:

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
  `gg tell "all" "TASK-016 started by $GG_AGENT ($GG_ROLE)" --from "$GG_ROLE" --audience agents --task TASK-016`
- Choosing an approach among alternatives other agents might care about:
  `gg tell "all" "TASK-016: picked neo4j-go-driver over mgclient-go — Bolt support, active maintenance" --from "$GG_ROLE" --audience agents --task TASK-016`
- Hitting a blocker that affects shared assumptions:
  `gg tell "all" "TASK-016 blocked: Go 1.26 incompatibility in neo4j driver, investigating workaround" --from "$GG_ROLE" --audience agents --task TASK-016`
- Handing off for independent verification after `gg task ready-for-live`:
  `gg tell reviewer "TASK-016 ready for review: Memgraph Go client live, internal/graph/ ready for TASK-007" --from "$GG_ROLE" --task TASK-016`

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

The host agent is responsible for **extracting durable outputs from their result
and executing the `gg` calls** as soon as the round completes. Concretely:

- A subagent says "we should reject X because Y" → you run `gg record "X" --decision-status rejected --reason "Y"`
- A subagent proposes durable project work / a punch list → you run `gg task create` for each item future agents need
- A subagent reaches a conclusion the user accepts → you run `gg record "conclusion" --reason "why"`

Do this before moving on; otherwise the knowledge stays trapped in one prompt.

## NEVER

- Make durable decisions without `gg`
- Re-propose a previously rejected approach (search first)
- Say "we'll do that later" for durable work without opening a task
- Ask the user to run `gg` commands — you run them
- Finish a subagent round without persisting its durable decisions/tasks/rejections to `gg`
- Broadcast every step — only broadcast moments other agents genuinely need
- Run `gg bug fix` without durable impact/root-cause evidence — unobserved blast radius is how bugs keep regressing

<!-- gg-managed:start -->
## gg Durable Memory Protocol (managed by gg — do not edit this block)

gg-cli does not own the agent's workflow. Use the native workflow that fits
the work, and sync durable outputs into gg so future agents can retrieve them.

Durable outputs include decisions, rejected approaches, shared work items,
bugs/root causes, blockers, handoffs, evidence summaries, and artifact references.
Evidence summaries should stay compact: commands run, live smoke result, impacted files, known gaps, and artifact paths.

Recommended orientation:

1. Read this entire AGENTS.md file.
2. Run `gg session-start --agent <agent_id> --role <role>` when available.
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

- Record durable decisions, rejections, bugs, blockers, handoffs, evidence summaries, artifact references, and shared work items with `gg record`, `gg task`, `gg bug`, and `gg tell`.
- Use `gg search --compact <topic>` and `gg context --compact` before changing important project behavior so prior decisions and rejected approaches are visible.
- `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"`, `gg inbox --role "$GG_ROLE" --peek`, and `gg next --agent "$GG_AGENT" --role "$GG_ROLE"` are orientation helpers. They do not replace the agent's native planning or execution workflow.
- If the project uses gg-managed tasks, use `gg task start/renew/release/ready-for-live/done` according to the configured ownership and review gates. When handing off for review, include a compact evidence packet in `gg task ready-for-live --plan` or `gg tell --task`: commands run, live smoke result, impacted files, known gaps, and artifact paths. If another tool owns local planning, mirror durable outcomes back into gg instead of creating a parallel hidden tracker.
- GSD may be a native scratchpad/helper, but `.gsd/gsd.db` is not canonical shared memory. Do not call `gsd_plan_*` as the durable memory source in projects using gg; record durable GSD outcomes in gg.
- Source files (.go/.ts/.js/.py/.rs/.java): max 500 lines. Test files (*_test.go, *.test.*, *.spec.*): max 800 lines. Oversized files must be split into cohesive modules — extract helpers, split by concern, no god-objects. The pre-task-done gate (30-file-size.sh) surfaces violations; GG_FILE_SIZE_GATE=block escalates to hard fail.
- Before claiming completion, capture evidence that a future agent can inspect: commands run, live smoke result, impacted files, known gaps, artifact paths, and any required behavior matrix / negative path / legacy compatibility / stale-string / docs/templates/generated artifact checks. When project hooks require it, put the evidence in the commit body as `Review-Convergence: ...`.
- No sycophancy. When the user asserts a factually wrong technical claim (API semantics, framework behavior, security, deployment model, etc.), verify via code/docs, state the correction directly with evidence (file:line, doc link, or runnable repro), propose the correct approach, then ask the user to confirm direction. If the user insists after seeing the evidence, proceed but `gg record` the disagreement so future sessions can trace the call. This rule is factual claims only — subjective preferences (style, naming, layout) are the user's call.
<!-- gg:contract:end -->
