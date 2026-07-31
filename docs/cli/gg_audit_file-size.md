## gg audit file-size

List source files violating the 500-line (800 for tests) size rule

### Synopsis

Walk the project and report files that exceed the size limit.
Source files (.go/.ts/.js/.py/.rs/.java) must stay under 500 lines;
test files (*_test.go, *.test.*, *.spec.*) under 800 lines.

A file at or above 90% of its limit (450 source / 720 test) is also listed
under "approaching limit". Those are not violations and never affect the exit
code — they are the warning band that lets a file be split on the next touch
instead of at the wall.

Files in the .gg/file-size-baseline.json grandfather list are only
flagged when they have grown beyond their baseline value. The baseline does not
suppress the warning band: a grandfathered file is exempt from failing, not
from being visible.

Use --no-baseline to see raw violations ignoring the grandfather list.
Use --over N to report every file above N lines, replacing the per-type
defaults — this is also the machine-readable way to query the band
(--over 450 --json). --over is a raw size query and always ignores the
grandfather list, which only ever excuses violations of the real limits.
Use --json for machine-readable output (a bare array of violations).

```
gg audit file-size [flags]
```

### Options

```
  -h, --help          help for file-size
      --json          emit JSON array of violations
      --no-baseline   ignore the grandfather baseline
      --over int      custom line threshold; overrides per-type defaults
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
