# GG — Original Vision and Phase Plan

> **Historical document.** This is the original design vision written before development began.
> It describes the intended phases and data model. The canonical current state is the README
> and the live codebase — this document may reflect aspirations that were later revised, scoped
> down, or implemented differently.

---

## Vision

AI agents (Claude Code, GSD, BMAD, Codex, etc.) run independently in their own terminals but all
share the same knowledge base. No orchestrator, no daemon, no UI. Just a CLI + Qdrant + Memgraph.

**Tagline:** "One brain, any agent."

---

## Problem

- Each agent works in isolation, unaware of what the others know
- A decision made by one agent is invisible to others
- Rejected approaches get re-proposed from scratch
- Code structure knowledge is rediscovered from zero every session
- No inter-agent communication

## Solution

A single CLI tool (`gg`) + two databases (Qdrant + Memgraph, running locally in Docker):

- **Qdrant** → decisions, tasks, messages, rejections (semantic search)
- **Memgraph** → code structure, file relationships, dependency graph

The same rules are injected into every agent's context file. The agent automatically calls the
`gg` CLI to use the shared brain. The user talks to the agent; the agent runs `gg`.

---

## Tech Stack

| Component        | Technology                          |
| ---------------- | ----------------------------------- |
| CLI              | Go                                  |
| Semantic Storage | Qdrant (Docker)                     |
| Code Graph       | Memgraph (Docker)                   |
| Embedding        | OpenAI API or local (nomic-embed)   |
| Config           | YAML                                |

---

## Project Layout

```
.gg/
  RULES.md                ← agent rules (single source, injected into each agent)
  config.yaml             ← qdrant/memgraph connection settings
  docker-compose.yaml     ← Qdrant + Memgraph
  volumes/
    qdrant/               ← Qdrant data (project-scoped, portable)
    memgraph/             ← Memgraph data (project-scoped, portable)
```

No other files. No task files, no decision files, no session files. Everything lives in the database.

---

## CLI Commands (original design)

### Session

```bash
gg init              # create .gg/, docker-compose up, first index
gg status            # open tasks, pending messages, recent decisions
```

### Decisions

```bash
gg decide "use JWT" --reason "stateless, mobile-friendly" --tags "auth,backend"
gg search "authentication"          # semantic search
gg reject "session-based auth" --reason "stateful, doesn't scale" --task "TASK-001"
```

### Tasks

```bash
gg task create "JWT auth endpoint" --detail "login, register, refresh" --priority high --tags "auth"
gg task list                        # all tasks (filter: --status pending/done/blocked)
gg task get TASK-001                # detail + related decisions + affected files
gg task done TASK-001 "JWT auth implemented, tests written"
gg task block TASK-001 "payment API key missing"
```

### Code Intelligence

```bash
gg index                            # index the full codebase into Memgraph
gg index --changed                  # incremental index (last commit changes only)
gg impact src/auth/login.ts         # what breaks if this file changes
```

### Agent Messaging

```bash
gg tell "developer" "auth module ready, JWT 1h expire"
gg tell "qa" "login endpoint rate limiting needs testing"
gg inbox                            # messages addressed to you
gg inbox --role developer           # filter by role
```

---

## Data Model

### Qdrant Collections

```
decisions
  ├── id: uuid
  ├── text: "Use JWT-based authentication"
  ├── reason: "stateless, mobile-friendly, microservice-ready"
  ├── tags: ["auth", "backend"]
  ├── task_id: "TASK-001" (nullable)
  ├── created_at: timestamp
  └── embedding: vector

tasks
  ├── id: "TASK-001"
  ├── title: "JWT auth endpoint"
  ├── detail: "implement login, register, refresh token"
  ├── status: "pending" | "in_progress" | "done" | "blocked"
  ├── priority: "high" | "medium" | "low"
  ├── depends_on: ["TASK-000"]
  ├── tags: ["auth", "backend"]
  ├── block_reason: null
  ├── done_summary: null
  ├── created_at: timestamp
  └── embedding: vector

messages
  ├── id: uuid
  ├── from_role: "architect"
  ├── to_role: "developer"
  ├── content: "auth module ready, JWT 1h expire"
  ├── read: false
  ├── task_id: "TASK-001" (nullable)
  └── created_at: timestamp

rejections
  ├── id: uuid
  ├── approach: "session-based authentication"
  ├── reason: "stateful, complicates horizontal scaling"
  ├── task_id: "TASK-001" (nullable)
  ├── created_at: timestamp
  └── embedding: vector
```

