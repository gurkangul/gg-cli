## gg note

Record a free-form note (deprecated — use gg record or commit message)

### Synopsis

Stores a timestamped note in the activity log.

DEPRECATED: usage data shows note is underused (1 call in dogfood).
  - For decisions and rationale: use 'gg record "text" --reason "why"'
  - For progress context: use a git commit message or 'gg task done TASK-X "summary"'
  - For search: 'gg search' covers decisions, tasks, and messages

This command will be removed in a future release (v0.2+).
TODO(v0.2): remove gg note

```
gg note "text" [flags]
```

### Options

```
  -h, --help          help for note
      --tags string   comma-separated tags
      --task string   link note to a task (e.g. TASK-042)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg note list](gg_note_list.md)	 - List recent notes
* [gg note search](gg_note_search.md)	 - Semantic search over notes

