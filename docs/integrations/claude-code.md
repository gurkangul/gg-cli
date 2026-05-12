# gg Integration: Claude Code

This document explains how to inject `gg` agent rules into a Claude Code project so that Claude automatically calls `gg` during sessions.

## Rules file

Claude Code reads project-level rules from `CLAUDE.md` (project root) or `~/.claude/CLAUDE.md` (global).

Place the inject snippet in your project's `CLAUDE.md` or at the top of an existing file.

## Inject snippet

Add the following block to `CLAUDE.md`:

```markdown
## gg — Shared Agent Knowledge Base

This project uses `gg` as a shared knowledge base CLI.
All decisions, tasks, messages, and rejected approaches are recorded via `gg`.

### Rules

1. **Session start** — always run `gg status` first and summarize open tasks,
   unread inbox, and recent decisions for the user.

2. **During discussion** — before proposing any approach, run
   `gg search "<topic>"` to check for existing decisions or rejections.

3. **Decision** — when the user reaches a decision (explicit or implicit),
   immediately run:
   ```sh
   gg record "decision text" --reason "why" --tags "..."
   ```

4. **Rejected approach** — when an approach is considered but not chosen:
   ```sh
   gg record "approach" --decision-status=rejected --reason "why not"
   ```

5. **Task** — when a unit of work is clearly needed:
   ```sh
   gg task create "title" --detail "..." --priority high
   ```

6. **Messaging** — to hand off work to another agent role:
   ```sh
   gg tell "<role>" "message" --from claude-code
   ```

See [AGENTS.md](./AGENTS.md) for the full protocol.
```

## Verification

After injecting, start a new Claude Code session and confirm the rule is active:

1. Claude should open with a `gg status` call (check the tool call log).
2. Run a test decision:
   ```sh
   gg search "test"
   ```
   You should see the call appear in `gg inbox` from the agent's perspective.
3. Confirm agent-initiated calls show in status:
   ```sh
   gg status
   ```

## Version update

When `gg` adds new commands or changes the protocol, update `CLAUDE.md` to match the new `AGENTS.md`. Check `gg --version` to confirm the CLI matches the rules version documented here.

```sh
gg --version
# gg version 0.1.0
```

To get the latest CLI and managed project files:
```sh
gg update check
gg update
```
