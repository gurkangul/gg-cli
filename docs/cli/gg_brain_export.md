## gg brain export

Serialize project brain to .gg/brain/ (JSONL, payload-only)

### Synopsis

Export all Qdrant collections and Memgraph graph data to deterministic
JSONL files under .gg/brain/. Vectors are excluded by default — run
'gg reindex --embed' after import to rebuild them.

The output is git-trackable and byte-deterministic: identical store
state always produces identical files.

```
gg brain export [flags]
```

### Options

```
      --dry-run   print what would be written without writing
  -h, --help      help for export
      --strict    exit 1 if any secret pattern is found, write nothing
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
