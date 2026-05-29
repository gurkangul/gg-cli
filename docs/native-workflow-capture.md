# Native Workflow Capture Points

Audience: agents and maintainers who use gg alongside other agent tools. After
reading this, an agent should know when to mirror durable outputs into gg without
changing its own planning or execution style.

Core rule: do not standardize agent behavior. Standardize durable memory
capture.

This is lightweight protocol guidance. It is not an adapter framework, runtime
integration, orchestration layer, mode system, daemon, RPC surface, MCP bridge,
or background sync design.

Agent-native artifacts may stay in their native tools. Mirror concise summaries
and references into gg when future agents need to retrieve the outcome.

Use existing verbs only:

- `gg search` / `gg context` before changing important behavior.
- `gg impact` before source edits where blast radius matters.
- `gg record` for durable decisions and rejected approaches.
- `gg task` for durable project work, story outputs, and follow-ups.
- `gg bug` for bugs, root causes, repros, and fix evidence.
- `gg tell` for blockers, handoffs, and concise evidence or artifact references.

Durable outputs include decisions, rejected approaches, task/story outputs, bugs
and root causes, test/diff/evidence summaries, important artifact references,
blockers, and handoffs.

Minimal evidence packet for handoff/review:

- Commands run: `<command> → <exit/result>`
- Live smoke: `<what was exercised> → <result or not applicable>`
- Impacted files: `<files changed>; impact checked with <gg impact commands>`
- Known gaps: `<none or explicit gap>`
- Artifacts: `<paths/references only; keep bulky artifacts outside gg>`

Use this packet in existing verbs such as `gg tell --task`,
`gg task ready-for-live --plan`, `gg bug fix`, or `gg record`. Do not create a
second tracker just to store evidence.

## BMAD

Native strengths:
- Persona-led product, architecture, UX, QA, and story discussions.
- Party-mode and elicitation rounds that expose tradeoffs and rejected options.

Native artifacts:
- PRDs, architecture notes, UX specs, story files, review findings, transcripts,
  and subagent summaries.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A BMAD round produces an accepted decision, rejected approach, durable story,
  follow-up task, blocker, or artifact reference.
- The host agent finishes a party-mode or specialist round and extracts durable
  outputs from agents that cannot call gg themselves.

Use gg:
- `gg record` for accepted decisions and rejected approaches.
- `gg task` for durable stories, follow-ups, or implementation work.
- `gg bug` for bugs and root causes found during QA, review, or implementation
  planning rounds.
- `gg tell` for blockers, handoffs, evidence summaries, and artifact references.
- `gg search` / `gg context` before revisiting a product or architecture topic.

Do not force:
- BMAD personas to call gg directly from isolated prompts.
- Every brainstorming note or transcript line into gg.
- BMAD story structure to become the universal gg task structure.

Example:

```sh
gg search "auth architecture" --compact
# BMAD architecture round runs normally
gg record "Use session cookies for admin auth" --reason "BMAD round accepted lower operational risk" --tags "bmad,auth"
gg record "JWT-only admin auth" --decision-status rejected --reason "Rejected in BMAD round: revocation and support burden" --tags "bmad,auth"
gg task create "Implement admin session cookie auth" --detail "Story accepted by BMAD round; see docs/story reference" --priority high --tags "bmad,auth"
gg tell implementer "Admin auth story ready; durable decisions captured in gg" --from architect --audience agents
```

## GSD2

Native strengths:
- Structured context, specs, milestones, slices, task plans, summaries, and UAT
  notes for local execution.
- Good scratchpad discipline for deep implementation work.

Native artifacts:
- `.gsd` context files, plans, summaries, validation notes, UAT notes, local
  knowledge, and GSD database state.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- GSD output becomes shared project knowledge rather than local scratchpad state.
- A GSD decision, rejected path, task, bug, root cause, evidence summary,
  blocker, or handoff must be visible to non-GSD agents.

Use gg:
- `gg record` for durable GSD decisions and rejected approaches.
- `gg task` for work items that future agents need outside GSD.
- `gg bug` for defects and root causes found during GSD execution.
- `gg tell` for GSD handoffs, blockers, and evidence summaries.
- `gg search` / `gg context` before GSD planning changes project direction.

Do not force:
- `.gsd/gsd.db` to be treated as shared canonical memory.
- gg to own GSD runtime/session coordination or local planning state.
- A ban on manual GSD use. GSD may stay a native scratchpad; gg is canonical
  only for shared durable memory.

Example:

