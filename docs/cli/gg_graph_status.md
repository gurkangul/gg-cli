## gg graph status

One-line typed CodeGraph freshness an agent can read

### Synopsis

Print the CodeGraph freshness as a single typed line:

  codegraph: fresh|stale(reason)|empty|... | idx=<sha> head=<sha> | files=N sym=M edges=K

This is the agent-facing readiness signal for the local code graph. It reuses the
same freshness contract as 'gg index status' (no duplicate logic); use --compact
for the dense one-line form and the default render for a fuller breakdown.

gg never refreshes the graph in the background — when the line reports stale or
empty, repair is explicit (gg doctor --fix-index or gg index --lang <lang>).

```
gg graph status [flags]
```

### Options

```
      --compact   print the dense one-line typed freshness signal
  -h, --help      help for status
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg graph](gg_graph.md)	 - Work with the local code graph
