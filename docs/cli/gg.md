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

* [gg audit](gg_audit.md)	 - Session mutation audit (called by PostToolUse and Stop hooks)
* [gg become](gg_become.md)	 - Adopt a project role (e.g. become master)
* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
* [gg bug](gg_bug.md)	 - Manage bug lifecycle
* [gg check](gg_check.md)	 - Pre-push health check — open tasks, unresolved discussions
* [gg config](gg_config.md)	 - Inspect or modify project configuration
* [gg context](gg_context.md)	 - Fetch a unified context bundle for a topic or task
* [gg dashboard](gg_dashboard.md)	 - Live worker-pane dashboard (TASK-276 AC3)
* [gg decide](gg_decide.md)	 - Record a decision or rejection (deprecated: use gg record)
* [gg doctor](gg_doctor.md)	 - Diagnose and repair gg configuration
* [gg export](gg_export.md)	 - Export all project data to a portable bundle
* [gg gsd](gg_gsd.md)	 - GSD integration utilities
* [gg impact](gg_impact.md)	 - Show downstream impact of changing a file, or blast radius of a bug or task
* [gg import](gg_import.md)	 - Import a project bundle exported by 'gg export'
* [gg inbox](gg_inbox.md)	 - Read unread messages
* [gg index](gg_index.md)	 - Index the codebase into the Memgraph knowledge graph
* [gg init](gg_init.md)	 - Initialize shared gg infrastructure (~/.gg/) and register this project
* [gg master](gg_master.md)	 - Master-session utilities: session handoff, resume state
* [gg metrics](gg_metrics.md)	 - Project health metrics
* [gg record](gg_record.md)	 - Record that a decision was made (the canonical knowledge-capture verb)
* [gg reembed](gg_reembed.md)	 - Migrate all Qdrant collections to the currently configured embedding model
* [gg reject](gg_reject.md)	 - Record a rejected approach (deprecated: use gg record --decision-status=rejected)
* [gg search](gg_search.md)	 - Find relevant context — semantic search across decisions, tasks, and messages
* [gg session-start](gg_session-start.md)	 - Print session bootstrap briefing (called by agent SessionStart hooks)
* [gg spawn](gg_spawn.md)	 - Multi-agent orchestration: spawn worker panes, run queue, track liveness
* [gg status](gg_status.md)	 - Orient yourself — show open tasks, pending messages, and recent decisions
* [gg system](gg_system.md)	 - Host-level gg operations (cross-project registry + sync)
* [gg task](gg_task.md)	 - Manage tasks
* [gg telemetry](gg_telemetry.md)	 - Manage local usage telemetry
* [gg tell](gg_tell.md)	 - Send a message to one or more agent roles
* [gg trace](gg_trace.md)	 - Inspect GG_TRACE span data
* [gg update](gg_update.md)	 - Update gg to the latest public release
* [gg verify](gg_verify.md)	 - Write-boundary verification for a source file
* [gg watch](gg_watch.md)	 - Tail inbox messages and event stream
* [gg wave](gg_wave.md)	 - Manage wave/milestone calendar buckets (Memgraph only)