### Memgraph Schema

```cypher
// Node types
(:File {path: "src/auth/login.ts", language: "typescript", last_indexed: timestamp})
(:Function {name: "handleLogin", file: "src/auth/login.ts", line: 42})
(:Module {name: "auth", path: "src/auth/"})
(:Package {name: "jsonwebtoken", version: "9.0.0"})

// Relationships
(File)-[:IMPORTS]->(File)
(File)-[:BELONGS_TO]->(Module)
(Function)-[:CALLS]->(Function)
(Function)-[:DEFINED_IN]->(File)
(File)-[:USES_PACKAGE]->(Package)
(Module)-[:DEPENDS_ON]->(Module)
```

---

## Git Hooks

```bash
# .git/hooks/post-commit
#!/bin/sh
gg index --changed

# .git/hooks/pre-push
#!/bin/sh
gg check  # warn on unrecorded decisions or open tasks
```

---

## Phase 1 — Core CLI (Week 1)

- [x] Go project init + CLI framework (cobra)
- [x] `gg init` — create .gg/, docker-compose up, config.yaml
- [x] Qdrant connection + collection setup
- [x] Embedding generation (local via Ollama / nomic-embed-text)
- [x] `gg decide` / `gg search` / `gg reject`
- [x] `gg task create/list/get/done/block`
- [x] `gg tell` / `gg inbox`
- [x] `gg status`
- [x] docker-compose.yaml (Qdrant + Memgraph + Ollama)
- [x] AGENTS.md template generation

**Deliverable:** Agents can record decisions, open tasks, and message each other.

## Phase 2 — Code Intelligence (Week 2)

- [x] Memgraph connection + schema setup
- [x] `gg index` — SCIP-based indexing (Go, TypeScript, Python) → Memgraph
- [x] `gg index --changed` — incremental index
- [x] `gg impact <file>` — dependency analysis
- [x] Git post-commit hook (auto-index)
- [x] Git pre-push hook (check)
- [x] `gg context "topic"` — unified Qdrant + Memgraph query
- [x] Task get output with related decisions + affected files

**Deliverable:** Agents understand code structure and can run impact analysis.

## Phase 3 — Polish + Open Source (3-5 days)

- [x] README.md — hero banner, quick start
- [x] `gg doctor` — connectivity checks, agent rules check
- [x] Tests
- [x] GitHub Actions CI
- [x] GitHub release
- [x] docs/ — architecture, commands, getting started

**Estimated total: ~2.5-3 weeks**

---

## Example Flow

```
Terminal 1: BMAD/Claude Code (Architect role)
─────────────────────────────────────────────────
You: "How should we build the auth system?"

Agent: (gg search "auth" → no results)
       "No prior decisions on auth.
        Two options: JWT or session-based..."

You: "Let's go with JWT"

Agent: (gg decide "JWT-based auth" --reason "stateless, mobile-friendly" --tags "auth")
       (gg reject "session-based auth" --reason "stateful, complicates scaling")
       "Recorded. Opening a task for the auth endpoint."
       (gg task create "JWT auth endpoint" --detail "login, register, refresh token.
        JWT 1h expire, refresh 7 days." --priority high --tags "auth,api")
       "TASK-001 opened."
       (gg tell "developer" "JWT auth design approved, details in TASK-001")


Terminal 2: GSD (Developer role)
─────────────────────────────────────────────────
You: "Work on the tasks"

GSD: (gg inbox → "JWT auth design approved, details in TASK-001")
     (gg task list --status pending → TASK-001)
     (gg task get TASK-001 → detail + JWT decision + rejection)
     (gg impact "src/auth/" → new module, no existing dependents)

     → writes code: src/auth/login.ts, src/auth/register.ts
     → writes tests: src/auth/__tests__/
     → commits
     → (post-commit hook: gg index --changed)

     (gg task done TASK-001 "JWT auth implemented. Login, register, refresh endpoints ready.")
     (gg tell "qa" "auth endpoints ready, test rate limiting and token expire edge cases")
     "TASK-001 complete."
```

---

## Future Ideas (post-MVP)

- Web UI dashboard (optional — tasks, decisions, graph visualization)
- Agent auto-triggering (daemon mode)
- Plugin marketplace (custom embedding models, graph indexers)
- Team mode — multiple developers sharing one brain across a network
- Advanced analytics, agent activity scoring
