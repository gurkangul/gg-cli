## gg reconcile

[experimental] Reconcile append-only task events with the live task projection

### Synopsis

Experimental: may change or be removed in a MINOR without a deprecation cycle (see docs/stability.md §2).

Compares .gg/brain/task-events.jsonl against the vector store task projection.

Default mode is read-only: reports missing projections, projection drift,
orphaned owners/leases, and stale leases. Use --apply to repair safe cases:
missing non-cancelled projections are replayed from .gg/brain/tasks.jsonl,
drifted task lifecycle fields are reset from the event log, stale leases are
released back to pending, and projected cancelled tasks are removed.

```
gg reconcile [flags]
```

### Options

```
      --apply   repair safe task projection drift
  -h, --help    help for reconcile
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
