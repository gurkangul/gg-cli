# gg Onboarding — 10-minute walkthrough

This guide takes you from zero to a live gg brain in ~10 minutes.

## Prerequisites

Install `gg`:

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@latest
```

If the first tagged release is not available yet, install from the current
main branch instead:

```sh
go install github.com/gurkangul/gg-cli/cmd/gg@main
```

First project setup:

```sh
# Run from your project root
gg init

# Verify everything is connected
gg doctor

# Install agent hooks
gg doctor --install-agent-hooks

# Start an agent session briefing
GG_AGENT=codex GG_ROLE=implementer gg session-start --agent=codex --role=implementer
```

`gg init` starts the local Docker services through gg's shared compose file.
Memgraph-backed code indexing is optional; the decision/task/search memory
works without running `gg index`.

---

## Option A: Start from scratch (your own project)

### Step 1 — Record your first decision

```sh
gg record "use PostgreSQL for the primary datastore" \
  --reason "ACID compliance, team familiarity" \
  --tags "database,architecture"
```

### Step 2 — Create a task

```sh
gg task create "set up schema migrations" \
  --priority high \
  --detail "use golang-migrate" \
  --requester user
```

### Step 3 — Check status

```sh
gg status
```

You'll see your active tasks grouped by priority, plus recent decisions.

### Step 4 — Search context

```sh
gg search "database"
```

Returns semantically similar decisions, tasks, and notes.

---

## Option B: Load demo data (instant populated brain)

If you want to explore with pre-existing data, import the demo project snapshot:

```sh
# From the gg-cli repo root:
mkdir -p .gg/brain
cp -R examples/demo-project/. .gg/brain/
gg brain import --skip-embed-check
```

This loads 8 decisions, 7 tasks, and 3 messages from a fictional "todo-api" project.
No real embedding vectors are restored (the demo has none), but all metadata is
searchable via `gg status`, `gg task list`, and `gg search`.

After import:

```sh
gg task list
# ○ demo-task-0003 [high] CRUD endpoints: /todos   ← in_progress
# ○ demo-task-0004 [high] Rate limiter middleware   ← pending
# ...

gg search "authentication"
# → returns JWT decision + auth middleware task

gg status
```

---

## Multi-agent setup

Set `GG_AGENT` and `GG_ROLE` so durable records are attributed to the right agent:

```sh
export GG_AGENT=codex
export GG_ROLE=developer   # or: architect, reviewer, agent
```

Send a message to another role:

```sh
gg tell architect "Rate limiter is unblocked — proceeding with in-memory backend" \
  --from developer
```

gg does not replace the agent's native workflow. Use `gg record`, `gg task`,
`gg bug`, and `gg tell` when decisions, rejected approaches, shared work items,
root causes, evidence summaries, blockers, or handoffs need to survive the
current session.

---

## Key commands reference

| Command | What it does |
|---------|-------------|
| `gg record "..."` | Record a decision with reason + tags |
| `gg task create "..." --requester user` | Create a task |
| `gg task list` | List tasks (filtered by priority/status) |
| `gg task done TASK-ID "summary"` | Mark task done (runs `.gg/hooks/pre-task-done.d/*.sh` first — exit 7 if a hook rejects) |
| `gg doctor --install-task-hooks` | Install verify-gate starter hooks (Go / Node / Bun auto-detect) |
| `gg search "..."` | Semantic search across all brain records |
| `gg status` | Project status (tasks + recent decisions) |
| `gg status render` | Write STATUS.md from live brain state |
| `gg brain export` | Export brain to `.gg/brain/` JSONL snapshot |
| `gg brain import <dir>` | Restore from snapshot |
| `gg doctor` | Check connectivity + diagnose issues |

Full reference: [docs/commands.md](commands.md)

---

## Feedback capture

After your first session, answer these and share with us:

```
# GG-FEEDBACK v1
date: YYYY-MM-DD
role: developer|architect|reviewer|other
project_type: go|python|swift|typescript|other
session_duration_min: N

## What worked
- 

## What was confusing
- 

## First command you ran (after init)
gg ...

## Command you wished existed
gg ...

## Surprise moments (good or bad)
- 

## Would you use this again? (yes/no/maybe)

## Net promoter score (0–10, would you recommend to a colleague)
```

File feedback as a GitHub issue with label `onboarding-feedback`, or send to the team Slack `#gg-feedback` channel.
