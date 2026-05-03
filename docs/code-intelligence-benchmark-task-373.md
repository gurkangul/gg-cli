# Code Intelligence Benchmark — gg vs rg (TASK-373)

Status: draft benchmark protocol + reproducible runner/report template  
Owner role: developer  
Task link: TASK-373

## Purpose

Measure how well `gg` code-intelligence commands (`gg search`, `gg context`, `gg impact`) support realistic agent questions compared with raw text search (`rg`) under a repeatable, local methodology.

This artifact avoids product claims. It only defines and records measurable outputs.

## Scope

- Target repositories:
  1. **Primary fixture (this repo):** `gg-cli`
  2. **Larger fixture:** any local repo with notably larger file count/history than `gg-cli` (see `Fixture setup`)
- Minimum question set: **6 questions** (>=5 required), each answerable in both tool tracks
- Tool tracks:
  - `gg-*` track (semantic/knowledge-assisted)
  - `rg` track (text-only baseline)

## Questions (Q1–Q6)

Use the same questions and grading criteria for every run.

1. **Q1 — Prior rejected approach for a topic**  
   Prompt: “What approaches to `<topic>` were rejected and why?”
2. **Q2 — Existing decision constraints**  
   Prompt: “What decisions constrain `<subsystem>` changes?”
3. **Q3 — Blast radius for editing a file**  
   Prompt: “If I change `<file>`, what depends on it?”
4. **Q4 — Work context for a task/bug**  
   Prompt: “What prior tasks/bugs/notes are relevant to `<task-or-bug>`?”
5. **Q5 — Locate protocol/gate requirements**  
   Prompt: “What mandatory workflow rules apply before closing work?”
6. **Q6 — Large fixture impact/context retrieval**  
   Prompt: same as Q3 or Q4 on the larger fixture

> Replace placeholders (`<topic>`, `<subsystem>`, etc.) with concrete values per fixture and record them in the run log.

## Metrics to capture

For each question and each track (`gg-*`, `rg`):

- **elapsed_ms**: wall-clock runtime in milliseconds
- **command_count**: number of commands required to produce final answer
- **bytes_emitted**: stdout bytes returned by all commands used for that answer
- **compact_savings_bytes** (gg-only, when relevant):  
  `non_compact_bytes - compact_bytes` for equivalent query form
- **answer_sufficiency_score**: rubric score (0–3) described below

### Answer-sufficiency rubric (0–3)

- **0 — insufficient:** does not answer question or misses critical evidence
- **1 — partial:** some relevant hits, but key context/evidence absent
- **2 — sufficient:** answers question with usable evidence and low ambiguity
- **3 — strong:** sufficient + directly actionable, minimal follow-up needed

Optional tie-break notes:
- “required manual synthesis” (yes/no)
- “false-positive burden” (low/medium/high)

## Methodology

1. Run in a clean local shell with fixed env labels:
   - `GG_AGENT=codex`
   - `GG_ROLE=developer`
2. Use the same machine, no network dependency, no concurrent heavy jobs.
3. For each question:
   - Run `gg-*` track first (compact variants where applicable), then `rg` track.
   - Capture stdout byte size and elapsed time for each command.
   - Count commands used to reach final answer.
   - Grade answer sufficiency using rubric above.
4. Repeat full question set on both fixtures.
5. Summarize in one table per fixture + combined totals.

### Compact savings measurement

For any `gg` command that supports `--compact` and is used in the answer:

1. Run compact form once.
2. Run equivalent non-compact form once.
3. Record `compact_savings_bytes = non_compact_bytes - compact_bytes`.

If no compact-capable command is used for a question, set savings to `n/a`.

## Reproduction instructions

### Fixture setup

1. Keep `gg-cli` as the primary fixture.
2. Select a larger local fixture path and export it:

```bash
export LARGE_FIXTURE_DIR="/absolute/path/to/larger/local/repo"
```

Selection rule: larger fixture should have substantially more tracked files than `gg-cli` (record actual file counts in run output).

### Run benchmark script

From `gg-cli` root:

```bash
scripts/benchmark/task-373-run.sh
```

Outputs:

- `docs/benchmark-results/task-373-<timestamp>.md` (human-readable report)
- `docs/benchmark-results/task-373-<timestamp>.json` (raw structured metrics)

## Output schema (JSON)

```json
{
  "run_id": "task-373-YYYYmmdd-HHMMSS",
  "fixtures": [
    {
      "name": "gg-cli",
      "path": "...",
      "question_results": [
        {
          "id": "Q1",
          "question": "...",
          "track": "gg",
          "elapsed_ms": 0,
          "command_count": 0,
          "bytes_emitted": 0,
          "compact_savings_bytes": 0,
          "answer_sufficiency_score": 0,
          "notes": ""
        }
      ]
    }
  ],
  "methodology": "see docs/code-intelligence-benchmark-task-373.md",
  "limitations": ["..."]
}
```

## Limitations (must be reported with every run)

- Results are machine/environment-specific (CPU, disk, shell, local state).
- Sufficiency scoring includes human judgment; rubric reduces but does not remove subjectivity.
- `rg` track quality depends heavily on operator query skill and iterative refinement.
- `gg` track quality depends on current local gg data population (decisions/tasks/messages).
- Command byte counts measure emitted output, not cognitive effort directly.
- Single-run measurements are noisy; repeated runs may be needed for tighter confidence.

## Interpretation guidance

Use benchmark output to discuss tradeoffs, not to assert universal superiority.  
Any claim must point to specific measured rows in the generated report.

## Acceptance checklist mapping

- **AC-1:** question set includes 6 questions and explicitly requires one larger fixture.
- **AC-2:** metrics section defines elapsed time, command count, bytes emitted, compact savings, and sufficiency rubric.
- **AC-3:** this committed artifact + runner script under `scripts/benchmark/` provides reproducibility.
- **AC-4:** methodology and limitations are explicit; report forbids unverifiable claims.
