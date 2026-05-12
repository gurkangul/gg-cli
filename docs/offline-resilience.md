# Offline Resilience — JSONL Primary + Outbox Replay

> Status: implemented (TASK-352, fixes BUG-030)

## Problem

Prior to this fix, all brain-write commands (`gg record`, `gg task create`, `gg bug report`, `gg reject`) performed a synchronous Qdrant upsert as their only write path.  When Qdrant was unreachable the command failed with a non-zero exit code and the user's data was lost.

## Architecture

### JSONL-first write path (AC-1)

Every brain-write verb now writes to a local JSONL file **first**, before attempting Qdrant:

```
.gg/brain/
  decisions.jsonl   # gg record
  rejections.jsonl  # gg record --decision-status=rejected
  tasks.jsonl       # gg task create
  bugs.jsonl        # gg bug report
```

Each line is a JSON object:

```json
{"uuid":"<uuid>","kind":"decisions","created_at":"2026-04-26T10:00:00Z","author":"developer","payload":{...}}
```

- **Idempotent by uuid**: writing the same uuid twice is safe; the outbox replay uses uuid to avoid double-insert.
- **Append-only**: entries are never deleted from JSONL (consistent with the forward-only memory design).

### Qdrant upsert as secondary best-effort (AC-2)

After the JSONL write succeeds, the command attempts a Qdrant upsert with the normal short timeout.

- **Qdrant up**: upsert succeeds → exit 0, no outbox entry.
- **Qdrant down**: upsert fails → `OutboxQueued` is returned → caller writes an outbox entry and prints to stderr:

  ```
  ⚠ queued for vector index (Qdrant unreachable; will replay on recovery)
  ```

  The command still exits 0.  Data is safe in JSONL.

### Outbox replay (AC-3)

When Qdrant recovers, drain the outbox:

```bash
gg doctor --reconcile
```

The reconcile command:
1. Lists outbox entries.
2. For brain replay kinds (`record-replay`, `reject-replay`, `task-replay`, `bug-replay`): reads the entry from `.gg/brain/<kind>.jsonl` by UUID and re-upserts to Qdrant.
3. Removes the outbox entry on success; increments the retry counter on failure.
4. Continues to the next entry on failure (no abort-on-error).

After replay, run a reindex to restore vector embeddings for semantic search:

```bash
gg brain reindex  # (future command — vectors for replayed entries are zero-filled)
```

### Offline ID allocation

Task and bug ID allocation uses a file-locked seq file (`.gg/.task-seq`, `.gg/.bug-seq`).  When the seq file is empty and Qdrant is unavailable, the bootstrap falls back to scanning `.gg/brain/tasks.jsonl` / `.gg/brain/bugs.jsonl` for the highest existing ID, preventing collisions without Qdrant.

### Read path graceful degrade (AC-4)

`gg search` falls back to a text scan of `.gg/brain/decisions.jsonl` and `.gg/brain/rejections.jsonl` when Qdrant is unreachable, printing:

```
⚠ Qdrant unreachable — read served from JSONL (may miss cross-project context)
```

`gg record`'s duplicate-detection prompt (`promptIfDuplicate`) is silently skipped when Qdrant is down — creation proceeds without a dedup check.

`runInboxGatePreflight` already fails-open when Qdrant is unreachable (no change needed).

## Data flow

```
gg record "use JWT" --reason "stateless"
  │
  ├─► write .gg/brain/decisions.jsonl    ← primary, always
  │
  ├─► attempt Qdrant upsert (5s timeout)
  │     │
  │     ├─ success → done, exit 0
  │     │
  │     └─ failure → write .gg/outbox/<uuid>.json
  │                   print ⚠ stderr note
  │                   exit 0
```

## Recovery flow

```
docker start gg-qdrant-1
gg doctor --reconcile
  │
  ├─► for each brain-kind outbox entry:
  │     read .gg/brain/<kind>.jsonl → find entry by UUID
  │     upsert to Qdrant
  │     delete outbox entry on success
  │
  └─► gg search "use JWT"  → returns result from Qdrant
```

## File locations

| File | Purpose |
|------|---------|
| `.gg/brain/decisions.jsonl` | Append-only brain decisions |
| `.gg/brain/rejections.jsonl` | Append-only brain rejections |
| `.gg/brain/tasks.jsonl` | Append-only brain tasks |
| `.gg/brain/bugs.jsonl` | Append-only brain bugs |
| `.gg/outbox/<uuid>.json` | Pending Qdrant upserts (drained by `--reconcile`) |

## Out of scope

- Memgraph-side replay (already covered by the existing index outbox).
- Changing the embedding provider.
- Encryption-at-rest for JSONL files.
- Full semantic search from JSONL (text scan only — semantic ranking requires Qdrant).
