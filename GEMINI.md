# Gemini Context

This project uses **gg** as canonical shared memory. The full native-workflow +
durable-memory protocol lives in **AGENTS.md** at the repo root — read it first.

<!-- gg-managed:start -->
## gg Durable Memory Protocol (managed by gg — do not edit this block)

gg-cli does not own the agent's workflow. Use the native workflow that fits
the work, and sync durable outputs into gg so future agents can retrieve them.

Durable outputs include decisions, rejected approaches, shared work items,
bugs/root causes, blockers, handoffs, evidence summaries, and artifact references.
Evidence summaries should stay compact: commands run, live smoke result, impacted files, known gaps, and artifact paths.

Mandatory orientation (run before your first action):

1. Read the full protocol in AGENTS.md at the repo root.
2. Run `gg session-start --agent <agent_id> --role <role>` as your FIRST action (mandatory when a shell is available).
3. Run `gg search --compact <topic>` before changing important behavior.
4. Record durable decisions/rejections/evidence with `gg record`, `gg task`, `gg bug`, or `gg tell`.

Skipping orientation means working blind to prior decisions and rejected approaches.

<!-- gg-managed:end -->

<!-- gg:contract:begin v1 -->
## GG DURABLE MEMORY CONTRACT

gg-cli does not own the agent's workflow. Use the native workflow that fits the work: BMAD, GSD, OMO Slim, Antigravity, Codex, Claude Code, Cursor, Aider, a manual shell, or another local process.

The mandatory rule is durable memory sync: anything future agents must know goes into gg.

- Record durable decisions, rejections, bugs, blockers, handoffs, evidence summaries, artifact references, and shared work items with `gg record`, `gg task`, `gg bug`, and `gg tell`. When a decision rests on something you verified, attach the proof with `gg record --evidence "…"` so future agents can tell a checked fact from an unverified claim (empty evidence surfaces as `[unverified]`).
- Use `gg search --compact <topic>` and `gg context --compact` before changing important project behavior so prior decisions and rejected approaches are visible.
- `gg session-start --agent "$GG_AGENT" --role "$GG_ROLE"`, `gg inbox --role "$GG_ROLE" --peek`, and `gg next --agent "$GG_AGENT" --role "$GG_ROLE"` are orientation helpers. They do not replace the agent's native planning or execution workflow.
- If the project uses gg-managed tasks, use `gg task start/renew/release/ready-for-live/done` according to the configured ownership and review gates. When handing off for review, include a compact evidence packet in `gg task ready-for-live --plan` or `gg tell --task`: commands run, live smoke result, impacted files, known gaps, and artifact paths. If another tool owns local planning, mirror durable outcomes back into gg instead of creating a parallel hidden tracker.
- GSD may be a native scratchpad/helper, but `.gsd/gsd.db` is not canonical shared memory. Do not call `gsd_plan_*` as the durable memory source in projects using gg; record durable GSD outcomes in gg.
- Source files (.go/.ts/.js/.py/.rs/.java): max 500 lines. Test files (*_test.go, *.test.*, *.spec.*): max 800 lines. Oversized files must be split into cohesive modules — extract helpers, split by concern, no god-objects. The pre-task-done gate (30-file-size.sh) surfaces violations; GG_FILE_SIZE_GATE=block escalates to hard fail.
- Before claiming completion, capture evidence that a future agent can inspect: commands run, live smoke result, impacted files, known gaps, artifact paths, and any required behavior matrix / negative path / legacy compatibility / stale-string / docs/templates/generated artifact checks. When project hooks require it, put the evidence in the commit body as `Review-Convergence: ...`.
- No sycophancy. When the user asserts a factually wrong technical claim (API semantics, framework behavior, security, deployment model, etc.), verify via code/docs, state the correction directly with evidence (file:line, doc link, or runnable repro), propose the correct approach, then ask the user to confirm direction. If the user insists after seeing the evidence, proceed but `gg record` the disagreement so future sessions can trace the call. This rule is factual claims only — subjective preferences (style, naming, layout) are the user's call.
<!-- gg:contract:end -->
