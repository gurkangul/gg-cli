## gg telemetry compact-missed

Show per-verb missed compact savings (last 7 days, agent-origin only)

### Synopsis

For each verb that has at least one compact-mode call (i.e. the
command has a working compact render path), report how many agent-origin calls
still ran default and the estimated bytes/tokens that would have been saved if
those calls had used --compact. Human full-reads are excluded — they are not
missed compact opportunities. The estimate is per-verb and conservative: it
uses each verb's own observed avg-bytes-saved-per-compact-call.

```
gg telemetry compact-missed [flags]
```

### Options

```
  -h, --help   help for compact-missed
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg telemetry](gg_telemetry.md)	 - [experimental] Manage local usage telemetry
