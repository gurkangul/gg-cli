## gg config set

Set a config field (e.g. developer.command 'gsd --model openai-codex/gpt-5.3-codex')

### Synopsis

Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  backup.enabled      — true/false session-start auto-backup toggle
  backup.interval     — staleness threshold for brain export (e.g. 24h, 6h)
  backup.timeout      — per-backup subprocess timeout (e.g. 30s, 2m)
  developer.command    — any subprocess command used for worker panes
  roles.<role>.command   — role command override (master, developer, reviewer, ...)
  roles.<role>.transport — role transport metadata
  runtime_profiles.<name>.command        — named runtime command
  runtime_profiles.<name>.role           — role served by the profile
  runtime_profiles.<name>.priority       — lower number wins
  runtime_profiles.<name>.health_command — command exit 0 means healthy
  developer.transport  — allowlist: cmux, side-session-prompt
  developer.agent      — deprecated legacy alias for developer.command
  developer.spawn_command — deprecated legacy alias for developer.command

Examples:
  gg config set backup.interval 6h
  gg config set developer.command "gsd --model openai-codex/gpt-5.3-codex"
  gg config set roles.reviewer.command "codex --model gpt-5.3-codex"
  gg config set runtime_profiles.gsd-dev.command "gsd --model openai-codex/gpt-5.3-codex"
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
