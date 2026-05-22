## gg brain import

Restore Qdrant + Memgraph from .gg/brain/ (idempotent)

### Synopsis

Import a brain snapshot from .gg/brain/ into the local Qdrant and Memgraph stores.

Validates manifest SHA-256 checksums and embedding model compatibility before writing.
By default uses upsert semantics — safe to run multiple times.

Use --wipe to drop all data before importing (destructive, requires --yes).

```
gg brain import [flags]
```

### Options

```
      --dry-run                  report counts without writing
      --force-project-mismatch   allow importing a snapshot from a different project_id
  -h, --help                     help for import
      --no-reindex               skip gg reindex --embed trigger after import
      --skip-embed-check         bypass embedding model mismatch check
      --wipe                     drop all project data before importing (destructive)
      --yes                      confirm destructive --wipe without interactive prompt
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
