## gg wave

Manage optional wave/milestone buckets — sprints (code graph only)

### Synopsis

Wave nodes group tasks into time-bounded sprints or milestones.
Waves are OPTIONAL: projects that don't do time-based planning can ignore this
command entirely — nothing else depends on waves existing.
They live in the embedded code graph (.gg/graph.db) only — no vector collection is created.

  gg wave add wave1 --name "Phase 1" --goal "Ship MVP" --start 2026-01-01 --end 2026-03-31
  gg wave list
  gg wave list --active
  gg wave status wave1
  gg wave assign TASK-042 --wave wave1

### Options

```
  -h, --help   help for wave
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg wave add](gg_wave_add.md)	 - Create or update a wave node
* [gg wave assign](gg_wave_assign.md)	 - Assign a task to a wave
* [gg wave list](gg_wave_list.md)	 - List waves
* [gg wave migrate-tags](gg_wave_migrate-tags.md)	 - Dry-run tag-to-wave migration (--apply to execute)
* [gg wave status](gg_wave_status.md)	 - Show wave details and assigned tasks
