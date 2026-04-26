## gg become master

Install master-role-extras block and record liveness heartbeat

### Synopsis

Opt-in: master discipline (review gate, worker lifecycle, bypass audit) applies only after this command runs.

Opt this session into the master role for the current project.

Two things happen:

  1. The master-role-extras block is installed (or updated) in CLAUDE.md.
     This block contains the master orchestration protocol: worker lifecycle,
     pane management, review responsibilities, resume protocol, and bypass
     discipline. If the block is already current, no change is made.

  2. A heartbeat is written to the project's runtime directory so worker
     sessions know a master is present. Workers read this liveness signal
     via the 46-master-guard hook before closing tasks.

Run this once per master session. Wire 'gg spawn heartbeat' into a loop
(e.g. every 60 s) to maintain liveness across a long session.

After running, open the first worker with:
  gg spawn worker --task TASK-NNN

```
gg become master [flags]
```

### Options

```
      --force-reset   overwrite DRIFTED (malformed) master-role markers
  -h, --help          help for master
      --yes           non-interactive: skip prompts, accept defaults
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg become](gg_become.md)	 - Adopt a project role (e.g. become master)

