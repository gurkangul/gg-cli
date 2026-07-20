## gg

Shared brain for AI agents

### Synopsis

GG — One brain, any agent. A shared knowledge base CLI for AI agents.

### Options

```
  -h, --help   help for gg
      --json   output results as JSON
```

### SEE ALSO

* [gg audit](gg_audit.md)	 - [experimental] Session mutation audit (called by PostToolUse and Stop hooks)
* [gg backlinks](gg_backlinks.md)	 - Show every brain entry that references this task, bug, or decision
* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
* [gg bug](gg_bug.md)	 - Manage bug lifecycle
* [gg canon](gg_canon.md)	 - Distilled institutional memory — the durable knowledge every agent should start with
* [gg check](gg_check.md)	 - Pre-push health check — open tasks, unresolved discussions
* [gg config](gg_config.md)	 - Inspect or modify project configuration
* [gg context](gg_context.md)	 - Fetch a unified context bundle for the project, a topic, or a task
* [gg decisions](gg_decisions.md)	 - List or search decisions
* [gg def](gg_def.md)	 - Find where a symbol is defined (code graph, offline)
* [gg doctor](gg_doctor.md)	 - Diagnose and repair gg configuration
* [gg export](gg_export.md)	 - Export all project data to a portable bundle
* [gg graph](gg_graph.md)	 - Work with the local code graph
* [gg gsd](gg_gsd.md)	 - [experimental] GSD integration utilities
* [gg impact](gg_impact.md)	 - Show downstream impact of changing a file, or blast radius of a bug or task
* [gg import](gg_import.md)	 - Import a project bundle exported by 'gg export'
* [gg inbox](gg_inbox.md)	 - Read unread messages
* [gg index](gg_index.md)	 - Index the codebase into the embedded code graph (.gg/graph.db)
* [gg init](gg_init.md)	 - Initialize shared gg infrastructure (~/.gg/) and register this project
* [gg lsp](gg_lsp.md)	 - Live, type-aware code intelligence via a language server
* [gg mcp](gg_mcp.md)	 - Model Context Protocol server — expose the project brain to MCP clients
* [gg metrics](gg_metrics.md)	 - [experimental] Project health metrics
* [gg next](gg_next.md)	 - Recommend the next safe agent command
* [gg onboard](gg_onboard.md)	 - Print what a new agent inherits — the distilled project briefing + how to work here
* [gg reconcile](gg_reconcile.md)	 - [experimental] Reconcile append-only task events with the live task projection
* [gg record](gg_record.md)	 - Record that a decision was made (the canonical knowledge-capture verb)
* [gg reembed](gg_reembed.md)	 - Rebuild the embedded vector index (.gg/vectorstore.db) from .gg/brain/*.jsonl
* [gg related](gg_related.md)	 - Walk the link graph outward from a task, bug, or decision
* [gg search](gg_search.md)	 - Find relevant context — semantic search across decisions, tasks, and messages
* [gg serve](gg_serve.md)	 - Local dashboard — visualize every gg project's brain (decisions, work, live search)
* [gg session-start](gg_session-start.md)	 - Print session bootstrap briefing (called by agent SessionStart hooks)
* [gg status](gg_status.md)	 - Orient yourself — show open tasks, pending messages, and recent decisions
* [gg system](gg_system.md)	 - Host-level gg operations (cross-project registry + sync)
* [gg task](gg_task.md)	 - Manage tasks
* [gg telemetry](gg_telemetry.md)	 - [experimental] Manage local usage telemetry
* [gg tell](gg_tell.md)	 - Send a message to one or more agent roles
* [gg trace](gg_trace.md)	 - [experimental] Inspect GG_TRACE span data
* [gg update](gg_update.md)	 - Update gg to the latest public release
* [gg uses](gg_uses.md)	 - Find which files use (reference) a symbol — symbol-exact reverse blast-radius
* [gg verify](gg_verify.md)	 - Write-boundary verification for a source file
* [gg watch](gg_watch.md)	 - Tail inbox messages and event stream
* [gg wave](gg_wave.md)	 - Manage optional wave/milestone buckets — sprints (code graph only)
