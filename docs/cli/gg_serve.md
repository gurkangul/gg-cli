## gg serve

Local dashboard — visualize every gg project's brain (decisions, work, live search)

### Synopsis

gg serve starts a FOREGROUND, localhost-only web dashboard.

Unlike the other commands, it is path-independent: run it from anywhere and it
lists every gg project registered on this host (~/.gg/projects.json) and lets you
switch between them — each project's brain stays fully isolated (no merging). Run
inside a project and that project is selected by default.

It is NOT a daemon: it runs only until you press Ctrl-C, binds to 127.0.0.1
exclusively (no network exposure), and serves the same JSONL and embedded-SQLite
stores the CLI reads.

  gg serve                # launcher for all projects at http://127.0.0.1:7777
  gg serve --port 8080    # pick another port
  gg serve --no-open      # do not open the browser automatically

```
gg serve [flags]
```

### Options

```
  -h, --help       help for serve
      --no-open    do not open the browser automatically
      --port int   localhost port to bind (default 7777)
      --write      enable write actions (record decision / create task) via POST — off by default
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
