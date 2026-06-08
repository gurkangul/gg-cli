# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.2] - 2026-06-08

### Changed

- **Graph tab now uses a dagre DAG layout** (left-to-right: decision → task →
  dependency) instead of naive columns, so the relationship web is readable even
  at hundreds of nodes.
- Overview "decisions" card relabeled **active decisions** to make clear it
  counts active records (vs the full ledger incl. superseded/rejected).

## [0.4.1] - 2026-06-08

### Added

- **Graph tab** (react-flow) — visualize the brain's relationship web:
  decision→task (DECIDES), task→task (DEPENDS_ON / BLOCKS), bug→task (AFFECTS).
  Built from the store (`Task.DependsOn/Blocks`, `Decision/Bug.TaskID`), not
  Memgraph — gg-cli's per-project brain edges aren't reliably synced to Memgraph,
  and the store always has the links. Only connected records are shown; the
  37k-symbol code graph is excluded to stay legible. New `/api/graph` endpoint.

## [0.4.0] - 2026-06-08

The dashboard becomes a real React SPA — without giving up the single-binary,
offline, no-daemon architecture.

### Changed

- **`gg serve` now serves a React + Vite + TypeScript + Tailwind dashboard**,
  replacing the hand-rolled vanilla page. The compiled bundle is embedded in the
  Go binary via `go:embed` (committed `dashboard/dist`), so end users still get a
  single binary with no Node runtime, fully offline, served foreground on
  127.0.0.1 only. Node is a build/dev dependency only.
  - Tabs: Overview (+ auto-canon), Live Search (embed→Qdrant pipeline with
    timings + score bars), Decisions, Work (Kanban), Bugs, Files (raw JSONL
    browser), Context (visual telemetry).
  - Dev DX: `cd dashboard && npm run dev` with `/api` proxied to a running
    `gg serve` (hot reload). Production: `npm run build` → embedded `dist`.
  - This unblocks rich visual libraries (react-flow graph, charts, drag-drop
    Kanban) for future iterations without an architecture change.

## [0.3.37] - 2026-06-08

Dashboard v1 complete (still the embedded vanilla build; the React SPA is next).

### Added

- **Kanban board** (`Work` tab) — tasks laid out in lifecycle columns
  (pending / in progress / ready for live / blocked / done).
- **Files tab** — browse the raw JSONL source-of-truth stores (name, record
  count, size) and view the most recent records of any store. Path-traversal
  guarded; new `/api/files` and `/api/file` endpoints.
- **Context & Activity tab** — the telemetry view is now visual: net/compact
  tokens saved, a per-command activity breakdown ("what gg is actually doing"),
  compaction savings by verb, session context-pressure, and the 3-tier context
  model (session-start / for-task / search) so the context economy is legible.

## [0.3.36] - 2026-06-08

A way to *see* the project brain — and how it works.

### Added

- **`gg serve` — local dashboard.** A FOREGROUND, localhost-only web UI for the
  project brain. Not a daemon: it binds 127.0.0.1 only, runs until Ctrl-C, and is
  read-only (consistent with the no-daemon / no-network architecture; same
  precedent as foreground `--watch`). Anyone who ran `gg init` opens it with
  `gg serve` (`--port`, `--no-open` flags). The dashboard is a single embedded
  page with no external assets (works fully offline). Views:
  - **Overview** — counts + the auto-derived canon + recent decisions.
  - **Live Search** — type a question and watch how gg answers it: the query is
    embedded into a 768-dim vector (Ollama), then Qdrant returns nearest records
    by cosine similarity, with embed/search timings and per-result score bars —
    the same path an agent's `gg search` takes.
  - **Decisions / Work / Bugs** — browse the memory (noise-filtered).
  - **Telemetry** — local context-economy and activity stats.

## [0.3.35] - 2026-06-08

Usability polish for the per-project brain — make the current form excellent to
use day to day (scope stays single-project; no cross-project/user brain).

### Changed

- **Leaner session-start canon.** The canon injected at session-start now uses a
  compact view (hard total cap on decisions, shorter lines) with a
  `gg canon show` pointer for full depth. Dropped the per-session briefing from
  ~13.7 KB to ~8.3 KB (~3.4K → ~2.1K tokens) while keeping the full institutional
  briefing. `gg canon show` is unchanged (full depth).
- **Release logs no longer pollute the canon.** Operational decisions
  ("Release vX shipped and synced…") are filtered as low-signal alongside
  bypass-rationale rows, so the canon and overview carry durable knowledge only.

## [0.3.34] - 2026-06-08

Canon goes fully automatic — institutional memory with zero manual upkeep. A new
agent now inherits the distilled senior-dev knowledge at session-start without
anyone ever running `gg canon set`.

### Added

