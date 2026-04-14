# gg Integration: Aider

This document explains how to inject `gg` agent rules into Aider so that the assistant automatically calls `gg` during sessions.

## Rules file

Aider reads a system prompt addition from `.aider.conf.yml` or via the `--system-prompt` flag. The recommended approach is a project-level conventions file referenced in `.aider.conf.yml`.

## Inject snippet

**Step 1.** Create `.aider/gg-rules.md` in your project root:

```markdown
## gg — Shared Agent Knowledge Base

This project uses `gg` for shared agent memory.
All decisions, tasks, messages, and rejected approaches are recorded via `gg`.
The full protocol is in `AGENTS.md` at the project root.

### Quick rules

- **Session start**: always run `gg status` first.
- **Before proposing an approach**: run `gg search "<topic>"`.
- **On decision**: `gg record "text" --reason "why" --tags "..."`
- **On rejected approach**: `gg record --stance=reject "approach" --reason "why not"`
- **On new task**: `gg task create "title" --priority high`
- **Set role**: `export GG_ROLE=aider`
```

**Step 2.** Reference it in `.aider.conf.yml`:

```yaml
# .aider.conf.yml
read:
  - .aider/gg-rules.md
  - AGENTS.md
```

Or pass it at startup:
```sh
aider --read .aider/gg-rules.md --read AGENTS.md
```

## Verification

After injecting, start an Aider session:
```sh
aider
```

Ask the assistant: *"What should you do at the start of this session?"*

It should respond by running `gg status`. Confirm from the shell:
```sh
gg status
# Tasks, messages, and decisions should reflect any agent activity
```

## Version update

The canonical rules are in `AGENTS.md`. To update after a `gg` upgrade:

```sh
go install github.com/gurkangul/gg@latest
gg init  # regenerates AGENTS.md
```

Keep `.aider/gg-rules.md` as a short pointer; put the full protocol in `AGENTS.md` so there is one source of truth.
