## gg mcp

Model Context Protocol server — expose the project brain to MCP clients

### Synopsis

gg mcp runs a hand-rolled, READ-ONLY Model Context Protocol server.

An MCP client (Claude Desktop, an IDE, another agent) spawns 'gg mcp serve' as a
child process and exchanges newline-delimited JSON-RPC 2.0 messages over the
child's stdin/stdout. There is no port and no daemon.

The server exposes only read tools (gg_search, gg_context, gg_impact, gg_canon,
gg_task_get, gg_bug_get). No write tools exist — the brain cannot be mutated
through MCP.

### Options

```
  -h, --help   help for mcp
```

### Options inherited from parent commands

```
      --json   output results as JSON
```

### SEE ALSO

* [gg](gg.md)	 - Shared brain for AI agents
* [gg mcp serve](gg_mcp_serve.md)	 - Serve the project brain over MCP (JSON-RPC 2.0 on stdio)
