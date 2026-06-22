## gg telemetry

[experimental] Manage local usage telemetry

### Synopsis

Experimental: may change or be removed in a MINOR without a deprecation cycle (see docs/stability.md §2).

Local-only, PII-free usage telemetry. Opt-in — disabled by default.

Enable in your project config:
  gg config set telemetry.enabled true

Or via environment variable (temporary):
  GG_TELEMETRY=1 gg status

Disable permanently:
  gg config set telemetry.enabled false

```
gg telemetry [flags]
```

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
* [gg telemetry compact-missed](gg_telemetry_compact-missed.md)	 - Show per-verb missed compact savings (last 7 days, agent-origin only)
* [gg telemetry summary](gg_telemetry_summary.md)	 - Show command usage summary for the last 7 days
