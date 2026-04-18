# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

## [0.1.0] - 2026-04-14

### Added

**Core CLI**
- `gg init` — bootstrap project: creates `gg.yaml`, `docker-compose.yaml`, `AGENTS.md`, and `RULES.md` in the project root
- `gg status` — session overview: pending tasks, unread messages, open discussions, and recent decisions
- `gg search <query>` — semantic vector search across decisions, rejections, tasks, notes, and bugs
- `gg record "text"` — canonical verb for recording decisions (`--stance=accept`, default) and rejected approaches (`--stance=reject`); supports `--reason`, `--tags`, `--task`, `--from`
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

- `gg decide` — use `gg record` (or `gg record --stance=accept`) instead; will be removed in a future major release
- `gg reject` — use `gg record --stance=reject` instead; will be removed in a future major release

### Fixed

- Task ID race condition under concurrent goroutines (replaced with file-locked allocator)
- `scroll_all` truncation in Qdrant scroll API — paginated correctly
- `ctx` propagation through all store and graph calls
- Signal handling and graceful shutdown on interrupt
- `NewValueMap` panic on `[]string` property values in Memgraph
- Qdrant client/server version mismatch warning silenced (cosmetic stderr noise)
- Copylocks vet warning in flock implementation
- `elementId()` not supported in Memgraph 3.0 — replaced with `toString(id(n))`

[0.1.0]: https://github.com/gurkangul/gg/releases/tag/v0.1.0
[Unreleased]: https://github.com/gurkangul/gg/compare/v0.1.0...HEAD
