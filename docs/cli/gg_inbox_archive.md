## gg inbox archive

Archive stale agent-to-agent status broadcasts (out of inbox, kept in JSONL)

### Synopsis

Archive ephemeral audience=agents status broadcasts ("TASK-N started/done"…)
older than a cutoff. Archived messages drop out of the default inbox so it stops
bloating, but stay in JSONL (forward-only — never deleted), recoverable via the
durable brain. Consolidation layer (TASK-470).

```
gg inbox archive [flags]
```

### Options

```
  -h, --help                help for archive
      --older-than string   archive audience=agents broadcasts older than this duration (default 720h = 30 days) (default "720h")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg inbox](gg_inbox.md)	 - Read unread messages