```sh
gg context "index freshness" --compact
# GSD2 produces local context and task notes normally
gg record "CodeGraph freshness is one-shot repair plus explicit watcher" --reason "GSD2 planning confirmed no background index daemon" --tags "gsd,index"
gg task create "Document CodeGraph freshness UX" --detail "Durable GSD2 follow-up; local slice notes remain in .gsd" --priority medium --tags "gsd,index"
gg tell reviewer "GSD2 notes mirrored. Evidence: commands run: go test ./... -count=1; live smoke: not applicable; impacted files: docs/native-workflow-capture.md (gg impact checked); known gaps: none; artifacts: .gsd/summaries/TASK-123.md" --from implementer --audience agents
```

## OMO Slim

Native strengths:
- Specialist-agent execution, compact focused roles, and fast review/refactor/test
  loops.
- Parallel analysis where each specialist contributes a narrow conclusion.

Native artifacts:
- Specialist reports, punch lists, patch summaries, review findings, test output,
  and handoff notes.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A specialist result becomes a durable decision, rejection, task, bug finding,
  blocker, or handoff.
- OMO Slim produces evidence that another agent should trust without rerunning the
  entire round.

Use gg:
- `gg record` for accepted specialist conclusions and rejected approaches.
- `gg task` for durable punch-list items or story outputs.
- `gg bug` for reproducible defects and root-cause findings.
- `gg tell` for cross-role handoffs and concise evidence summaries.
- `gg search` / `gg context` before specialists revisit a debated topic.

Do not force:
- OMO Slim to expose internal specialist prompts or every intermediate thought.
- OMO Slim global configuration changes into project tasks unless they affect the
  project itself.
- One OMO specialist's local scratchpad to become canonical memory.

Example:

```sh
gg search "review convergence" --compact
# OMO Slim specialists review the change normally
gg record "Require reviewer-visible evidence summary before task handoff" --reason "OMO Slim review found future agents need compact proof, not raw logs" --tags "omo,evidence"
gg task create "Add reviewer evidence summary to handoff docs" --detail "Durable OMO Slim punch-list item" --priority medium --tags "omo,evidence"
gg tell reviewer "OMO Slim review complete; durable findings captured in gg" --from implementer --audience agents
```

## Antigravity

Native strengths:
- Planning, browser-driven verification, artifact inspection, and visual/live
  evidence gathering.
- Good fit for workflows that need screenshots, traces, or UI proof.

Native artifacts:
- Plans, implementation summaries, browser traces, screenshots, HAR files,
  verification bundles, and artifact links.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A browser/live check proves behavior, exposes a bug, or changes the plan.
- An artifact reference is important for future review, debugging, or handoff.
- A planning decision or rejected route should not be rediscovered.

Use gg:
- `gg record` for planning decisions and rejected approaches.
- `gg bug` for defects with repro/evidence references.
- `gg task` for durable follow-ups found during verification.
- `gg tell` for blocker/handoff messages with artifact paths or summaries.
- `gg context` / `gg impact` before changing important behavior or source files.

Do not force:
- Raw screenshots, traces, or HAR bodies into gg.
- Antigravity's browser/planning flow to become a gg-controlled runtime.
- Visual verification artifacts to replace concise textual evidence summaries.

Example:

```sh
gg context "onboarding flow" --compact
# Antigravity plans and verifies in browser normally
gg bug report "Onboarding submit fails after invalid email" --detail "Browser verification found retry dead end; artifact: .artifacts/onboarding-retry-trace.zip"
gg tell implementer "Onboarding retry bug reproduced; trace path captured in bug detail" --from reviewer --audience agents
```

## Codex

Native strengths:
- Terminal-first coding, patch generation, refactors, and command-driven test
  loops.
- Good fit for focused implementation tasks with clear diffs.

Native artifacts:
- Diffs, patches, command output, test logs, local notes, and commit summaries.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A code change encodes a decision or rejected approach.
- A bug root cause, repro result, test summary, or handoff needs to survive the
  Codex session.
- A new durable work item emerges from implementation.

Use gg:
- `gg search` / `gg context` before changing important behavior.
- `gg impact` before source edits where blast radius matters.
- `gg record`, `gg task`, `gg bug`, and `gg tell` for durable outputs.

Do not force:
- Codex to use gg as its planner or todo list for private coding steps.
- Every failed test iteration into gg.
- A local diff or commit message to be the only durable memory.

Example:

```sh
export GG_AGENT=codex-1 GG_ROLE=implementer
gg search "task ownership lifecycle" --compact
gg impact path/to/changed-file.go --compact
# Codex edits and tests normally
gg tell reviewer "TASK-123 ready. Evidence: commands run: go test ./internal/tasks; live smoke: not applicable; impacted files: cmd/task_status.go (gg impact checked); known gaps: none; artifacts: .artifacts/TASK-123-test.txt" --from "$GG_ROLE" --task TASK-123
```

