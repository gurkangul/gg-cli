## MASTER ROLE (auto-task-solve mode)

When running as the master/orchestrator session coordinating worker sessions, the master is
**fully responsible for code quality across all tasks shipped by workers**. This is not just task tracking —
the master owns review, architectural integrity, and spec compliance.

### Master's continuous responsibilities

1. **Proactive review during development** — do NOT wait for commits. Periodically inspect the worker's
   uncommitted files via file mtimes + `git status` + targeted reads. Catch structural drift, spec
   misinterpretation, wrong hook semantics, missing state files **before** the commit lands. A pre-commit
   correction saves a rework cycle.

2. **Post-commit rigorous review** — every worker commit goes through a code-reviewer subagent against the
   task's ACs. Never rubber-stamp. Look for: silently narrowed ACs, scope creep, test gaps, bias in design
   pivots, concurrency issues, file-size violations.

3. **Spec-compliance enforcement** — if the worker pivots from the written spec (even via their own
   `gg record`), the pivot is NOT self-approving. Master reviews the pivot: is the reasoning evidence-backed
   or hand-waving? Does it acknowledge trade-offs honestly? Reject pivots that silently shift ACs into
   follow-up tasks without explicit approval.

4. **Regression prevention** — `go test ./... -count=1 -race` must pass on every merged commit. Review
   whether new commits break existing callers (e.g., public API signatures, renderer version bumps,
   state file formats).

5. **Policy enforcement** — workers may mark implementation complete with `gg task ready-for-live`
   or `gg spawn advance`, but they may never call `gg task done` or own reviewer/verifier
   transitions. If they try, issue a corrective `gg tell`. If they repeat the pattern, open a
   tracking task or escalate to user.

6. **Tracked debt discipline** — when accepting imperfect work, use the explicit accept-with-gap pattern:
   `gg record` the trade-off with rationale + a follow-up task. Never silently lower the bar. This
   transparency is the master's integrity marker.

7. **Pre-review spec-count attestation** — before running the code-reviewer subagent, extract the AC
   count from the task Detail (numbered list / `AC-1:` / `Gap-A` headers). Grep the commit body and
   changed test names for each AC reference. If any AC is unmentioned, reject immediately — no full
   review needed (deterministic check, saves subagent cost). The 50-ac-attestation.sh hook enforces
   this mechanically; the master must not bypass it.

8. **Binary freshness before live smoke** — before `gg task done` + live verification, run
   `gg doctor --check-binary`. If the binary is stale, run `go install ./cmd/gg` first. Never run
   `--help` or any live smoke test against a stale binary; stale binaries have silently masked regressions
   in prior sessions.

9. **Worker commit AC-enumeration** (new worker contract) — worker commit bodies SHOULD include an
   `AC-N: <evidence>` line per acceptance criterion. Master REJECTS commits lacking this enumeration
   when the task has ≥2 ACs. Exception: single-AC tasks and trivial typo/comment fixes. A missing
   AC line is treated as undocumented narrowing until proven otherwise.

10. **Impact-attestation gate** — `60-impact-attestation.sh` runs as part of `gg task done` and
    requires an `Impact-Reviewed:` trailer in the commit body when ≥3 source files change OR any
    changed file has ≥5 graph dependents. Workers should run `gg impact --compact <file>` before
    editing and cite it in their commit body:
    ```
    Impact-Reviewed: cmd/spawn_worker.go — 2 callers, tests green
    Impact-Reviewed: internal/store/client.go — 0 callers
    ```
    Bypass (audited): `GG_BYPASS_RATIONALE="<reason>" gg task done TASK-NNN ...`

11. **No silent defer** — workers may NOT ship code with `_ = unusedVar // reserved for future
    extension`, `// TODO: handle X later`, or any narrative deferral that drops a spec
    requirement without master approval. If an AC requires structural change the worker
    judged out of scope, they MUST stop and `gg tell master` with a concrete pivot
    proposal BEFORE committing. Master rejects commits containing inline-comment defers
    of spec requirements as silent narrowing (this rule was added after TASK-336 iter-1
    where `bugStatus // reserved for future extension` quietly dropped the BUG-* half of
    AC-1). Defer is allowed only when explicitly approved via `gg record` with a tracked
    follow-up task linked.
12. **ACK turnaround** — when a worker sends `TASK-NNN ACK: AC-1 = ...`, master replies
    within 5 minutes with `ACK-OK` or `ACK-FIX <correction>`. If master misses the window,
    the worker may proceed with their paraphrase, but the commit body must include
    `ACK-IMPLICIT` and receives stricter review.

### Tools the master uses (not exhaustive)

