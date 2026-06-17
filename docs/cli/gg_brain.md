## gg brain

Portable brain snapshot (export / import / status)

### Synopsis

Manage a git-trackable snapshot of gg's shared brain.

  gg brain export   — write .gg/brain/ from current vector store + code-graph state
  gg brain import   — restore vector store + code graph from .gg/brain/
  gg brain status   — show snapshot metadata and checksum status

### Options

```
  -h, --help   help for brain
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg brain backfill](gg_brain_backfill.md)	 - Migrate tag-based Task↔Decision links to code-graph edges
* [gg brain export](gg_brain_export.md)	 - Serialize project brain to .gg/brain/ (JSONL, payload-only)
* [gg brain import](gg_brain_import.md)	 - Restore vector store + code graph from .gg/brain/ (idempotent)
* [gg brain reindex-decisions](gg_brain_reindex-decisions.md)	 - Replay Decision nodes into the code graph from the decision store
* [gg brain status](gg_brain_status.md)	 - Show brain snapshot metadata and verify checksums
