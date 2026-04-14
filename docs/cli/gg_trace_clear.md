## gg trace clear

Delete old trace files

### Synopsis

Deletes .gg/traces/*.jsonl files older than the given duration.

```
gg trace clear [flags]
```

### Options

```
  -h, --help                help for clear
      --older-than string   delete files older than this duration (e.g. 7d, 30d) (default "7d")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg trace](gg_trace.md)	 - Inspect GG_TRACE span data

