## gg task ready-for-live

Mark a task as ready for live verification — transitions in_progress → ready_for_live

### Synopsis

Record that an implementation is complete and ready for an independent live-verifier
to run against the live environment. Writes the actor (from --from or $GG_ROLE) alongside
the timestamp so 'gg task done --verifier <role>' can enforce same-actor-cannot-verify
when .gg/config.yaml has tasks.verifier_separation: true.

WHEN TO USE: you have finished implementing and local tests (unit + integration) are green,
but production-shaped verification (live e2e / make e2e-cold / manual smoke) has not yet
been run by an independent role. The verify plan should be one sentence describing what
the live-verifier is expected to exercise.

The plan is stored on the task and surfaced by 'gg task get'.

See also: gg task done (close after verifier sign-off).

```
gg task ready-for-live TASK-ID "verify plan" [flags]
```

### Options

```
      --from string   role performing the transition (defaults to $GG_ROLE / $GG_AGENT)
  -h, --help          help for ready-for-live
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
