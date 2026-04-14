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
  -h, --help               help for doctor
      --install-indexers   install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)
      --reconcile          scan the outbox for incomplete dual-store writes and report what needs repair
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

