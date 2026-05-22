## gg task review

Set review status on a task (approve or reject)

### Synopsis

Update the review_status of a task without changing its lifecycle status.

Review status is orthogonal to task status: a done task can be approved or
rejected by a reviewer without re-opening it.

  gg task review TASK-042 --approve
  gg task review TASK-042 --approve --notes "LGTM, minor nit in line 47"
  gg task review TASK-042 --reject  --notes "Breaks isolation contract"

```
gg task review TASK-ID [flags]
```

### Options

```
      --approve        approve the task
      --by string      reviewer role or name (defaults to GG_ROLE or 'reviewer')
  -h, --help           help for review
      --notes string   reviewer notes
      --reject         reject the task
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