- **Auto-derived canon (`BuildAutoCanon`).** `gg canon show` and session-start
  now compute the canon live from the ledger — no manual curation. Three
  sections, distilled deterministically (gg is no-network/no-LLM, so
  "summarization" = selection + dedup + ranking):
  - **key-decisions** — active decisions, deduplicated, with pinned and
    architecture/constraint-tagged ones always included regardless of age
    (important-old is never summarized away); the routine tail is capped.
  - **rejected-approaches** — what not to re-propose, deduplicated.
  - **failure-modes** — fixed-bug root causes (the lessons).
  Manual `gg canon set` still works and now renders as a "Curated" layer above
  the auto-derived digest.

### Changed

- **Noise no longer dominates a newcomer's first screen.** Low-signal
  bypass-rationale records and near-identical duplicate decisions are filtered
  out of both the canon and the `gg context` project overview
  (`FilterDecisionNoise`). In dogfooding, the overview's top went from four
  identical bypass-rationale rows to the real architectural decisions.

## [0.3.33] - 2026-06-08

Regression-gate repair found while closing the institutional-memory tasks
through the gates (no bypass) — the pre-task-done repro gate was silently broken
for every recent bug.

### Fixed

- **Regression gate ran `_test.go` repros as shell scripts.** `gg bug run-repros`
  executed every registered repro via `sh <path>`. The 19 newer bugs
  (BUG-062..086) register the locking `*_test.go` as their repro, so
  `sh foo_test.go` failed in ~3ms — the `90-bug-repros` pre-task-done hook
  reported 19 false failures and blocked every task close. A `*_test.go` repro
  now runs via `go test -run ^(Test…)$ <dir>` scoped to that file's tests; shell
  repros still run via `sh`. All 82 repros pass.

### Changed

- **Cleared lint debt (17 → 9 golangci issues).** Removed 3 dead functions
  (`impactGraphFreshnessWarnings`, `goInstallGG`, `hasCodeGraphSourceFiles`) and
  rewrote 5 gocritic if-else chains to `switch`/`else if` (impact, index_status,
  task_list, task_create). Pure refactor, no behavior change — restores a green
  `60-lint-gate`.

## [0.3.32] - 2026-06-07

Institutional-memory layer — gg moves from an append-only ledger toward a
self-distilling project memory a new agent can inherit.

### Added

- **`gg canon`** (TASK-468): the agent-distilled "what every dev must know" layer.
  `gg canon gather` dumps the raw material (active decisions, rejections,
  fixed-bug root causes); `gg canon set <area> "…"` stores durable per-area
  knowledge; `gg canon show` prints it; session-start injects it (like RULES) so
  every new agent starts with it. Stored at `.gg/canon.jsonl` (outside the
  export-managed `brain/` dir). Distillation is agent-driven (no-network: no
  cloud LLM).
- **`gg record --pin`** (TASK-469): pinned decisions surface first in the project
  overview regardless of age, so important-but-old decisions are never buried by
  recency. Rendered with 📌.
- **`gg inbox archive`** (TASK-470): retire stale `audience=agents` status
  broadcasts from the inbox (kept in JSONL, forward-only) so it stops bloating.
- **`gg doctor --install-index-hooks`** (TASK-471): opt-in pre-push + post-merge
  git hooks that run `gg index --changed` to keep the local CodeGraph fresh.
  Foreground + non-blocking, not a daemon.

### Changed

- session-start bypass audit collapsed to a one-line per-gate summary; project
  orientation no longer surfaces test/scrubber fixture notes.

## [0.3.31] - 2026-06-07

Found during a full end-to-end command QA sweep.

### Fixed

- **Task lifecycle was unsatisfiable for agents** (BUG-050 regression): under
  `GG_AGENT` the inbox/`task get` auto-compacts, and the BUG-074 fix stopped
  compact reads from recording a hydration proof — so `ready-for-live` / `done` /
  `block` always refused with "no recent full task detail read" and there was no
  in-flow override. Added `gg task get TASK-X --full`, which forces a full
  (non-compact) render even under agent auto-compact and records the hydration
  proof; the gate error messages now point at it. Bugs were unaffected
  (`gg bug triage` records hydration unconditionally).
- **Decision evidence was stored but never shown** (BUG-086, completes BUG-071):
  `gg record --evidence` persisted evidence but no renderer displayed it. The
  full context render now shows `Evidence: …`, and marks decisions with no
  evidence as `[unverified]` (evidence was already present in `--json`).

### Notes

- v0.3.30's critical search fix (BUG-085) is included here; its release publish
  was blocked by a transient GitHub 504.

## [0.3.30] - 2026-06-07

### Fixed

- **Critical: semantic search returned zero results** (BUG-085). The
  non-degraded-vector filter used Qdrant `is_null` on `gg_vector_degraded` as a
  MUST condition across every `Search*` query, but normal records never set that
  key and `is_null` matches only keys that exist and are explicitly null — so the
  filter excluded the entire brain and `gg search` / `gg context` returned
  nothing. Switched to `is_empty` (matches missing/null/empty), which keeps
  normal records and excludes only explicitly-degraded ones. Regression covered
  by TestSearchExcludesOnlyDegraded_Integration. Affected v0.3.27–v0.3.29.

