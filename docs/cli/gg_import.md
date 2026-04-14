## gg import

Import a project bundle exported by 'gg export'

### Synopsis

Restore a project from a gzip-compressed JSON bundle.

By default the project ID from the bundle is preserved, meaning the data is
restored into the same logical project. Use --as to import under a new project
ID (useful when migrating to a new machine with a fresh config).

Note: Memgraph graph data is not included in bundles. Run 'gg index' after
import to rebuild the code intelligence graph.

Examples:
  gg import gg-export-2026-01-01.json.gz
  gg import gg-export-2026-01-01.json.gz --as new-project-uuid

```
gg import <bundle.json.gz> [flags]
```

### Options

```
      --as string   import under a different project ID (UUID)
  -h, --help        help for import
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents

