## gg doctor

Diagnose and repair gg configuration

### Synopsis

Check service connectivity and verify that required indexer binaries
are available. Use --install-indexers to automatically install missing SCIP
binaries using their native package managers (go install, npm, pip).

Exit codes:
  0  all checks passed
  1  one or more checks failed

```
gg doctor [flags]
```

### Options

```
      --agent string             restrict --install-agent-hooks to a single agent (claude, cursor, codex, bmad, gsd)
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
      --fix-binary               with --check-binary: rebuild and reinstall gg via go install ./cmd/gg when the binary is stale
      --force                    with --install-agent-hooks: bypass detection and install for the named agent(s)
      --force-reset              with --check-contract --fix: overwrite manually-edited (DRIFTED) contract blocks
      --heal                     migrate legacy .gg/telemetry.jsonl and .gg/cache/ to ~/.gg/projects/<id>/ (idempotent)
  -h, --help                     help for doctor
      --history                  with --check-secrets: run full git history scan only (gitleaks detect)
      --install-agent-hooks      write agent-side config (SessionStart hook / alwaysApply rule / read-preload) to enforce gg usage
      --install-agents-md        inject the gg tracker-rules managed block into AGENTS.md (idempotent; alias for --install-agent-hooks --agent codex)
      --install-indexers         install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)
      --install-secret-scanner   download and install the pinned gitleaks binary into ~/.gg/bin/gitleaks (checksum-verified)
      --install-task-hooks       install verify-gate (pre-task-done.d) + post-done task-done.d hooks; auto-detects Go (go.mod) and/or Node/Bun (package.json)
      --reconcile                scan the outbox for incomplete dual-store writes and report what needs repair
      --refresh-hooks            overwrite drifted gg-managed hook templates after backing up each stale copy
      --refresh-hooks-force      with --refresh-hooks: also overwrite user-customized hooks that lack gg-template markers
      --staged                   with --check-secrets: run staged/working-tree scan only (gitleaks detect --no-git)
      --sync-artifacts           compare .gg/installed.json against the current CLI templates and show a drift table
      --sync-baseline            rescan project and refresh .gg/file-size-baseline.json with current line counts
      --wipe-brain               drop all Qdrant collections and Memgraph nodes for this project (destructive — use for testing)
      --yes                      with --wipe-brain: skip interactive confirmation
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

