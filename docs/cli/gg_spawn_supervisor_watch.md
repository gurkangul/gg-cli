## gg spawn supervisor watch

Watch inbox and trigger matching worker panes

```
gg spawn supervisor watch [flags]
```

### Options

```
  -h, --help           help for watch
      --open-missing   when pane missing, open a new worker pane if task is specified
      --poll int       seconds between inbox polls (default 2)
      --role string    worker role to consume (default: $GG_ROLE)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg spawn supervisor](gg_spawn_supervisor.md)	 - Route gg messages to live worker panes

