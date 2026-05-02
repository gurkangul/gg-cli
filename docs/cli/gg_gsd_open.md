## gg gsd open

Open interactive GSD in a new terminal pane

### Synopsis

Open interactive GSD in a new terminal pane rooted at the current gg project.

This is the stable pane-launch path for GSD. It starts the interactive TUI;
headless commands such as 'gsd headless query' only report state and do not
open a tab or pane.

```
gg gsd open [flags]
```

### Options

```
      --agent string   command to run in the pane (default: $GG_SPAWN_AGENT or 'gsd')
  -h, --help           help for open
      --split string   pane split direction: horizontal (below) or vertical (right, default) (default "vertical")
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg gsd](gg_gsd.md)	 - GSD integration utilities

