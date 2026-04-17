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

* [gg brain](gg_brain.md)	 - Portable brain snapshot (export / import / status)
* [gg bug](gg_bug.md)	 - Manage bug lifecycle
* [gg check](gg_check.md)	 - Pre-push health check — open tasks, unresolved discussions
* [gg context](gg_context.md)	 - Fetch a unified context bundle for a topic
* [gg decide](gg_decide.md)	 - Record a decision or rejection (deprecated: use gg record)
* [gg discuss](gg_discuss.md)	 - Manage open discussions (deprecated — use gg message or gg record)
* [gg doctor](gg_doctor.md)	 - Diagnose and repair gg configuration
* [gg export](gg_export.md)	 - Export all project data to a portable bundle
* [gg impact](gg_impact.md)	 - Show downstream impact of changing a source file
* [gg import](gg_import.md)	 - Import a project bundle exported by 'gg export'
* [gg inbox](gg_inbox.md)	 - Read unread messages
* [gg index](gg_index.md)	 - Index the codebase into the Memgraph knowledge graph
* [gg init](gg_init.md)	 - Initialize shared gg infrastructure (~/.gg/) and register this project
* [gg note](gg_note.md)	 - Record a free-form note (deprecated — use gg record or commit message)
* [gg record](gg_record.md)	 - Record that a decision was made (the canonical knowledge-capture verb)
* [gg reembed](gg_reembed.md)	 - Migrate all Qdrant collections to the currently configured embedding model
* [gg reject](gg_reject.md)	 - Record a rejected approach (deprecated: use gg record --decision-status=rejected)
* [gg search](gg_search.md)	 - Find relevant context — semantic search across decisions, tasks, and messages
* [gg session-start](gg_session-start.md)	 - Print session bootstrap briefing (called by agent SessionStart hooks)
* [gg status](gg_status.md)	 - Orient yourself — show open tasks, pending messages, and recent decisions
* [gg task](gg_task.md)	 - Manage tasks
* [gg telemetry](gg_telemetry.md)	 - Manage local usage telemetry
* [gg tell](gg_tell.md)	 - Send a message to another agent role
* [gg trace](gg_trace.md)	 - Inspect GG_TRACE span data
* [gg wave](gg_wave.md)	 - Manage wave/milestone calendar buckets (Memgraph only)

