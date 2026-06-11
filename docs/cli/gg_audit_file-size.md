## gg audit file-size

List source files violating the 500-line (800 for tests) size rule

### Synopsis

Walk the project and report files that exceed the size limit.
Source files (.go/.ts/.js/.py/.rs/.java) must stay under 500 lines;
test files (*_test.go, *.test.*, *.spec.*) under 800 lines.

Files in the .gg/file-size-baseline.json grandfather list are only
flagged when they have grown beyond their baseline value.

Use --no-baseline to see raw violations ignoring the grandfather list.
Use --over N to use a custom threshold instead of the per-type defaults.
Use --json for machine-readable output.

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
