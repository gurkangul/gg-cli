## gg brain backfill

Migrate tag-based Task↔Decision links to Memgraph edges

### Synopsis

Scan Qdrant for implicit Task↔Decision relationships and write them as
explicit Memgraph edges — (Decision)-[:DECIDES]->(Task) — tagged with
created_by=backfill_v1 for rollback identification.

Two sources are evaluated:

  Source 1 — Direct links: Decision.task_id field carries an explicit task ID.
             These are unambiguous and always migrated.

  Source 2 — Tag overlap: if exactly one decision and exactly one task share a
             tag, that pair is unambiguous and is migrated. Tags matched by
             multiple decisions or tasks are reported as ambiguous and skipped.

By default the command runs in dry-run mode and prints a report without
writing anything. Pass --apply to execute the migration.

Examples:
  gg brain backfill              # audit only
  gg brain backfill --apply      # write edges to Memgraph

```
gg brain backfill [flags]
```

### Options

```
      --apply   write edges to Memgraph (default: dry-run only)
  -h, --help    help for backfill
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)

