# SocratiCode to gg-cli Evaluation Matrix (TASK-384)

This matrix evaluates feature ideas from SocratiCode against gg-cli constraints and roadmap direction.

## Constraints used for verdict rationale

- No hidden daemon behavior
- Local-only execution and storage
- Agent-native subprocess model (CLI invoked by agents)
- Project isolation (UUID-scoped data boundaries)
- Forward-only memory model

## Verdict key

- **adopt**: implement concept substantially as-is (clean-room)
- **adapt**: implement a constrained gg-shaped variant
- **reject**: conflicts with gg direction or hard constraints
- **defer**: useful but intentionally postponed
- **already covered**: capability exists in gg today

## Feature matrix

| Area | SocratiCode capability | gg-cli current state | Verdict | gg task link | Rationale (constraint-tied) |
|---|---|---|---|---|---|
| Onboarding / plugins | Agent-facing packaging with install helpers and role guidance | Core CLI exists; packaging/onboarding consistency across agents needs polish | adapt | TASK-379 | Keep agent-native subprocess invocation and local config flow; no server/plugin runtime that introduces daemon coupling. |
| MCP / server lifecycle | Long-lived MCP server process, lifecycle management, tool transport | gg intentionally runs as CLI subprocess calls from agents | reject | n/a | Conflicts with **no hidden daemon** and subprocess-native model; adding server lifecycle would blur isolation and operability boundaries. |
| Code search quality benchmarking | Side-by-side quality/time comparisons for developer queries | gg has search/context/impact but benchmark artifact needs stronger reproducibility | adopt | TASK-373 | Supports local-only evidence-driven improvement without architectural drift; aligns with transparent open-source posture. |
| Graph / impact depth | Rich cross-file impact graph and navigation affordances | gg has impact graph; freshness/quality visibility and deeper ergonomics are partial | adapt | TASK-375, TASK-374 | Keep graph local and project-scoped; expand depth while preserving deterministic CLI outputs and isolation. |
| Context artifacts | Inclusion of non-code artifacts (schemas/specs/docs) in retrieval | gg context focuses on decisions/tasks/messages/graph; artifact enrichment pending | adopt | TASK-376 | Fits local-only storage and forward-memory model when artifact indexing is project-scoped and hash-tracked. |
| Watcher (file index) | Automatic/watch-based background index updates | gg has explicit indexing; watcher mode is not default | adapt | TASK-380 | Only acceptable as **explicit foreground command**; preserves no-daemon rule and terminal lifecycle ownership. |
| **Supervisor / agent-triggering** | Agents can actively trigger/coordinate follow-up work | gg has messaging (`gg tell`) but trigger reliability is a known gap | adapt | TASK-385 | Must be solved separately from file-index watcher; design should remain subprocess/local and avoid hidden resident scheduler. |
| Cross-project / branch behavior | Linked project context and multi-repo awareness | gg has strict project isolation; linked read-only context is pending | adapt | TASK-382 | Allow read-only federation while preserving project_id write boundaries and isolation guarantees. |
| Testing / benchmark posture | Demonstrated evals and quality evidence bundled with feature claims | gg has CI/tests; comparative benchmark methodology needs committed artifact | adopt | TASK-373 | Improves commercialization/open-source credibility through reproducible local measurements, not marketing assertions. |
| Licensing posture | AGPL constraints from source project | gg is MIT-licensed and must remain clean-room | already covered | n/a | Existing direction already mandates concept-only borrowing, no code copy; protects commercial/open-source distribution flexibility. |
| Commercialization / open-source posture | Productized distribution framing | gg is open-source-first, alpha-honest, local-only | already covered | n/a | Current posture aligns with transparent capability claims and non-SaaS local architecture; continue evidence-backed messaging. |

## Explicit gap callout: supervisor vs watcher

`TASK-380` (index watch mode) addresses **file-change to index refresh** mechanics only.

`TASK-385` tracks **agent-triggering/supervisor behavior** (e.g., reliable follow-up activation, coordination semantics, and visibility). These are distinct problems and should not be merged into one implementation track.

## Notes

- This matrix is a planning/evaluation artifact; it does not change runtime behavior by itself.
- All adopt/adapt rows are linked to gg tasks as required by TASK-384 ACs.
