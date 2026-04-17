## gg telemetry

Manage local usage telemetry

### Synopsis

Local-only, PII-free usage telemetry. Opt-in — disabled by default.

Enable in your project config:
  gg config set telemetry.enabled true

Or via environment variable (temporary):
  GG_TELEMETRY=1 gg status

Disable permanently:
  gg config set telemetry.enabled false

### Options

```
  -h, --help   help for telemetry
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg telemetry summary](gg_telemetry_summary.md)	 - Show command usage summary for the last 7 days

