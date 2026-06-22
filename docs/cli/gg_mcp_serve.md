## gg mcp serve

Serve the project brain over MCP (JSON-RPC 2.0 on stdio)

### Synopsis

Serve the project brain over a stdio JSON-RPC 2.0 transport.

The project is resolved from the current working directory (walk-up to .gg).
Use --project <path> to point a global-config client at a specific project.

Diagnostics go to stderr; stdout carries only JSON-RPC responses.

```
gg mcp serve [flags]
```

### Options

```
  -h, --help             help for serve
      --project string   project directory to serve (overrides CWD-based resolution)
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg mcp](gg_mcp.md)	 - Model Context Protocol server — expose the project brain to MCP clients
