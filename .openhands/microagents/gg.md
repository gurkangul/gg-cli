---
name: gg
type: knowledge
always: true
---

# gg Durable Memory (OpenHands microagent — managed by gg)

The full native-workflow + durable-memory protocol lives in **AGENTS.md** at the
repo root. Read it first.

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
