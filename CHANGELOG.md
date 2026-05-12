# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[0.1.0]: https://github.com/gurkangul/gg-cli/releases/tag/v0.1.0
[Unreleased]: https://github.com/gurkangul/gg-cli/compare/v0.1.0...HEAD
