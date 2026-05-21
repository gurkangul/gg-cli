## gg task renew

Renew the current owner lease for an in-progress task

```
gg task renew TASK-ID [flags]
```

### Options

```
  -h, --help             help for renew
      --lease duration   new lease duration from now (default 30m0s)
      --owner string     agent renewing the claim (defaults to $GG_AGENT / $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg task](gg_task.md)	 - Manage tasks
