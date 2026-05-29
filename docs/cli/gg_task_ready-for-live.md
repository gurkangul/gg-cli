## gg task ready-for-live

Record implementation evidence for independent live verification

### Synopsis

Record the durable handoff that tells a reviewer or future agent what evidence exists
and what still needs independent live verification. Writes the actor (from --from or $GG_ROLE)
alongside the timestamp so 'gg task done --verifier <role>' can require a separate verifier
when .gg/config.yaml has tasks.verifier_separation: true.

WHEN TO USE: you have finished implementing and local tests (unit + integration) are green,
but production-shaped verification (live e2e / make e2e-cold / manual smoke) has not yet
been run by an independent role. The verify plan should be one sentence describing what
the live-verifier is expected to exercise plus a compact evidence summary: commands run,
live smoke result, impacted files checked with gg impact, known gaps, and artifact paths.

The plan is stored on the task and surfaced by 'gg task get'. If a task is already
ready_for_live, running this command again updates that stored plan without
changing state; use either the positional plan or --plan.

Example:
  gg task ready-for-live TASK-123 --from "$GG_ROLE" --plan "Reviewer: inspect diff and rerun smoke. Evidence: commands=go test ./... -count=1; live=CLI smoke passed; impact=cmd/foo.go checked with gg impact; gaps=none; artifacts=.artifacts/TASK-123-smoke.txt"

See also: gg task done (close after verifier sign-off), gg tell --task (handoff message).

```
gg task ready-for-live TASK-ID ["verify plan"] [flags]
```

### Options

```
      --from string   role performing the transition (defaults to $GG_ROLE / $GG_AGENT)
  -h, --help          help for ready-for-live
      --plan string   verify plan/evidence summary to store on the task (alternative to positional plan)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
