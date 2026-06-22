## gg canon suggest

Emit the distilled-vs-raw packet + a structured op contract the agent fills (NO LLM, offline)

### Synopsis

gg canon suggest is a no-LLM, no-network consolidation ritual.

gg has no cloud LLM — the AGENT does the distilling. suggest emits a deterministic
packet so the agent can converge the canon by hand:

  DISTILLED   the canon already inherited (curated + auto-derived)
  RAW DELTA   recent ledger entries not yet reflected in a curated area
  OP CONTRACT a structured add/edit/delete template (typed + tagged + dedup hint)

Fill the contract, then apply it:
  gg canon suggest --json > ops.json     # edit the "operations" array
  gg canon apply --ops ops.json          # deterministic, offline
  # …or run the printed 'gg canon set <area> "…"' lines directly.

The auto-derived layer is a live digest of the ledger and is NOT editable here —
to change it, edit the underlying records (gg record / gg reject / gg bug).

```
gg canon suggest [flags]
```

### Options

```
  -h, --help   help for suggest
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg canon](gg_canon.md)	 - Distilled institutional memory — the durable knowledge every agent should start with
