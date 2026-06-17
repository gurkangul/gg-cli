## gg canon

Distilled institutional memory — the durable knowledge every agent should start with

### Synopsis

gg canon is the agent-distilled layer on top of the raw decision/bug ledger.

A new agent reads the canon and inherits the senior-dev's distilled knowledge,
instead of re-deriving it from hundreds of individual records. The canon is
injected at session-start (like RULES), stored JSONL-first (survives any rebuild),
and produced by an agent — gg has no cloud LLM (no-network), so distillation is
agent-driven:

  gg canon gather                       # dump the raw material (active decisions,
                                        #   rejections, fixed-bug root causes)
  # ...agent distills it into durable per-area knowledge...
  gg canon set architecture "JSONL is source of truth; the vector store is a derived index…"
  gg canon show                         # what every new agent now starts with

### Options

```
  -h, --help   help for canon
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg canon gather](gg_canon_gather.md)	 - Dump active decisions + rejections + fixed-bug root causes as raw material to distill
* [gg canon set](gg_canon_set.md)	 - Write/overwrite the canon for an area (e.g. architecture, auth, gotchas)
* [gg canon show](gg_canon_show.md)	 - Show the project canon (the distilled knowledge injected at session-start)
