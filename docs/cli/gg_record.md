## gg record

Record that a decision was made (the canonical knowledge-capture verb)

### Synopsis

Record an architectural decision, rejected approach, or design choice.

WHEN TO USE: you've concluded something — chosen a library, rejected an approach,
established a constraint. Anything that would appear in an ADR belongs here.

WHEN NOT TO USE: for in-progress deliberation use 'gg tell <to> <msg> --from <role>'; for task
tracking use 'gg task create'. For handoff/evidence summaries, use 'gg tell --task' or
'gg task ready-for-live --plan'; for closure summaries, use 'gg task done'.

Examples:
  gg record "use JWT for auth" --reason "stateless, scales horizontally" --tags "auth,security"
  gg record "do NOT use Redis sessions" --decision-status=rejected --reason "ops burden"
  gg record "switch to PostgreSQL" --rejected-alternatives "MySQL,SQLite" --implements TASK-003

See also: gg task (track work), gg search (find context), gg status (overview)

```
gg record "text" [flags]
```

### Options

```
      --decision-status string         decision lifecycle status: active, superseded, rejected (default "active")
      --evidence string                how this was verified (commands run, live smoke, source ref) — empty surfaces as [unverified]
      --from string                    author/role recording this (defaults to $GG_ROLE)
  -h, --help                           help for record
      --implements string              TASK-X that implements this decision (writes code-graph edge)
      --pin                            pin this decision so it surfaces first in gg context overview regardless of age (for canon-grade, must-not-be-buried decisions)
      --reason string                  why this decision was made (or rejected)
      --rejected-alternatives string   comma-separated approaches that were considered and rejected
      --rejects string                 decision UUID superseded by this one (writes code-graph edge)
      --stance string                  stance: "accept" (decision) or "reject" (rejection) (default "accept")
      --tags string                    comma-separated tags
      --task string                    related task ID
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
