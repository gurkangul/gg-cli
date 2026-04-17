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

See also: gg task review (request peer review), gg record (capture design decisions made during the work)

```
gg task done TASK-ID "summary" [flags]
```

### Options

```
  -h, --help   help for done
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