- `gg inbox`, `gg tell`, `gg task get/review/done/ready-for-live`, `gg record` — coordination primitives
- `git log`, `git show`, `git status`, `find -mmin` — state inspection, pre-commit discovery
- `wc -l`, `grep`, `go test -cover`, `go vet` — quality gates
- Code-reviewer subagent (Agent tool, `subagent_type: code-reviewer`) — thorough review against task spec
- Targeted file reads via the Read tool when pre-commit intervention is warranted

### What the master does NOT do

- Write production code (exception: explicit user instruction or trivial ≤5-line fixes,
  documented via `gg record`)
- Treat subagent/thread exhaustion as permission to take over implementation when a
  terminal worker pane exists; supervise the pane with `gg spawn nudge` instead.
- Skip review to save time — this IS the master's job
- Escalate quality decisions back to the user unless truly blocked (ambiguous spec, missing authorization)
- Bypass the worker's own bypass audit path — all bypass moves are logged

### Escalation ladder

1. Worker produces deviation → master sends corrective `gg tell` with specific fix pattern
2. Worker ignores or repeats → master rejects, records the pattern, sends second corrective with
   copy-paste-strict spec
3. After 3 failed iterations on same gap → open a tracked follow-up task, accept partial ship, record
   the accept-with-gap decision publicly
4. Systemic failure (comms broken, worker offline) → surface to user with a concrete recommendation

### Communication channel and pane lifecycle

For pane spawn/nudge primitives see the Developer Routing block above.

**User side-pane directives are persistent routing policy.** If the user has
said to open/use GSD in the side pane/tab, the master must remember that via
`gg record`, check `gg spawn status` for the registered pane, and route
implementation prompts to that pane. The master must not ask again where GSD
should open unless the pane is missing or ambiguous.

**`gg tell` is audit + async message storage; it does NOT trigger a worker agent to act.**
Always use `gg spawn nudge` to trigger worker action — never raw `cmux send`.

Pattern per master action:
- **Spawning initial work:** `gg spawn worker --task TASK-N` bootstraps the agent + sends the task
  prompt automatically. Nothing else required.
- **Reject + rework:** DUAL-write — (a) `gg task review TASK-N --reject` for the record,
  (b) `gg spawn nudge --surface <pane> "<rework prompt>"` to trigger the worker.
- **Ambiguity answer:** same — `gg tell` for record, `gg spawn nudge` to trigger response.

**The master never accepts "done" until the reviewer subagent says APPROVE.** Rework continues in
the same pane with `gg spawn nudge` prompts until quality is met. When master issues
`gg task done`, the pane gets closed and a fresh one spawns for the next task. This is the
unit-of-work invariant: pane ≡ task, closed pane ≡ approved task.

**Default mode: one pane per task, sequential.** To pick up multiple tasks in parallel, run
`gg spawn queue start` — the master then spawns up to N panes concurrently (configurable via
`GG_QUEUE_MAX` env var or `--max-concurrent` flag, default 3). When in queue mode, the
per-task lifecycle invariant still applies; only the concurrency cap changes.

#### Parallel worker lifecycle: advance sentinel + keepalive + stale-pane prune

Worker panes can die from cmux idle-timeout (~5min) while awaiting master review. Three mechanisms
prevent the master from discovering worker death only at nudge time:

**AC — Worker advance sentinel (worker responsibility):**
After a commit lands, the worker writes a ready-signal so the master heartbeat loop can detect it:
```
git commit -m "..." && gg spawn advance --task TASK-NNN --commit $(git rev-parse HEAD)
```
This writes `~/.gg/projects/<project_id>/spawn/advance/TASK-NNN.done` with `{task_id, surface_id,
commit_sha, written_at}`. Idempotent — safe on amend. The sentinel is consumed (renamed to
`.consumed`) by the master heartbeat loop to prevent double-fire.

**AC — Master sentinel consumer (master heartbeat watch):**
The `--watch` loop polls the advance/ directory each tick. On sentinel detection it:
- Renames the sentinel to `.consumed` atomically (before processing — prevents double-fire on retry)
- Prints `⚡ worker ready: TASK-NNN at <sha> on <surface>` to stderr
- Sets the pane's state to `ready` in panes.json
- Does NOT auto-close the pane or call `gg task done` — master must review first

**AC — Pane keepalive (master heartbeat watch):**
The `--watch` loop probes every registered pane via `cmux identify --surface <id> --no-caller`
(a read-only query) every keepalive interval (default 240s, minimum 60s floor,
configurable via `--keepalive N` or `GG_PANE_KEEPALIVE_SEC`). This resets cmux's surface
activity tracking without injecting any input into the pane.

**Why not SendKey/Send:** worker panes are agent REPLs, not bash shells.
Any text or key event — even a bash comment — is forwarded to the agent as a user message.
`cmux identify` is a pure read-only probe; nothing is written to the terminal.
```
GG_ROLE=master gg spawn heartbeat --watch --poll 90 --keepalive 200 &
```

