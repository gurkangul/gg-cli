## gg trace show

Print recorded spans

### Synopsis

Prints spans in reverse-chronological order (newest first). Filter by op name, time window, and result count.

```
gg trace show [flags]
```

### Options

```
  -h, --help           help for show
      --limit int      max spans to show (0 = all) (default 50)
      --op string      filter by operation name (e.g. search, record, index)
      --since string   only show spans newer than this duration (e.g. 1h, 2d, 30m)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg trace](gg_trace.md)	 - Inspect GG_TRACE span data

