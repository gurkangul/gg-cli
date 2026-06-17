## gg export

Export all project data to a portable bundle

### Synopsis

Export all project memory (decisions, tasks, notes, discussions, messages,
rejections, bugs) into a gzip-compressed JSON bundle.

The bundle includes embedding vectors so that 'gg import' can restore the
project without re-embedding on the target machine.

Note: code graph data (gg index) is not included in the bundle.
Run 'gg index' after import to rebuild the code graph.

Examples:
  gg export                         # writes gg-export-<date>.json.gz
  gg export my-project.json.gz      # writes to the given path

```
gg export [output.json.gz] [flags]
```

### Options

```
  -h, --help   help for export
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
