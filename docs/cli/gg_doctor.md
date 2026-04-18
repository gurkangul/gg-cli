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
      --agent string          restrict --install-agent-hooks to a single agent (claude, cursor, aider, codex, zai)
      --dry-run               with --install-agent-hooks: report what would change without writing anything
      --force                 with --install-agent-hooks: bypass detection and install for the named agent(s)
      --heal                  migrate legacy .gg/telemetry.jsonl and .gg/cache/ to ~/.gg/projects/<id>/ (idempotent)
  -h, --help                  help for doctor
      --install-agent-hooks   write agent-side config (SessionStart hook / alwaysApply rule / read-preload) to enforce gg usage
      --install-agents-md     inject the gg tracker-rules managed block into AGENTS.md (idempotent; alias for --install-agent-hooks --agent codex)
      --install-indexers      install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)
      --install-task-hooks    install verify-gate (pre-task-done.d) + post-done task-done.d hooks; auto-detects Go (go.mod) and/or Node/Bun (package.json)
      --reconcile             scan the outbox for incomplete dual-store writes and report what needs repair
      --wipe-brain            drop all Qdrant collections and Memgraph nodes for this project (destructive — use for testing)
      --yes                   with --wipe-brain: skip interactive confirmation
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

