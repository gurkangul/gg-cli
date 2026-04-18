# Claude Code Integration

## Write-Boundary Verification (PostToolUse hook)

`gg verify --file <path>` runs language-appropriate fast checks on a source
file: formatting (`gofmt -l`) and static analysis (`go vet`) for Go files.
Budget: ≤2s per file.

Wire it as a `PostToolUse` hook in `.claude/settings.json` so it fires
automatically after every `Edit` or `Write` tool call Claude Code makes:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "gg verify --file \"$CLAUDE_TOOL_INPUT_FILE_PATH\""
          }
        ]
      }
    ]
  }
}
```

> **Env var note:** `CLAUDE_TOOL_INPUT_FILE_PATH` is the `file_path` parameter
> from the `Edit`/`Write` tool call, injected by the Claude Code harness into
> the hook's environment. If the name differs in your Claude Code version, use
> whatever env var carries the edited file path.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All checks passed |
| 7 | Checks failed (format or vet error) — Claude Code should treat this as a signal to fix the issue before continuing |

### Respecting GG_ENFORCEMENT

The hook fires regardless of `GG_ENFORCEMENT` — `gg verify --file` is a
read-only diagnostic. Set `GG_NO_VERIFY=1` to skip it temporarily:

```json
{
  "type": "command",
  "command": "[ \"${GG_NO_VERIFY:-0}\" = '1' ] || gg verify --file \"$CLAUDE_TOOL_INPUT_FILE_PATH\""
}
```

### Manual smoke test

1. Introduce a formatting violation:
   ```sh
   printf 'package cmd\nfunc Bad( ){ }\n' > /tmp/bad.go
   ```
2. Run verify:
   ```sh
   gg verify --file /tmp/bad.go
   ```
3. Expect non-zero exit and `✗ gofmt` in output.

## Regression Gate (pre-task-done hook)

See [verify-gate.md](verify-gate.md) for the `gg task done` pre-hook that
runs all registered bug repro scripts. That gate is installed by
`gg doctor --install-task-hooks` and controlled by `GG_ENFORCEMENT`.