## Claude Code

Native strengths:
- Repository navigation, tool orchestration, multi-file edits, subagents, skills,
  and structured review/implementation loops.

Native artifacts:
- Plans, todo lists, diffs, tool transcripts, subagent outputs, test logs, review
  notes, and handoff summaries.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A user-approved decision, rejected approach, subagent result, bug/root cause,
  evidence summary, or handoff should be visible outside the Claude Code chat.
- A Claude subagent produces durable findings that it cannot persist itself.

Use gg:
- `gg record` for decisions and rejected approaches.
- `gg task` for durable follow-ups or story outputs.
- `gg bug` for bugs, root causes, repros, and fix evidence.
- `gg tell` for blockers, handoffs, and artifact references.
- `gg search` / `gg context` before contradicting existing project memory.

Do not force:
- Claude Code todo lists or every tool transcript into gg.
- Subagents to self-persist if their prompt lacks project rules.
- Claude Code to self-certify completion when the project requires reviewer
  separation.

Example:

```sh
gg context --compact
# Claude Code uses skills/subagents normally
gg record "Use narrow one-method interfaces for store test seams" --reason "Subagent review confirmed it matches existing project pattern" --tags "claude,testing"
gg tell reviewer "TASK-123 ready. Evidence: commands run: go test ./internal/store; live smoke: not applicable; impacted files: internal/store/tasks.go (gg impact checked); known gaps: none; artifacts: subagent-review-summary.md" --from implementer --task TASK-123
```

## Cursor

Native strengths:
- IDE-aware edits, inline refactors, diagnostics, UI/component work, and quick
  iteration with surrounding code context.

Native artifacts:
- Composer/chat plans, changed files, diagnostics, test output, local previews,
  and patch summaries.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- Cursor chat settles a durable decision or rejects an approach.
- A durable follow-up, bug, root cause, evidence summary, blocker, or handoff
  emerges from the IDE session.
- The same knowledge would otherwise be trapped in a local Cursor conversation.

Use gg:
- `gg search` / `gg context` before changing important behavior.
- `gg impact` before source edits where blast radius matters.
- `gg record`, `gg task`, `gg bug`, and `gg tell` for durable outputs.

Do not force:
- Every inline suggestion, diagnostic, or chat note into gg.
- Cursor rules to prescribe a universal planning ceremony.
- UI preview artifacts into gg beyond concise summaries and paths.

Example:

```sh
export GG_AGENT=cursor-1 GG_ROLE=implementer
gg search "settings panel accessibility" --compact
# Cursor edits and previews normally
gg tell reviewer "Settings panel ready. Evidence: commands run: npm test -- settings-panel; live smoke: keyboard nav preview passed; impacted files: src/settings/panel.tsx (gg impact checked); known gaps: none; artifacts: .artifacts/settings-panel.png" --from "$GG_ROLE" --task TASK-123
```

## Aider

Native strengths:
- Tight edit-test-commit loops, small patch sets, and git-oriented coding.
- Good fit for focused changes where the diff is the primary working artifact.

Native artifacts:
- Git diffs, commits, chat transcripts, test commands, and patch summaries.

Capture into gg when:
- Mirror decisions, rejected approaches, durable tasks/story outputs, bugs/root
  causes, evidence summaries/artifact references, blockers, and handoffs when
  future agents need them.
- A commit or diff embodies a durable decision, rejection, bug fix, evidence
  summary, blocker, or handoff.
- Aider discovers follow-up work that another agent may pick up later.

Use gg:
- `gg search` / `gg context` before important changes.
- `gg impact` before source edits where blast radius matters.
- `gg record` for durable design decisions and rejected approaches.
- `gg task` for follow-ups or story outputs.
- `gg bug` and `gg tell` for root causes, evidence, blockers, and handoffs.

Do not force:
- Aider's whole chat transcript into gg.
- Every micro-commit to become a gg task.
- Git history to be the only place future agents learn why the change happened.

Example:

```sh
gg search "brain import scanner limit" --compact
gg impact path/to/changed-file.go --compact
# Aider edits and commits normally
gg record "Cap Scanner initial buffer to the max line limit" --reason "Aider fix confirmed bufio.Scanner.Buffer uses max(max, cap(buf))" --tags "aider,brain-import"
gg tell reviewer "Brain import scanner fix committed. Evidence: commands run: go test ./internal/store; live smoke: not applicable; impacted files: path/to/changed-file.go (gg impact checked); known gaps: none; artifacts: commit diff" --from implementer --audience agents
```
