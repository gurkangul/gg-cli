## gg master resume

Print a structured session-handoff dump for a fresh master session

### Synopsis

Produce a one-shot snapshot of all gg state a fresh Opus master session needs
to resume without asking the user to re-explain. Runs the 7-source pipeline from
the master-resume protocol documented in CLAUDE.md:

  1. git log --oneline -10                     (recent commits)
  2. spawn state: heartbeat + queue + panes    (local, no Qdrant)
  3. pending tasks (up to 20)                  (Qdrant)
  4. ready_for_live tasks                      (Qdrant)
  5. unread inbox (--include-agents)           (Qdrant, peek — no mark-as-read)
  6. recent decisions (compact)                (Qdrant)
  7. panes.json raw                            (local)

Outputs plain text sections. Qdrant down → local sections still print.
Combine with --json for machine-readable output.

```
gg master resume [flags]
```

### Options

```
  -h, --help   help for resume
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg master](gg_master.md)	 - Master-session utilities: session handoff, resume state

