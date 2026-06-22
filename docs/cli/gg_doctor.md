## gg doctor

Diagnose and repair gg configuration

### Synopsis

Check service connectivity and verify that required indexer binaries
are available. Use --install-indexers to automatically install missing SCIP
binaries with maintained package-manager flows (go, TypeScript, Python).
Swift detection is supported, but Swift graph generation requires a user-provided
compatible scip-swift binary because gg does not bundle one.

Doctor reports the same CodeGraph freshness contract used by session-start,
next, impact, and index status. It never starts a background index daemon;
use --fix-index for explicit repair or gg index --watch / gg watch --index for
optional foreground active mode.

Exit codes:
  0  all checks passed
  1  one or more checks failed

```
gg doctor [flags]
```

### Options

```
      --agent string             restrict --install-agent-hooks to a single agent (claude, cursor, codex, gemini, openhands, bmad, gsd)
      --apply                    with --sync-artifacts: re-install drifted or missing artifacts
      --bypass-audit             list GG_ENFORCEMENT=off bypass events from ~/.gg/projects/<id>/state.json (default: last 7d)
      --bypass-since string      with --bypass-audit: time window (7d, 24h, 30d, or RFC3339 timestamp) (default "7d")
      --capture-lint-baseline    run golangci-lint and write .gg/lint-baseline.json; the 60-lint-gate.sh pre-done hook uses this to block new warnings
      --check-binary             verify the installed gg binary is not older than the HEAD commit of the local gg-cli source
      --check-contract           compare the managed contract block in each agent's entry-point file against the current template (exit 1 on drift)
      --check-secrets            scan the repo for secrets using gitleaks (staged + history); falls back to narrow-regex scan when gitleaks is absent
      --diagnose-sandbox         probe localhost TCP to detect sandbox restrictions; reports 'TCP localhost permitted' or 'TCP localhost BLOCKED'
      --dry-run                  with --install-agent-hooks: report what would change without writing anything
      --fix                      with --check-contract: repair STALE and MISSING entries; refuses DRIFTED without --force-reset
      --fix-binary               with --check-binary: rebuild and reinstall gg from the local source checkout when the binary is stale
      --fix-git-identity         reset a repo-local agent git identity so commits are attributed to you (the human)
      --fix-index                refresh a missing or stale code graph by running the recommended gg index command(s)
      --force                    with --install-agent-hooks: bypass detection and install for the named agent(s)
      --force-reset              with --check-contract --fix: overwrite manually-edited (DRIFTED) contract blocks
      --heal                     migrate legacy .gg/telemetry.jsonl and .gg/cache/ to ~/.gg/projects/<id>/ (idempotent)
  -h, --help                     help for doctor
      --history                  with --check-secrets: run full git history scan only (gitleaks detect)
      --install-agent-hooks      write agent-side config (SessionStart hook / alwaysApply rule / read-preload) to enforce gg usage
      --install-agents-md        inject the gg tracker-rules managed block into AGENTS.md (idempotent; alias for --install-agent-hooks --agent codex)
      --install-index-hooks      install opt-in git hooks (pre-push + post-merge) that run gg index --changed to keep the local CodeGraph fresh; foreground + non-blocking, not a daemon
      --install-indexers         install missing SCIP indexer binaries with maintained installers (scip-go, scip-typescript, scip-python)
      --install-secret-scanner   download and install the pinned gitleaks binary into ~/.gg/bin/gitleaks (checksum-verified)
      --install-task-hooks       install verify-gate (pre-task-done.d) + post-done task-done.d hooks; auto-detects Go (go.mod) and/or Node/Bun (package.json)
      --reconcile                scan the outbox for incomplete dual-store writes and report what needs repair
      --refresh-hooks            overwrite drifted gg-managed hook templates after backing up each stale copy
      --refresh-hooks-force      with --refresh-hooks: also overwrite user-customized hooks that lack gg-template markers
      --staged                   with --check-secrets: run staged/working-tree scan only (gitleaks detect --no-git)
      --strict                   exit non-zero when artifact drift is detected (for CI); without --strict, drift is advisory and does not affect the exit code
      --sync-artifacts           compare .gg/installed.json against the current CLI templates and show a drift table
      --sync-baseline            rescan project and refresh .gg/file-size-baseline.json with current line counts
      --wipe-brain               drop all vector collections and code-graph nodes for this project (destructive — use for testing)
      --yes                      auto-accept confirmations for state-changing doctor flows (--wipe-brain, --heal RULES.md re-render, --install-task-hooks Makefile test-tier); also honored via GG_YES=1
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
