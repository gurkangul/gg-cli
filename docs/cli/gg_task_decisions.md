## gg task decisions

List decisions explicitly linked to a task

### Synopsis

List decisions whose --task flag named this task exactly.

This is a structural lookup, not a semantic search — it returns only decisions
where Decision.TaskID == <TASK-ID>. Use it to prove that a task's design
choices were captured with gg record --task <TASK-ID>.

Consumed by the decision-capture gate (hooks/pre-task-done.d/20-decide-capture.sh).

```
gg task decisions TASK-ID [flags]
```

### Options

```
  -h, --help   help for decisions
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

