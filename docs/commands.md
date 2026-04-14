# Command Reference

Full reference for all `gg` commands. Run `gg <command> --help` for live usage.

## Session

| Command | Description |
|---|---|
| `gg status` | Open tasks, pending messages, recent decisions |
| `gg search "topic"` | Semantic search over decisions and rejections |
| `gg context "topic"` | Unified context bundle (decisions + rejections + tasks + notes) |

## Decisions & rejections

```sh
gg record "use JWT" --reason "stateless" --tags "auth"           # accepted decision
gg record --stance=reject "sessions" --reason "stateful"         # rejected approach
```

> **Deprecated aliases:** `gg decide` → `gg record`; `gg reject` → `gg record --stance=reject`

## Tasks

```sh
gg task create "title" --detail "..." --priority high|medium|low --tags "t1,t2"
gg task list [--status pending|in_progress|done|blocked] [--ready]
gg task get TASK-ID
gg task done TASK-ID "summary"
gg task block TASK-ID "reason"
gg task deps TASK-ID
```

## Bugs

```sh
gg bug report "title" --detail "..." --severity critical|high|medium|low
gg bug list [--status open|fixing|fixed|wontfix]
gg bug get BUG-ID
gg bug start BUG-ID
gg bug fix BUG-ID "summary" --root-cause "what caused it"
gg bug wontfix BUG-ID "reason"
gg bug triage BUG-ID          # unified context bundle for fixing
```

## Discussions

```sh
gg discuss open "topic" --detail "..."
gg discuss list [--all]
gg discuss get DISC-ID
gg discuss resolve DISC-ID --via decision --summary "decided X"
gg discuss dismiss DISC-ID --reason "superseded"
```

## Notes

```sh
gg note "observation"
gg note list
gg note search "topic"
```

## Messaging

```sh
gg tell "role" "message" --from architect
gg inbox [--role developer]
```

## Code index & impact

```sh
gg index --lang go|typescript|python [--changed]
gg impact <file>          # downstream deps + exported symbols + related KB entries
gg check                  # pre-push: open tasks + unresolved discussions
```

See [docs/commands/impact.md](commands/impact.md) for the full `gg impact` semantic contract (hop depth, exit codes, KB selection, flags).

## Operations

```sh
gg init                              # initialize project + Qdrant collections
gg doctor                            # connectivity + indexer binary checks
gg doctor --install-indexers         # auto-install missing SCIP binaries
gg doctor --reconcile                # report incomplete Memgraph writes
gg reembed --confirm                 # migrate collections to new embedding model
```

## Global flags

| Flag | Description |
|---|---|
| `--json` | Output as JSON (for agent consumption) |
| `--from <role>` | Override author (defaults to `$GG_ROLE`) |
