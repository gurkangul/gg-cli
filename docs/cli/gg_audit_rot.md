## gg audit rot

Report decaying ledger entries: stale evidence, unproven load-bearing rules, orphans

### Synopsis

Sweep the decision ledger for entries that have quietly gone bad.

Reports three kinds of rot:
  stale       evidence-backed decisions whose verification is old enough that it
              should be re-checked before being leaned on
  unproven    pinned or policy-tagged decisions carrying NO evidence — the most
              load-bearing entries in the ledger, never actually verified
  orphan      active decisions with no link in either direction, reachable only
              by search and never by walking the graph

Read-only: nothing is superseded, retagged, or rewritten, and the command always
exits 0. Pins and constraint/convention/policy tags are exempt from staleness —
a recorded rule is not a measurement that expires.

See also: gg backlinks (who links here), gg related (walk the graph)

```
gg audit rot [flags]
```

### Options

```
      --compact         one line per entry — preserves agent context window
  -h, --help            help for rot
      --include-aging   also report decisions that are aging but not yet stale
      --limit int       max entries to list per category (0 = no limit) (default 15)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
