## gg system brain

Cross-project brain health operations

### Synopsis

Inspect brain readiness across every project in ~/.gg/projects.json.

This is intentionally separate from 'gg system sync': sync also performs
contract/hooks propagation plus tracker self-heal, while brain status verifies
project_id, backend reachability, portable brain snapshots, drift, and
CodeGraph freshness.

### Options

```
  -h, --help   help for brain
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg system](gg_system.md)	 - Host-level gg operations (cross-project registry + sync)
* [gg system brain status](gg_system_brain_status.md)	 - Show per-project brain snapshot/backend/CodeGraph health
