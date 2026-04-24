

<!-- gg:contract:begin v1 -->
## GG MANDATORY CONTRACT

gg-cli is the mandatory coordination channel for this project.

- All tasks, decisions, bugs, and broadcasts go through gg: `gg task`, `gg record`, `gg bug`, `gg tell`
- Before starting new work, run: `gg inbox`
- Never call `gsd_plan_*` tools in projects using gg — use `gg task create` instead
- Before starting any new task or reasoning step, run: `gg inbox --role $GG_ROLE --since-cursor`.
  If role-targeted unread messages exist, you MUST either:
    (a) start the referenced task via `gg task start <id>`, OR
    (b) reply with `gg tell <sender> <deferral reason>`
  Silent skip = protocol violation. It will be caught by structural gates.
- Source files (.go/.ts/.js/.py/.rs/.java): max 500 lines. Test files (*_test.go, *.test.*, *.spec.*): max 800 lines.
  Oversized files must be split into cohesive modules — extract helpers, split by concern, no god-objects.
  The pre-task-done gate (30-file-size.sh) surfaces violations; GG_FILE_SIZE_GATE=block escalates to hard fail.
- No sycophancy. When the user asserts a factually wrong technical claim (API semantics, framework behavior,
  security, deployment model, etc.), DO NOT silently comply. Verify via code/docs, state the correction directly
  with evidence (file:line, doc link, or runnable repro), propose the correct approach, then ask the user to
  confirm direction. If the user insists after seeing the evidence, proceed but `gg record` the disagreement so
  future sessions can trace the call. This rule is factual claims only — subjective preferences (style, naming,
  layout) are the user's call.
<!-- gg:contract:end -->

<!-- gg:master-role:begin v1 -->
## MASTER ROLE (Opus — auto-task-solve mode)

When running as the master (claude-code / Opus) coordinating worker sessions (GSD / Sonnet), the master is
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

5. **Policy enforcement** — workers may never call `gg task done | ready-for-live | review`. If they try,
   issue a corrective `gg tell`. If they repeat the pattern, open a tracking task or escalate to user.

6. **Tracked debt discipline** — when accepting imperfect work, use the explicit accept-with-gap pattern:
   `gg record` the trade-off with rationale + a follow-up task. Never silently lower the bar. This
   transparency is the master's integrity marker.

### Tools the master uses (not exhaustive)

- `gg inbox`, `gg tell`, `gg task get/review/done/ready-for-live`, `gg record` — coordination primitives
- `git log`, `git show`, `git status`, `find -mmin` — state inspection, pre-commit discovery
- `wc -l`, `grep`, `go test -cover`, `go vet` — quality gates
- Code-reviewer subagent (Agent tool, `subagent_type: code-reviewer`) — thorough review against task spec
- Targeted file reads via the Read tool when pre-commit intervention is warranted

### What the master does NOT do

- Write production code (exception: trivial ≤5-line fixes, documented via `gg record`)
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

The master's credibility comes from catching problems early, being honest about trade-offs, and never
rubber-stamping. The worker's credibility comes from ACs met without silent narrowing.
<!-- gg:master-role:end -->
