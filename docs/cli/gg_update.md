## gg update

Update gg to the latest public release

### Synopsis

Downloads the latest gg release binary for this platform from GitHub,
verifies its checksum, and atomically replaces the running executable.

For a local gg-cli checkout with unreleased changes, use:

  gg update --from-source

Source/dev builds are NOT clobbered by the release binary unless --force is
passed, so a maintainer's --from-source build survives. After updating, gg
refreshes registered project artifacts with system sync unless --skip-sync is
passed. Network access happens only when this command (or 'gg update check')
runs, or on a throttled session-start auto-update check.

```
gg update [flags]
```

### Options

```
      --force         install the latest release binary even on a source/dev build or when already up to date
      --from-source   rebuild and install gg from the local gg-cli source checkout instead of the latest public release
  -h, --help          help for update
      --skip-sync     skip post-install managed artifact sync
      --yes           accepted for automation compatibility; gg update never prompts
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg update check](gg_update_check.md)	 - Check whether a newer public gg release is available