## [0.3.29] - 2026-06-07

### Changed

- Document the v0.3.27 capabilities in the agent-facing docs so agents adopt them:
  `gg record --evidence` in the durable-memory contract and `AGENTS.md`, plus
  per-session identity auto-derivation and per-recipient inbox notes in the
  orientation guidance. Propagates to registered projects via `gg system sync`.

## [0.3.28] - 2026-06-07

Ships local fixes that were committed in parallel and merged with the v0.3.27
memory-integrity cluster.

### Fixed

- Active-status filter on `SearchDecisions`/`SearchBugs` with a status badge in
  renderers (BUG-064).
- Strict mode fails closed on non-executable hooks (BUG-065).
- Exclude degraded zero-vector records from all `Search*` queries (BUG-066).
- `gg record --rejects` supersedes the rejected decision in the store (BUG-068).
- Inbox gate preflight added to `gg task done` (BUG-072).
- Restored the permanent repro for BUG-061 (BUG-081).

## [0.3.27] - 2026-06-06

Memory-integrity bug cluster (17 bugs, PR #1) — restores the "one consistent
shared brain" guarantee: every agent reads the same durable, current memory.

### Fixed

- **Durable-memory mutations are JSONL-first with version/CAS** (BUG-062, BUG-063):
  decision/bug/message status updates were Qdrant-only with no concurrency guard,
  so they silently reverted on any Qdrant rebuild and concurrent writers clobbered
  each other. Mutations now append the full record to JSONL under an optimistic
  version guard (last-write-wins fold), then mirror to Qdrant best-effort.
- **reembed sources from JSONL** (BUG-069): no longer drops JSONL-only records or
  prefers stale Qdrant payloads.
- **reconcile** folds latest state into Qdrant, surfaces malformed JSONL lines
  (BUG-070), and holds a non-blocking lock so two reconcilers can't clobber a
  store (BUG-073).
- **Per-recipient inbox read-state** (BUG-082): one agent reading no longer marks
  a message read for everyone.
- **Claude inbox-first hook** actually injects now (BUG-083): grep matches the
  real (compact) header and only filters by role when `GG_ROLE` is set.
- **Per-session agent identity** (BUG-084): a generic `GG_AGENT=claude-code`
  under a Claude session derives a unique `claude-code-<session>` so concurrent
  tabs don't collapse ownership/verifier separation.
- **Identity-based verifier separation** (BUG-067): closure is refused when the
  closing runtime is the one that set ready-for-live (role strings were spoofable).
- **Embedding dimension guard** (BUG-078): refuse to persist `Dim:0` and self-heal
  a corrupt zero-dim meta instead of silently disabling the mismatch check.
- **Hook-level gate disable is audited** (BUG-079): `GG_AC_ATTESTATION=off` /
  `GG_REVIEW_CONVERGENCE=off` now require a rationale and write a searchable
  brain event.
- **Review-convergence trailer** must enumerate >=3 matrix categories, not a bare
  token (BUG-077).
- **Hydration gate** is satisfied only by a full (non-compact) read (BUG-074).
- **`gg context` offline** does a live JSONL scan before the stale LKG cache
  (BUG-075) and reports per-collection query failures in `--json` (BUG-076).
- Hardening (BUG-080): numeric ID export sort, atomic session-cursor write,
  documented lock-bypassing `projectstate.Write`, and a JSONL bootstrap for the
  discussion sequence.

### Added

- `gg record --evidence` and a Decision `Evidence` field; `Note` now records an
  author — provenance so an unverified claim and a proven one are not stored with
  identical weight (BUG-071).
- `internal/identity` package resolving the effective per-runtime agent identity.

## [0.3.26] - 2026-05-30

### Fixed

- Clarify `GG_AGENT` examples so GSD shells use a `gsd-*` runtime identity instead of copying a host-agent example literally.

## [0.3.25] - 2026-05-30

### Added

- Document native workflow capture points for BMAD, GSD2, OMO Slim, Antigravity, Codex, Claude Code, Cursor, and Aider.
- Add native-workflow memory-sync smoke coverage for decisions, rejections, and handoff retrieval.

### Changed

- Reframe agent protocol, templates, and gate wording around durable shared memory and evidence capture instead of gg-owned workflow orchestration.

### Fixed

- Remove retired orchestration wording from active docs and tests while preserving stale-wording absence coverage.

## [0.3.24] - 2026-05-28

### Added

- Add Swift CodeGraph recognition and externally-backed SCIP ingestion: `gg index --lang swift`, SwiftPM/Xcode/.swift freshness detection, explicit `scip-swift` preflight errors, and Swift setup documentation.

## [0.3.23] - 2026-05-26

### Added

- `gg context --compact` without a topic now emits a compact project-level onboarding bundle for fresh agent sessions.
- `gg task ready-for-live` can update the stored verifier plan on already-ready tasks, including via `--plan`.

### Changed

- CodeGraph freshness now explicitly excludes dependency lockfile-only churn such as `go.sum`, npm/yarn/pnpm lockfiles, and Python lockfiles.
- Reviewer packets and reconciliation output now surface clearer task lifecycle drift details.

### Fixed

- Impact attestation now checks the active task diff first and only falls back to HEAD trailers when HEAD references the exact task being closed.

## [0.3.22] - 2026-05-24

### Fixed

- `gg system sync` now self-heals missing per-project tracker collections before refreshing contracts/hooks, while preserving non-destructive stale registry reporting and cancellation semantics.
- `gg system brain status` now describes its role separately from the tracker self-heal performed by `gg system sync`.

## [0.3.21] - 2026-05-22

### Added

- Standardize CodeGraph freshness notices across session start, next-step, impact, doctor, and index status outputs.

### Fixed

- Make foreground CodeGraph watchers use per-language freshness state and dirty-tree fingerprints to avoid stale slices and repeated full-refresh loops.

## [0.3.20] - 2026-05-22

### Fixed

- Regenerate CLI docs with deterministic trailing newlines so the docs drift CI gate stays clean.
- Add focused task-claim helper coverage to keep `internal/store` above the required coverage threshold.

## [0.3.19] - 2026-05-22

### Added

- CodeGraph freshness notices now surface changed/new/deleted/module-file counts across `gg index status`, `gg doctor`, `gg impact`, `gg session-start`, and `gg next`.
- Added a time-to-productivity onboarding smoke script covering session start, next-step recommendation, search/context retrieval, impact, and ready-task listing.

### Fixed

- `gg doctor --fix-index` now repopulates empty or unavailable Memgraph projections with a full index instead of no-oping through `--changed`, and treats Qdrant downtime as advisory for CodeGraph repair.
- TypeScript/Python `src/` fallback discovery no longer overrides nested manifest-based module roots.
- Inbox-obedience auditing now keeps assignment bypass handling aligned with focused user-directed release work.

## [0.3.18] - 2026-05-19

### Fixed

- Clear the remaining `golangci-lint` debt surfaced by gg task quality hooks.

## [0.3.17] - 2026-05-19

### Fixed

- `gg update` now installs the concrete latest version selected by `gg update check`, and uses `GOPROXY=direct` when direct lookup beats a stale Go proxy result.

## [0.3.16] - 2026-05-19

### Documentation

- Trim README into a shorter public-facing overview and point detailed command/config material to docs.
- Clarify agent-status broadcasts should use `--audience agents` in runtime/template guidance so status noise stays out of human inboxes.
- Refresh deprecated decision-capture help text to point users at canonical `gg record` forms.

## [0.3.15] - 2026-05-19

### Added

- `gg index status` and `gg system brain status` now treat projects with no supported CodeGraph source as `not_applicable`, so non-code projects no longer block cross-project brain health.

### Fixed

- `gg doctor` now warns when the installed `gg` binary cannot be proven fresh because the source checkout has uncommitted build-affecting changes.
- `gg bug fix/start/wontfix/reopen/attach-repro` now require a recent full `gg bug get` or `gg bug triage` hydration proof in tagged agent sessions.
- `gg audit inbox-obedience` no longer treats `gg tell all` broadcasts as per-role acknowledgements unless a role is explicitly mentioned.
- TypeScript CodeGraph indexing now preserves other language graph slices, records per-language freshness, and can index nested package roots when a workspace root lacks a TypeScript config.

## [0.3.14] - 2026-05-18

### Added

- `gg system brain status` reports cross-project project ID, backend, brain snapshot drift/freshness, and CodeGraph health separately from `gg system sync` contract propagation.

### Fixed

- `gg task done` no longer panics when a valid compact-hydration proof returns a typed-nil gate result.

## [0.3.13] - 2026-05-18

### Fixed

- Release builds now cross-compile Windows binaries by moving project runtime state locking behind platform-specific files.

## [0.3.12] - 2026-05-18

### Fixed

- Release workflow now publishes GoReleaser-built binary archives and checksums to GitHub Releases instead of creating metadata-only releases.

## [0.3.11] - 2026-05-18

### Added

- `gg doctor` now checks code-graph freshness, degraded placeholder vectors, embedding vector validity, and a semantic canary before reporting the project brain as healthy.
- `gg index --changed`, `gg index status`, and `gg impact` now account for dirty tracked files and untracked source files via working-tree fingerprints, preventing stale impact answers after local edits.
- `gg task ready-for-live` and `gg task block` now share the compact-hydration gate used by `gg task done`, so tagged agent sessions must hydrate the full task before changing task state.

### Fixed

- `gg update` now verifies the installed `gg` binary version after update attempts, avoiding false "latest" reports caused by Go proxy or PATH skew.
- Offline JSONL search now renders tasks and bugs as native result kinds instead of coercing them into decisions.
- `gg doctor --reconcile` marks zero-vector Qdrant payload replays as degraded and tells users to run `gg reembed` to restore semantic recall.
- Runtime/config state writes now use cross-process locking on Unix to reduce concurrent agent clobbering.

## [0.3.10] - 2026-05-16

### Added

- `gg task done` now enforces a compact-hydration gate for tagged agent sessions: agents must run a recent full `gg task get TASK-ID` before closing the task, preventing compact list/search rows from being treated as source-of-truth.
- Full task hydration proofs are stored in project runtime state with locked updates so concurrent Hermes agents cannot clobber another session's proof, bypass audit entries, or session-start version state.

## [0.3.7] - 2026-05-15

### Fixed

- `gg status` now renders compact hydration risk even when compact calls have zero full re-fetches, so zero-hydration agent sessions show the source-of-truth warning instead of hiding the Hydration line.
- File-size scans now skip dependency/framework/runtime trees such as Hermes mounts and generated caches while preserving source paths like `internal/cache`, keeping `gg status` responsive in mounted agent workspaces.

## [0.3.6] - 2026-05-13

### Added

- Compact output now marks hidden record bodies inline (`[reason]`, `[tags]`, `[detail]`, `[resolved]`) so agents can see when a full hydrate is needed without expanding the compact row.
- Agent compact output now ends with `! compact: reasons/details omitted; hydrate before action` to reinforce that compact is an index, not the source record.
- `gg status` now warns when hydration/re-fetch rates are low enough that compact may be used as source-of-truth.

## [0.3.5] - 2026-05-12

### Fixed

- `gg session-start` now waits for the bounded brain auto-backup export before exiting, so project brain snapshots are reliably refreshed for other agents and projects instead of being abandoned by a fire-and-forget goroutine.

## [0.3.4] - 2026-05-12

### Added

**Compact system overhaul — agent auto-compact + 4 new surfaces**

- `isCompactActive(cmd)` — unified resolution: explicit `--compact` flag > `GG_COMPACT` env > agent origin (`GG_ROLE`/`GG_AGENT`/`--from`) with `--compact` flag registered > off. Agents skip the flag and get compact by default; humans stay on rich output; the flag always wins for explicit opt-out.
- `--compact` now available on: `gg inbox`, `gg task list`, `gg bug list`, `gg context --for-task` (previously only on `search`/`context`/`impact (file)`/`task get`).
- Shared line builders in `cmd/compact.go` — 7 duplicated render sites collapsed; format changes land in one place. `renderer_v:1` stamp in telemetry entries so aggregates across format changes can be bucketed.
- New `compact_tokens_saved` metric in `gg status` and `gg telemetry summary` (bytes/4 heuristic). Output: `Compact  74 calls, 208.5 KB / ~53K tok saved (avg 59% reduction)`.

### Fixed

- `compactTrim(s, n<=1)` no longer panics on `runes[:-1]` (latent bug — no caller passed 0 before, but the guard prevents future regressions).
- `gg impact TASK-X --compact` and `gg impact BUG-X --compact` now actually compress output. Previously both appended a 1-line summary after the default render, *increasing* bytes and skipping telemetry — the flag was dead-code.
- `gg impact BUG-X` default (non-compact) output now renders Related Decisions / Tasks / Rejections sections. They were fetched into the result struct but dropped from text mode (JSON output was unaffected).
- `gg audit decide-gaps --compact` now records telemetry via `emitCompact` (was silently emitting without a telemetry entry).
- `gg task get --compact --with-context` baseline measurement now includes the context block on both paths, so the savings percentage reported in `gg status` is honest instead of comparing compact-without-ctx against default-with-ctx.

**`gg watch` — real-time inbox and event stream**

- `gg watch` tails the project's telemetry JSONL and polls the inbox simultaneously, emitting new entries as they arrive. Designed for terminal status bars, desktop notification scripts, and agent-side monitoring loops.
- Flags: `--role` (filter by recipient), `--event` (filter by telemetry event type), `--tag`, `--since` (ISO timestamp or relative duration), `--format ndjson|pretty`, `--no-inbox`, `--no-telemetry`.
- stdout-pipeable: any tool that reads a line-delimited stream works without extra wiring.

**`gg brain backfill` — migrate implicit Task↔Decision links to Memgraph edges**

- Scans Qdrant for implicit Task↔Decision relationships and writes them as explicit `(Decision)-[:DECIDES]->(Task)` edges in Memgraph so `gg impact` and decision-traversal queries work on older projects that predate the edge model.
- Two sources: (1) `Decision.task_id` direct field (always migrated — unambiguous); (2) tag overlap where exactly one decision and one task share a tag (ambiguous multi-matches reported and skipped).
- Dry-run by default — pass `--apply` to execute. Idempotent `MERGE` with `created_by=backfill_v1` tag for rollback. Post-migration audit prints counts.

**`gg gsd audit` — GSD ↔ gg mirror drift detection**

- Compares `.gsd/gsd.db` task state against gg tasks whose titles contain `[GSD:<milestone>-<slice>-<task>]`, reporting tasks present in GSD but missing from gg. Exits non-zero on drift so CI can gate on mirror integrity.

**Task lifecycle auto-broadcast**

- `gg task create`, `gg task done`, and `gg task block` now broadcast a short summary to `all` automatically when `GG_AGENT` or `GG_ROLE` is set and `GG_NO_AUTO_NOTIFY` is unset. Parallel sessions see task state changes without manual `gg tell` calls.
- `GG_NO_AUTO_NOTIFY=1` suppresses the broadcast (same escape valve as the verify-gate notify).

**`gg tell` `@role` mention syntax + multi-target comma fanout**

- `@role` mentions in message bodies are auto-routed to the named recipient's inbox in addition to the declared target. Inbox renders `<<@role>>` so mentions are visually distinct.
- Comma-separated targets in the first positional argument (`gg tell "developer,qa" "..."`) fan the same message out to multiple recipients atomically.

**Claude Code PreToolUse guard**

- `gg gsd-guard` (hidden, invoked by a `PreToolUse` hook) reads the tool-call JSON from stdin and blocks `gsd_plan_*` MCP calls when `tracker.canonical: gg` is set in `.gg/config.yaml`, redirecting agents to `gg task create`.
- Installed automatically by `gg doctor --install-agent-hooks`.

**`gg init` AGENTS.md tracker governance + `gg doctor --install-agents-md`**

- The `gg init` AGENTS.md template includes a `## Tracker Rules` section naming gg as canonical and listing the forbidden MCP calls.
- `gg doctor --install-agents-md` injects the managed block into an existing project's AGENTS.md (idempotent).

**UserPromptSubmit inbox drift-detection hook**

- `gg doctor --install-agent-hooks` now writes a `UserPromptSubmit` hook that surfaces unread messages as agent context on every prompt via `gg inbox --peek`.

### Removed

- **`gg discuss`** — removed after deprecation window (TODO(v0.2) marker). Discussion tracking is handled by `gg record` (decisions) and `gg task create` (action items). 0 calls in dogfood.
- **`gg note`** — removed after deprecation window (TODO(v0.2) marker). Use `gg record` for decisions or commit messages for ambient context. 1 call in dogfood.

## [0.2.0] - 2026-04-18

### Added

**Pre-done verify gate — turn `gg` from a notebook into a checkpoint**

- `gg task done` now runs `.gg/hooks/pre-task-done.d/*.sh` **before** writing the new task state. Any non-zero exit aborts the transition with exit code `7` (`ExitVerifyFailed`) and the task stays in its current state.
- Pre-hooks are always strict by design. `hooks.strict` in `.gg/config.yaml` continues to govern only the post-done `task-done.d` hooks (advisory).
- Hook env contract — shared with future gates like `pre-review-approve.d`: `GG_TASK_ID`, `GG_TASK_SUMMARY`, `GG_PROJECT_ID`, `GG_ACTOR` (`GG_ROLE` or `GG_AGENT`).

**Cross-agent smart rejection signals**

- On rejection, stderr emits a single NDJSON event line with stable keys so any agent (Claude Code, Codex, Cursor, Aider, CI) can program against it without scraping text:
  `{"event":"verify_failed","task":"TASK-042","hook":"10-build.sh","exit":1,"ts":"<rfc3339>","detail":"<tail>"}`
- Internal `gg tell` from `verify-gate` to `all` fires best-effort so parallel sessions see the collision in their next `gg inbox` / `gg status` — no per-agent plumbing needed.
- `GG_NO_AUTO_NOTIFY=1` suppresses the broadcast only. Exit code and NDJSON event still fire. Used by CI, reentrant hook scripts, and tests.
- A store-down failure during the notify step is silently swallowed so it can never mask the underlying verify failure.

**`gg doctor --install-task-hooks` — one-command gate setup**

- Walks the project tree up to depth 3 and installs a pre/post hook pair for every `go.mod` and `package.json` it finds. Monorepos (`lift-cli/go.mod`, `packages/api/package.json`) are first-class.
- Skips `.git`, `.gg`, `.gsd`, `node_modules`, `vendor`, `dist`, `build`, `_bmad`, `_bmad-output` so phantom gates from vendored deps never land.
- Installs disambiguated filenames (`10-go-verify-lift-cli.sh`, `10-go-verify-packages-api.sh`) that each `cd` into their manifest directory before running checks via a substituted `__GG_SUBDIR__` placeholder.
- Node template auto-detects the package manager from lockfiles (`bun.lockb` → `pnpm-lock.yaml` → `yarn.lock` → `npm`) and only runs `typecheck` / `build` / `test` when defined in `package.json`.
- Idempotent — existing files are preserved so user edits survive a reinstall.

**Help text and docs**

- `gg task done --help` now documents the verify gate, the exit-7 contract, the NDJSON envelope, the auto-broadcast, and the installer bootstrap path.
- `docs/cli/` reference regenerated against the current command surface: 15 missing command files added (`gg brain *`, `gg wave *`, `gg telemetry`, `gg task review`, `gg status render`, `gg session-start`) and 26 existing files updated with new flags.

### Fixed

- `TestRecord_OriginHuman` in `internal/telemetry` now isolates `GG_AGENT` as well as `GG_ROLE`, so running the suite inside a standard agent session (`GG_AGENT=claude-code`) no longer produces a false positive failure.

### Changed

- Verify-gate internals renamed for gate symmetry ahead of future gates (`pre-review-approve.d`, …). `verifyRejection` kept as a type alias and `sendVerifyFailure` kept as a one-line wrapper for test-suite continuity; unused back-compat stubs (`runPreTaskDoneHooks`, `emitVerifyFailedEvent`) were removed. Call sites prefer the new `gateRejection`, `emitGateFailedEvent`, `sendGateFailure`, `notifyGateFailure`, and the gate-name-parameterised `runGateHooks(cmd, cache, gateName, taskID, summary)`.
- NDJSON payload now marshalled via an explicit `gateFailedPayload` struct with stable JSON tags, so field order in stderr is `event → gate → task → hook → exit → ts → detail` instead of Go's alphabetical map ordering.
- `gg task done` shares a single per-command config cache (`hookConfig`) between the pre-hook and post-hook paths — one `config.GGDir` + one `config.Load` per invocation instead of two.
- Installer walk parameters moved to configuration. `.gg/config.yaml` now accepts `doctor.hook_install.skip_dirs` and `doctor.hook_install.max_depth`; defaults are still the built-in skip list + depth 3.

### Documentation

- `docs/verify-gate.md` — dedicated reference: contract, env vars, NDJSON schema with stability guarantees, escape valves (`GG_NO_AUTO_NOTIFY`, `GG_DEBUG`), exit codes table, and troubleshooting.
- `docs/getting-started.md` picks up the installer one-liner in the "Install the verify gate" section.
- `docs/ONBOARDING.md` key-commands table links `gg task done` to the gate and adds `gg doctor --install-task-hooks`.
- `docs/adapters.md` new "gg task-lifecycle hooks" subsection distinguishes pre-done blocking from post-done advisory.
- `docs/demo.sh` now demonstrates `gg doctor --install-task-hooks` at the end of the walkthrough.
- `AGENTS.md` documents the monorepo walk defaults, skip-dir override, and the symlink caveat.

## [0.1.0] - 2026-04-14

### Added

**Core CLI**
- `gg init` — bootstrap project: creates `gg.yaml`, `docker-compose.yaml`, `AGENTS.md`, and `RULES.md` in the project root
- `gg status` — session overview: pending tasks, unread messages, open discussions, and recent decisions
- `gg search <query>` — semantic vector search across decisions, rejections, tasks, notes, and bugs
- `gg record "text"` — canonical verb for recording decisions and rejected approaches (`--decision-status=rejected`); supports `--reason`, `--tags`, `--task`, `--from`
- `gg task create/list/get/done/block` — full task lifecycle with priority, tags, detail, and file-locked ID allocation
- `gg tell <role> <message>` / `gg inbox` — agent-to-agent messaging with sender attribution and read tracking
- `gg note "text"` — ambient context notes, semantically searchable, no lifecycle overhead
- `gg context <query>` — knowledge bundle retrieval: related decisions, rejections, tasks, and notes in one call
- `gg discuss open/resolve/dismiss` — open discussion lifecycle with mandatory resolution before session close
- `gg bug report/triage/start/fix/wontfix` — full bug lifecycle with severity tiers and retrospective enforcement
- `gg doctor` — runtime health check: Qdrant/Memgraph connectivity, indexer binary detection, and `--install-indexers` flag for auto-install via native package managers (go/npm/pip)
- `gg doctor --reconcile` — manual trigger for outbox convergence
- `gg impact <file>` — downstream impact report: graph-traced dependents, exported symbols, and related KB entries
- `gg check` — pre-push gate: surfaces high-severity impact before commits leave the machine
- `gg index` — SCIP-based code graph indexing (Go, TypeScript, Python runners)
- `gg index --changed` — incremental re-index using `git diff` + 1-hop graph invalidation; falls back to full re-index on non-ancestor HEAD
- `gg reembed` — re-embed all stored entries when embedding model changes

**Storage & Embeddings**
- Qdrant vector store with per-project collection namespacing (`{projectID}_decisions`, `_tasks`, etc.)
- Ollama-backed local embeddings (replaced OpenAI dependency); dimension validated at `Generate` time
- File-locked sequential ID allocators for tasks, discussions, and bugs — collision-free under 50 concurrent goroutines
- Outbox pattern for dual-store writes (Qdrant + Memgraph): `UpsertNode`, `UpsertEdge`, and convergence via `--reconcile`
- Embedding model metadata stored with collection; `gg reembed` detects and migrates on model change

**Graph (Memgraph)**
- Memgraph Go client via `neo4j-go-driver/v5` (chosen over `memgraph-go-client` for Bolt compatibility and maintenance)
- Per-project isolation: all nodes stamped with `project_id`; `graph.New()` requires projectID; multi-project sharing safe
- Single query choke-point in `internal/graph/queries.go` — all Cypher routed through `runQuery`, raw `sess.Run` forbidden outside the package
- Boundary node schema for cross-file symbol edges
- Hybrid tier resolution metadata on graph nodes (SCIP-resolved vs. heuristic)
- `indexed_at_commit` tracking + `SweepProject()` — ghost symbols from branch switches are reaped on full re-index
- Non-ancestor detection: if HEAD is not a descendant of the last indexed commit, triggers a full re-index

**Code Indexing**
- SCIP-based indexing pipeline: `scip-go`, `scip-typescript`, `scip-python` (npm-installed)
- Version skew compat-matrix for SCIP indexers (`internal/index/compat`)
- `--changed` invalidation contract spec (`internal/index/CHANGED_CONTRACT.md`)
- `ErrIndexerMissing` typed error for missing indexer binaries

**AGENTS.md & Multi-Agent Protocol**
- Auto-generated `AGENTS.md` on `gg init` with full agent collaboration rules
- Autonomous next-task pickup rule (priority queue with claimed-task broadcast)
- OPEN DISCUSSIONS rule: discussions must be resolved/dismissed before session close
- Broadcast-status rule: cross-agent visibility at pick-up, approach choice, blocker, and completion
- Subagent / multi-agent round rule: orchestrator persists subagent decisions to `gg`
- Bug handling rule: report → triage → start → fix → retrospective lifecycle in AGENTS.md
- Note rule: `gg note` for ambient context that doesn't fit a decision/task

**Observability & Infrastructure**
- HealthCheck middleware in `loadDeps`: fail-fast on Qdrant/Memgraph unavailability
- `--json` flag wired to all commands for structured output
- Double-dash stripping in `requireNonEmpty` for robust arg parsing
- GitHub Actions CI: ubuntu/macos/windows matrix, race detector, `golangci-lint`

**Documentation**
- `README.md`: hero pitch, quickstart, prerequisites, architecture overview
- `docs/architecture.md`, `docs/commands.md`, `docs/adapters.md`, `docs/getting-started.md`, `docs/roadmap.md`
- `LICENSE` (MIT)

### Changed

- Switched embedding backend from OpenAI to local Ollama — no external API dependency, no key required
- `gg init` now creates `AGENTS.md` at project root automatically (previously manual)
- Task ID allocator upgraded from in-memory counter to file-locked (`O_EXCL`) sequential allocator

### Deprecated

- `gg decide` — use `gg record` instead; will be removed in a future major release
- `gg reject` — use `gg record --decision-status=rejected` instead; will be removed in a future major release

### Fixed

- Task ID race condition under concurrent goroutines (replaced with file-locked allocator)
- `scroll_all` truncation in Qdrant scroll API — paginated correctly
- `ctx` propagation through all store and graph calls
- Signal handling and graceful shutdown on interrupt
- `NewValueMap` panic on `[]string` property values in Memgraph
- Qdrant client/server version mismatch warning silenced (cosmetic stderr noise)
- Copylocks vet warning in flock implementation
- `elementId()` not supported in Memgraph 3.0 — replaced with `toString(id(n))`

[Unreleased]: https://github.com/gurkangul/gg-cli/compare/v0.3.23...HEAD
[0.3.23]: https://github.com/gurkangul/gg-cli/compare/v0.3.22...v0.3.23
[0.3.22]: https://github.com/gurkangul/gg-cli/compare/v0.3.21...v0.3.22
[0.3.21]: https://github.com/gurkangul/gg-cli/compare/v0.3.20...v0.3.21
[0.3.20]: https://github.com/gurkangul/gg-cli/compare/v0.3.19...v0.3.20
[0.3.19]: https://github.com/gurkangul/gg-cli/compare/v0.3.18...v0.3.19
[0.3.18]: https://github.com/gurkangul/gg-cli/compare/v0.3.17...v0.3.18
[0.3.17]: https://github.com/gurkangul/gg-cli/compare/v0.3.16...v0.3.17
[0.3.16]: https://github.com/gurkangul/gg-cli/compare/v0.3.15...v0.3.16
[0.3.15]: https://github.com/gurkangul/gg-cli/compare/v0.3.14...v0.3.15
[0.3.14]: https://github.com/gurkangul/gg-cli/compare/v0.3.13...v0.3.14
[0.3.13]: https://github.com/gurkangul/gg-cli/compare/v0.3.12...v0.3.13
[0.3.12]: https://github.com/gurkangul/gg-cli/compare/v0.3.11...v0.3.12
[0.3.11]: https://github.com/gurkangul/gg-cli/compare/v0.3.10...v0.3.11
[0.3.10]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.10
[0.3.7]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.7
[0.3.6]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.6
[0.3.5]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.5
[0.3.4]: https://github.com/gurkangul/gg-cli/releases/tag/v0.3.4
[0.2.0]: https://github.com/gurkangul/gg-cli/releases/tag/v0.2.0
[0.1.0]: https://github.com/gurkangul/gg-cli/releases/tag/v0.1.0
