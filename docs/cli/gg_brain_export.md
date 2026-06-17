## gg brain export

Serialize project brain to .gg/brain/ (JSONL, payload-only)

### Synopsis

Export all vector store collections and code-graph data to deterministic
JSONL files under .gg/brain/. Vectors are excluded by default — run
'gg reindex --embed' after import to rebuild them.

The output is git-trackable and byte-deterministic: identical store
state always produces identical files.

```
gg brain export [flags]
```

### Options

```
      --dry-run           print what would be written without writing
  -h, --help              help for export
      --if-stale string   only export when snapshot is older than DURATION (e.g. 24h); exit 0 when fresh
      --strict            exit 1 if any secret pattern is found, write nothing
      --verbose           show skip reason when --if-stale skips the export
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
