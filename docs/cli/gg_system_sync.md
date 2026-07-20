## gg system sync

Propagate latest gg artifacts and self-heal tracker collections

### Synopsis

Iterates ~/.gg/projects.json and runs tracker self-heal plus doctor --check-contract --fix
in each project so contract, hook, and tracker-collection updates baked into a
new gg binary reach every host-local project without the user cd'ing to each repo.

Stages per project:
  1. tracker self-heal                      (ensure decision/task/message/etc collections)
  2. gg doctor --check-contract --fix      (contract block drift repair)
  3. gg doctor --install-agent-hooks       (idempotent agent-hook refresh)
  4. gg doctor --install-task-hooks        (idempotent task-hook refresh)
  5. gg doctor --install-index-hooks       (refresh CodeGraph git hooks — only where already installed)

Projects whose root directory no longer exists are skipped with a
warning — prune them with 'gg system register --prune' after verifying.
Projects with missing .gg/config.yaml are also skipped, as they were likely
partially removed or migrated away.


```
gg system sync [flags]
```

### Options

```
      --contract-force-reset   pass --force-reset to gg doctor --check-contract --fix (overwrites manually-edited contract blocks)
      --contract-only          skip the agent-hook refresh stage (faster when only the contract changed)
      --dry-run                print what would change without writing
  -h, --help                   help for sync
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg system](gg_system.md)	 - Host-level gg operations (cross-project registry + sync)