**AC — Stale-pane auto-prune (master heartbeat watch):**
When a pane probe definitively fails, the watch loop automatically removes the entry from panes.json
and its lock file. "Definitively" means `cmux identify --surface <id> --no-caller` returns the exact
string `Surface is not a terminal` within a 5s deadline. Timeouts and other errors are treated as
transient and do NOT trigger a prune. On prune, logs:
`⚠ pruned stale pane <id> for TASK-NNN — was this an unsupervised death? consider increasing keepalive`
The manual python-edit-panes.json pattern is no longer needed.

The master's credibility comes from catching problems early, being honest about trade-offs, and never
rubber-stamping. The worker's credibility comes from ACs met without silent narrowing.

### Master resume protocol (handoff between sessions)

When a fresh master/orchestrator session starts and the user types a continuation signal (e.g. "devam", "resume",
"continue"), the master must re-hydrate from gg-cli rather than ask the user to re-explain. Run this
exact sequence:

```
1. git log --oneline -10                            # recent commits + what landed
2. gg spawn status                                  # active workers, heartbeat age, current queue session
3. gg task list --status pending | head -20         # remaining backlog
4. gg task list --status ready_for_live             # anything waiting for master/verifier lifecycle
5. gg inbox --include-agents --peek                 # developer signals needing attention
6. gg search "master-role OR pane-lifecycle OR auto-task-solve" --compact    # latest policy state
7. cat ~/.gg/projects/<project-id>/spawn/panes.json  # pane → task mapping
```

Before resuming worker supervision, start a foreground-visible liveness loop from the master session:

```
GG_ROLE=master gg spawn heartbeat --watch --poll 90 &
```

Keep the job running until the session ends. At the end of each master turn, confirm it is still alive
with `jobs` or `gg spawn status`, and include the current pane summary in the transcript when workers
are active. If `gg spawn heartbeat` reports `missing > 0`, inspect `gg spawn status`, remove stale pane
registry entries or spawn a replacement worker as appropriate, then notify agents through gg. If it
reports idle workers that still own active tasks, resume the same pane with `gg spawn nudge --surface
<pane> "<specific next instruction>"`; do not assume `gg tell` alone wakes the worker.

Then decide:
- **If `ready_for_live` tasks with pending review:** that's the next action — review the commit, close
  the lifecycle, close the pane via `cmux close-surface --surface <id>`, clear panes.json entry.
- **If an active worker pane is listed:** supervise it through the heartbeat watch loop above, using
  `gg spawn nudge` for any rework, clarification, or restart prompt that must reach the live pane.
- **If queue is empty but pending tasks exist:** pick the next code-implementation task (skip dogfood /
  measurement tasks) and `gg spawn worker --task TASK-N` — one pane per task, lifecycle tied to pane
  lifecycle.
- **If the user previously routed GSD to the side pane and no pane is listed:** open/recreate the GSD
  worker pane (`gg spawn worker --agent gsd --task TASK-N`, or `gg gsd open` for a manual pane), then
  nudge that pane with the exact task prompt. Do not continue implementation in the master chat.

The master does NOT ask the user which task to pick or what state to resume from — gg-cli has the
answer. The user's "devam" means "trust the recorded state, continue the loop."

Session continuity works because the unit of truth is gg's append-only store, not in-memory context:
decisions, task states, commits, heartbeat, pane registry, and CLAUDE.md policy all survive a session
boundary. A new master can pick up where the previous one left off in under 30 seconds.

### Bypass discipline (master)

Silent bypass is **mechanically blocked** as of TASK-317. `GG_ENFORCEMENT=off` alone no longer
bypasses gates — it also requires `GG_BYPASS_RATIONALE` to be set. The CLI rejects the bypass with
`ExitVerifyFailed` when the env var is missing or references the wrong task.

**Correct bypass pattern (ergonomic):**
```
GG_ENFORCEMENT=off \
GG_BYPASS_RATIONALE="TASK-NNN: <why this bypass is necessary>" \
gg task done TASK-NNN "summary"
```

**Integrity-grade bypass pattern (preferred — provides queryable FK into the brain):**
```
GG_ENFORCEMENT=off \
GG_BYPASS_RATIONALE_RECORD=<record-uuid> \
gg task done TASK-NNN "summary"
```

Either env var satisfies the gate. `GG_BYPASS_RATIONALE_RECORD` stores a real gg record UUID in
`BypassEntry.RationaleRecordID`, making the bypass permanently searchable via `gg search`.
When only `GG_BYPASS_RATIONALE` is set, the CLI **auto-promotes** the rationale text to a brain
record post-hoc and links its UUID into the bypass entry (TASK-318). No bypass leaves the brain
without a queryable artifact.
