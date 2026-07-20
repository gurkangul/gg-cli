## gg backlinks

Show every brain entry that references this task, bug, or decision

### Synopsis

List the entries that link TO a reference — the reverse of the links gg
already stores, plus [[wiki links]] and bare TASK-NNN / BUG-NNN mentions found in
free text.

WHEN TO USE: before changing or closing something, to see what else depends on
it — "what decisions reference TASK-042?", "what tasks were spawned by BUG-084?".

<ref> may be a TASK-NNN, a BUG-NNN, a record uuid, or an exact title.

Reads the JSONL ledger directly, so it works with the vector store and the code
graph both offline.

See also: gg impact (code + task blast radius), gg search (find by meaning)

```
gg backlinks <ref> [flags]
```

### Options

```
      --compact    one line per link — preserves agent context window
  -h, --help       help for backlinks
      --outgoing   also list what this entry links OUT to
      --unlinked   also list entries whose prose names this entry without linking it
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
