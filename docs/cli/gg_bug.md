## gg bug

Manage bug lifecycle

### Synopsis

Track defects from discovery through fix. Bugs are stored in Qdrant and
searchable by description. Each bug moves through a lifecycle:
  open → fixing → fixed | wontfix → reopened → fixing → fixed

### Options

```
  -h, --help   help for bug
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg bug attach-repro](gg_bug_attach-repro.md)	 - Attach a repro script to an already-fixed bug
* [gg bug fix](gg_bug_fix.md)	 - Mark a bug as fixed
* [gg bug get](gg_bug_get.md)	 - Show bug details
* [gg bug list](gg_bug_list.md)	 - List bugs
* [gg bug reindex](gg_bug_reindex.md)	 - Replay bug AFFECTS edges into Memgraph
* [gg bug reopen](gg_bug_reopen.md)	 - Reopen a fixed or wontfix bug
* [gg bug report](gg_bug_report.md)	 - Report a new bug
* [gg bug run-repros](gg_bug_run-repros.md)	 - Run all registered repro scripts for fixed bugs
* [gg bug scan-refs](gg_bug_scan-refs.md)	 - Scan text for BUG-NNN references and auto-reopen any that are fixed
* [gg bug start](gg_bug_start.md)	 - Move a bug to 'fixing' status
* [gg bug triage](gg_bug_triage.md)	 - Auto context bundle for fixing a bug
* [gg bug wontfix](gg_bug_wontfix.md)	 - Close a bug as won't-fix
