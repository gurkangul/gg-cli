## gg config set

Set a config field (e.g. developer.agent gsd-sonnet-4.6)

### Synopsis

Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  developer.agent      — allowlist: gsd-sonnet-4.6, claude-sonnet-4.5, claude-opus-4.7, unconfigured
  developer.transport  — allowlist: cmux, side-session-prompt
  developer.spawn_command — any string (custom agent launch command override)

Examples:
  gg config set developer.agent gsd-sonnet-4.6
  gg config set developer.transport cmux

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

