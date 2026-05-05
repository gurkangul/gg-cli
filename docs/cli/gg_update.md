## gg update

Update gg to the latest public release

### Synopsis

Checks the latest public gg module version and, when needed, runs:

  go install github.com/gurkangul/gg-cli/cmd/gg@latest

After installing, gg refreshes registered project artifacts with system sync
unless --skip-sync is passed. Network access happens only when this command
or 'gg update check' is run explicitly.

```
gg update [flags]
```

### Options

```
      --force       run go install even when the current version appears up to date
  -h, --help        help for update
      --skip-sync   skip post-install managed artifact sync
      --yes         accepted for automation compatibility; gg update never prompts
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg update check](gg_update_check.md)	 - Check whether a newer public gg release is available
