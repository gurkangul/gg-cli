## gg config set

Set a config field (e.g. developer.command 'gsd --model openai-codex/gpt-5.3-codex')

### Synopsis

Set a single config field and persist it to .gg/config.yaml.

Supported keys:
  developer.command    — any subprocess command used for worker panes
  roles.developer.command — explicit developer role command override
  roles.reviewer.command  — reviewer/verifier role command
  developer.transport  — allowlist: cmux, side-session-prompt
  developer.agent      — deprecated legacy alias for developer.command
  developer.spawn_command — deprecated legacy alias for developer.command

Examples:
  gg config set developer.command "gsd --model openai-codex/gpt-5.3-codex"
  gg config set roles.reviewer.command "gsd --model openai-codex/gpt-5.3-codex"
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
