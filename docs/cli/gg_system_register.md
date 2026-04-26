## gg system register

Add a project to the registry (or prune dead entries)

### Synopsis

Normally 'gg init' auto-registers. Use this command for:

  - projects initialized before the registry existed (backfill)
  - rehoming a project whose root directory moved
  - removing entries that point at deleted / missing directories

Examples:
  gg system register                         (register cwd)
  gg system register --path ~/my/app         (register a specific dir)
  gg system register --prune                 (drop entries with missing roots)
  gg system register --list                  (show registry contents)


```
gg system register [flags]
```

### Options

```
  -h, --help          help for register
      --list          print the current registry and exit
      --path string   project root to register (defaults to cwd)
      --prune         remove registry entries whose root directory no longer exists
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg system](gg_system.md)	 - Host-level gg operations (cross-project registry + sync)

