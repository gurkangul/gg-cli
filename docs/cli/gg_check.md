## gg check

Pre-push health check — open tasks, unresolved discussions

### Synopsis

Run a quick health check before pushing code.

Reports:
  - Open (pending/blocked) tasks that may be relevant to the current branch
  - Unresolved discussions that need a decision before merging

Exit codes (default / warn mode):
  0  always — issues are printed to stderr as warnings

Exit codes (--strict mode):
  0  all clear
  1  one or more issues found

Usage as a git pre-push hook (strict mode):
  echo "gg check --strict" >> .git/hooks/pre-push
  chmod +x .git/hooks/pre-push

Agent/pipeline usage (non-blocking, machine-readable):
  gg check --json

```
gg check [flags]
```

### Options

```
  -h, --help     help for check
      --strict   exit 1 when issues are found (for git hooks)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
