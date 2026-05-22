## gg config set

Set a config field

### Synopsis

Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  backup.enabled      — true/false session-start auto-backup toggle
  backup.interval     — staleness threshold for brain export (e.g. 24h, 6h)
  backup.timeout      — per-backup subprocess timeout (e.g. 30s, 2m)

Examples:
  gg config set backup.interval 6h

```
gg config set <key> <value> [flags]
```

### Options

```
  -h, --help   help for set
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg config](gg_config.md)	 - Inspect or modify project configuration
