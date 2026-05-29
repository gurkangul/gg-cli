# gg Integration: Claude Code

This document explains how to remind Claude Code sessions to use gg as shared
durable memory. Claude Code keeps its native workflow; gg stores the decisions,
rejections, bugs, evidence, and handoffs future agents need. For the compact
Claude Code capture map, see [Native Workflow Capture Points](../native-workflow-capture.md#claude-code).

## Rules file

Claude Code reads project-level rules from `CLAUDE.md` (project root) or
`~/.claude/CLAUDE.md` (global).

Place the inject snippet in your project's `CLAUDE.md` or at the top of an
existing file.

## Inject snippet

Add the following block to `CLAUDE.md`:

```markdown
## gg — Shared Durable Memory

This project uses `gg` as a durable shared memory and evidence ledger.
Use Claude Code normally, and sync durable outputs into gg.

### Rules

1. **Orientation** — run `gg status` or `gg context --compact` and summarize
   relevant open tasks, unread inbox, and recent decisions for the user.

2. **During discussion** — before proposing or changing important behavior, run
   `gg search "<topic>" --compact` to check for existing decisions or rejections.

3. **Decision** — when the user reaches a decision, run:
   ```sh
   gg record "decision text" --reason "why" --tags "..."
   ```

4. **Rejected approach** — when an approach is considered but not chosen:
   ```sh
   gg record "approach" --decision-status=rejected --reason "why not"
   ```

5. **Durable work item** — when a task/story output must be visible to future agents:
   ```sh
   gg task create "title" --detail "..." --priority high
   ```

6. **Bugs, evidence, and handoffs** — use `gg bug` for bug/root-cause records.
   Use `gg tell --task` or `gg task ready-for-live --plan` for handoffs with a
   compact evidence packet: commands run, live smoke result, impacted files,
   known gaps, and artifact paths. Keep bulky artifacts outside gg and record
   only their path/reference.

See the project-root `AGENTS.md` for the full protocol.
```

## Verification

After injecting, start a new Claude Code session and confirm the rule is active:

1. Claude should be able to run `gg status` or `gg context --compact`.
2. Run a test search:
   ```sh
   gg search "test" --compact
   ```
3. Confirm agent-initiated calls show in status:
   ```sh
   gg status
   ```

## Version update

When gg adds new commands or changes the protocol, update `CLAUDE.md` to match
the new `AGENTS.md`. Check `gg --version` to confirm the CLI matches the rules
version documented here.

```sh
gg --version
# gg version 0.1.0
```

To get the latest CLI and managed project files:

```sh
gg update check
gg update
```
