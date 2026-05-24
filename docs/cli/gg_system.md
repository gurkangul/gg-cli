## gg system

Host-level gg operations (cross-project registry + sync)

### Synopsis

Commands that operate on all gg-registered projects at once.

The registry lives at ~/.gg/projects.json and is populated automatically
by 'gg init'. Use 'gg system register' to add an existing project that
was initialized before the registry existed.

### Options

```
  -h, --help   help for system
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg system brain](gg_system_brain.md)	 - Cross-project brain health operations
* [gg system register](gg_system_register.md)	 - Add a project to the registry (or prune dead entries)
* [gg system sync](gg_system_sync.md)	 - Propagate latest gg artifacts and self-heal tracker collections
