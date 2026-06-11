## gg init

Initialize shared gg infrastructure (~/.gg/) and register this project

```
gg init [flags]
```

### Options

```
  -h, --help                  help for init
      --no-index              skip the post-setup index prompt (non-interactive no)
      --no-index-hooks        skip auto-installing the CodeGraph git hooks (pre-push/post-merge/post-commit)
      --skip-enforcement      skip installing agent hooks and task-done gate scripts
      --with-index gg index   also run gg index after setup (non-interactive yes)
      --yes                   non-interactive: skip prompts
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
