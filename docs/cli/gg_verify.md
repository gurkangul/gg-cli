## gg verify

Write-boundary verification for a source file

### Synopsis

Run language-appropriate fast checks on a single file.
Used as a PostToolUse hook to catch regressions at the write boundary
before they compound. Budget: ≤2s per file.

For Go files: gofmt (formatting) + go vet (on the package).
Other languages: skipped (exits 0).

Wire as a Claude Code PostToolUse hook — see docs/claude-code-integration.md.

```
gg verify [flags]
```

### Options

```
      --file string   source file to verify (required)
  -h, --help          help for verify
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
