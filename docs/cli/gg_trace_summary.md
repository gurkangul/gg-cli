## gg trace summary

Per-operation latency breakdown (p50/p95/p99)

### Synopsis

Groups all recorded spans by operation and shows p50/p95/p99 latencies and sample count.

```
gg trace summary [flags]
```

### Options

```
  -h, --help           help for summary
      --since string   only include spans newer than this duration (e.g. 1h, 2d)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg trace](gg_trace.md)	 - [experimental] Inspect GG_TRACE span data
