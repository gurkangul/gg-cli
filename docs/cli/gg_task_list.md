## gg task list

List tasks

```
gg task list [flags]
```

### Options

```
      --blockers        show tasks that are blocking other tasks (have --blocks targets)
      --compact         one line per task — drops author + block-reason detail to preserve agent context window
  -h, --help            help for list
      --needs-review    show done tasks awaiting review (review_status=none or pending)
      --pending-ack     show in-progress tasks whose worker ACK is waiting for ACK-OK or ACK-FIX
      --ready           show only pending tasks whose dependencies are all done
      --status string   filter by status: pending, in_progress, done, blocked
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks

