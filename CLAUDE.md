

<!-- gg:contract:begin v1 -->
## GG MANDATORY CONTRACT

gg-cli is the mandatory coordination channel for this project.

- All tasks, decisions, bugs, and broadcasts go through gg: `gg task`, `gg record`, `gg bug`, `gg tell`
- Agent identity terms are generic: `agent_id` is the unique runtime instance (for example `omo-slim`, `codex-1`, `claude-planner`), `role` is the work authority (for example `implementer`, `reviewer`, `planner`), and task `owner` is the leasing `agent_id`.
- Before starting new work, run: `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"`, then `gg inbox --role "$GG_ROLE" --peek`, `gg task list --ready --compact`, and `gg context --compact` for project-level onboarding.
- Never call `gsd_plan_*` tools in projects using gg — use `gg task create` instead
- Before starting any new task or reasoning step, run: `gg inbox --role "$GG_ROLE" --peek`.
  Use a unique `GG_AGENT` per runtime. Do not run role-less `gg inbox --advance-cursor`; it is rejected because it can hide role-targeted assignments.
  If role-targeted unread messages exist, you MUST either:
    (a) start the referenced runnable task via `gg task start <id> --owner "$GG_AGENT" --lease 30m`, OR
    (b) if the task is already `ready_for_live` and your role is reviewer/verifier, hydrate with `gg task get <id> --review` and review it, OR
    (c) if the linked task is already closed, treat it as stale assignment noise, OR
    (d) reply with `gg tell <sender> <deferral reason>`
  Silent skip = protocol violation. It will be caught by structural gates.
- Source files (.go/.ts/.js/.py/.rs/.java): max 500 lines. Test files (*_test.go, *.test.*, *.spec.*): max 800 lines.
  Oversized files must be split into cohesive modules — extract helpers, split by concern, no god-objects.
  The pre-task-done gate (30-file-size.sh) surfaces violations; GG_FILE_SIZE_GATE=block escalates to hard fail.
- Before claiming done, run the review convergence matrix and put the evidence in the commit body as
  `Review-Convergence: ...`. The matrix is: behavior matrix, negative path, legacy compatibility,
  stale-string sweep, docs/templates/generated artifacts, live smoke, and test/diff evidence.
  The pre-task-done gate (70-review-convergence.sh) blocks task close when this attestation is missing.
- No sycophancy. When the user asserts a factually wrong technical claim (API semantics, framework behavior,
  security, deployment model, etc.), DO NOT silently comply. Verify via code/docs, state the correction directly
  with evidence (file:line, doc link, or runnable repro), propose the correct approach, then ask the user to
  confirm direction. If the user insists after seeing the evidence, proceed but `gg record` the disagreement so
  future sessions can trace the call. This rule is factual claims only — subjective preferences (style, naming,
  layout) are the user's call.
<!-- gg:contract:end -->
