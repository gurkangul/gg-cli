## gg task done

Mark a task done — include a one-sentence summary of what was accomplished

### Synopsis

Close a task and record a completion summary in the shared brain.

WHEN TO USE: you have finished the work described in the task. The summary is stored
and surfaced in 'gg status' and 'gg search' — write it for the next agent that reads it.

VERIFY GATE: before writing the new state, gg runs every executable *.sh in
.gg/hooks/pre-task-done.d/ in lexicographic order. Any non-zero exit aborts
the transition with exit code 7 (ExitVerifyFailed); the task stays in its
current state and a machine-parseable {"event":"verify_failed",...} line is
emitted to stderr along with an internal 'gg tell' to all agents.
Install starter scripts with 'gg doctor --install-task-hooks' (auto-detects
Go and Node/Bun).

READY-FOR-LIVE GATE (opt-in): when .gg/config.yaml has
tasks.require_ready_for_live: true, this command refuses unless the task is
already in status "ready_for_live" (transition it with 'gg task ready-for-live'
after local checks pass). Combined with tasks.verifier_separation: true the
command also requires --verifier <role> and rejects when the verifier is the
same actor that performed the ready-for-live transition. Prevents the
premature-closure / same-actor-verification pattern surfaced by the
dogfood audit 2026-04-19.

COMPACT HYDRATION GATE: tagged agent sessions (GG_AGENT or GG_ROLE set) must run
'gg task get TASK-ID' shortly before 'gg task done'. Compact list/search rows are
scan/index views only; the targeted full task read writes a local hydration proof
so agents cannot close work from omitted detail.

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

