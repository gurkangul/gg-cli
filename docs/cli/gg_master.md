## gg master

Master-session utilities: session handoff, resume state

### Synopsis

Utilities for the Opus master session that orchestrates worker panes.

Subcommands:
  resume — print a structured session-handoff dump so a fresh master session
            can re-hydrate gg state without asking the user to re-explain.

Typical use:
  # At the start of a fresh master session, after the user types "devam" / "resume":
  gg master resume

### Options

```
  -h, --help   help for master
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg master resume](gg_master_resume.md)	 - Print a structured session-handoff dump for a fresh master session

