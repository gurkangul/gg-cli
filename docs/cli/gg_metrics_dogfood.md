## gg metrics dogfood

Per-project velocity and rework rate for dogfood sessions

### Synopsis

Compute two dogfood health signals for the project:

  velocity      = done tasks / week over the lookback window
  rework_rate   = decisions referencing rework iterations / total task decisions
  gap_rate      = decisions referencing accept-with-gap / total task closures

velocity measures throughput. rework_rate measures how often shipped work
needed a revision cycle. gap_rate measures how often work shipped with known
gaps (accepted but imperfect). Together they answer: are we moving fast, and
is the quality holding?

Signals are advisory — exits 0 regardless of values.

```
gg metrics dogfood [flags]
```

### Options

```
      --compact        one-line summary — three metrics, no chart
  -h, --help           help for dogfood
      --since string   lookback window (e.g. 7d, 14d, 30d) (default "14d")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg metrics](gg_metrics.md)	 - [experimental] Project health metrics
