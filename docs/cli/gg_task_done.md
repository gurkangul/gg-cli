## gg task done

Mark a task done — include a one-sentence summary of what was accomplished

### Synopsis

Close a task and record a completion summary in the shared brain.

WHEN TO USE: you have finished the work described in the task. The summary is stored
and surfaced in 'gg status' and 'gg search' — write it for the next agent that reads it.

VERIFY GATE: before writing the new state, gg runs every executable *.sh in
.gg/hooks/pre-task-done.d/ in lexicographic order. Any non-zero exit means a
required durable evidence check failed; the transition aborts with exit code 7
(ExitVerifyFailed), the task stays in its current state, and a machine-parseable
{"event":"verify_failed",...} line is emitted to stderr along with an internal
'gg tell' to all agents. Install starter scripts with 'gg doctor --install-task-hooks'
(auto-detects Go and Node/Bun).

READY-FOR-LIVE GATE (opt-in): when .gg/config.yaml has
tasks.require_ready_for_live: true, closure requires a stored ready-for-live
handoff/evidence packet (record it with 'gg task ready-for-live' after local
checks pass). Combined with tasks.verifier_separation: true the command also
requires --verifier <role> and rejects when the verifier is the same actor that
recorded the ready-for-live handoff. This protects reviewability by ensuring a
future agent can see both implementer evidence and independent verifier evidence.

COMPACT HYDRATION GATE: tagged agent sessions (GG_AGENT or GG_ROLE set) require
a recent 'gg task get TASK-ID' hydration proof before 'gg task done'. Compact
list/search rows are scan/index views only; the targeted full task read writes a
local proof that reviewers had access to acceptance criteria and prior context.

See also: gg task review (request peer review), gg record (capture design decisions made during the work)

```
gg task done TASK-ID "summary" [flags]
```

### Options

```
  -h, --help              help for done
      --verifier string   actor role that verified the live run (required when tasks.verifier_separation is true)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
