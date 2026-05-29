# gg Integration: OpenAI Codex / Codex CLI

This document explains how to remind Codex CLI sessions to use gg as shared
durable memory. Codex keeps its native workflow; gg stores the decisions,
rejections, bugs, evidence, and handoffs future agents need. For the compact
Codex capture map, see [Native Workflow Capture Points](../native-workflow-capture.md#codex).

## Rules file

Codex CLI reads agent instructions from:

- `~/.codex/instructions.md` — global instructions (all projects)
- `AGENTS.md` in the project root — project-level instructions (auto-detected)

`gg init` already creates `AGENTS.md` at your project root. No additional file is
required.

## Inject snippet

If you need a global snippet (applies to all Codex projects), add the following
to `~/.codex/instructions.md`:

```markdown
## gg — Shared Durable Memory

When a project has `AGENTS.md` that references gg, treat gg as the shared durable
memory and evidence ledger. Use your native Codex workflow, and sync durable
outputs into gg.

1. **Orientation** — run `gg status` or `gg context --compact` and summarize relevant open memory.
2. **Before changing important behavior** — run `gg search "<topic>" --compact`.
3. **Decision** — run `gg record "text" --reason "..." --tags "..."`.
4. **Rejected approach** — run `gg record "approach" --decision-status=rejected --reason "..."`.
5. **Durable work item** — run `gg task create "title" --priority high` only when future agents need to see the work item.
6. **Bug/evidence/handoff** — use `gg bug` for bug/root-cause records. Use `gg tell --task` or `gg task ready-for-live --plan` for compact evidence: commands run, live smoke result, impacted files, known gaps, and artifact paths.
7. Set `GG_AGENT` / `GG_ROLE` so records are attributed correctly.

The full protocol is in `AGENTS.md` at the project root.
```

For project-level use, `gg init` generates `AGENTS.md` with the current protocol
already included.

## Verification

1. Start a Codex CLI session in your project:
   ```sh
   codex
   ```
2. The agent should be able to run `gg status` or `gg context --compact` and
   see shared memory.
3. Confirm from the shell:
   ```sh
   gg status
   ```

## Version update

The canonical source of truth is `AGENTS.md` in the project root. To update
after a gg upgrade:

```sh
gg update check
gg update
```

If you maintain a global `~/.codex/instructions.md` snippet, update it to match
the new `AGENTS.md` protocol.
