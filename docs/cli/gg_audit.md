## gg audit

Session mutation audit (called by PostToolUse and Stop hooks)

### Synopsis

Track Edit/Write/MultiEdit mutations during a session and emit a
warning at session end when N>=3 mutations happened with no gg
record/decide/task calls — non-blocking, visibility only.

This command is called automatically by hooks installed via:
  gg doctor --install-agent-hooks

Set GG_NO_AUDIT=1 to suppress both the track and report hooks.

### Options

```
  -h, --help   help for audit
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg audit decide-gaps](gg_audit_decide-gaps.md)	 - Flag gg messages containing decision-language but no corresponding gg record
* [gg audit file-size](gg_audit_file-size.md)	 - List source files violating the 500-line (800 for tests) size rule
* [gg audit gaps](gg_audit_gaps.md)	 - List files with recent git commits but no gg record/decision/task coverage
* [gg audit health](gg_audit_health.md)	 - Reopen rate + surface pressure metrics for quality trend analysis
* [gg audit inbox-obedience](gg_audit_inbox-obedience.md)	 - Measure inbox-obey compliance per agent role
* [gg audit repeat-work](gg_audit_repeat-work.md)	 - Surface multi-iteration patterns that likely indicate a bug loop
* [gg audit report](gg_audit_report.md)	 - Emit audit warning at session end if mutations are untracked (Stop hook)
* [gg audit track](gg_audit_track.md)	 - Record one file mutation in the session audit log (PostToolUse hook)
* [gg audit trends](gg_audit_trends.md)	 - Quality-signal metrics: bug reopen rate over a lookback window

