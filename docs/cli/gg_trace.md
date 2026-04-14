## gg trace

Inspect GG_TRACE span data

### Synopsis

Commands for reading, summarising, and clearing trace spans.

Trace recording is enabled by setting GG_TRACE=1 before running gg commands.
Each operation is appended as a JSON line to .gg/traces/YYYY-MM-DD.jsonl.

### Options

```
  -h, --help   help for trace
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg trace clear](gg_trace_clear.md)	 - Delete old trace files
* [gg trace show](gg_trace_show.md)	 - Print recorded spans
* [gg trace summary](gg_trace_summary.md)	 - Per-operation latency breakdown (p50/p95/p99)

