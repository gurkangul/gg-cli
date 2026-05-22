## gg system sync

Propagate latest gg artifacts (contract + hooks) to every registered project

### Synopsis

Iterates ~/.gg/projects.json and runs doctor --fix in each project so
contract and hook updates baked into a new gg binary reach every host-local
project without the user cd'ing to each repo.

Stages per project:
  1. gg doctor --check-contract --fix      (contract block drift repair)
  2. gg doctor --install-agent-hooks       (idempotent agent-hook refresh)
  3. gg doctor --install-task-hooks        (idempotent task-hook refresh)

Projects whose root directory no longer exists are skipped with a
warning — prune them with 'gg system register --prune' after verifying.

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
