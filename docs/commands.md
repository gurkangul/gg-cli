# Command Reference

Full reference for all `gg` commands. Run `gg <command> --help` for live usage.

## Session

| Command | Description |
|---|---|
| `gg session-start --agent <id> --role <role>` | Start-of-session briefing and identity validation |
| `gg next --agent <id> --role <role>` | Read-only next-step packet: inbox, active work, ready tasks/reviews |
| `gg status` | Open tasks, pending messages, recent decisions |
| `gg search "topic" --compact` | Semantic search over decisions and rejections |
| `gg context "topic" --compact` | Unified context bundle (decisions + rejections + tasks + notes) |
| `gg inbox --role <role> --peek` | Role-scoped messages without consuming same-role reads |

## Decisions & rejections

```sh
gg record "use JWT" --reason "stateless" --tags "auth"           # accepted decision
gg record "sessions" --decision-status rejected --reason "stateful" # rejected approach
```

> **Deprecated aliases:** `gg decide` → `gg record`; `gg reject` → `gg record --decision-status=rejected`

## Tasks

```sh
gg task create "title" --detail "..." --priority high|medium|low --tags "t1,t2"
gg task list [--status pending|in_progress|ready_for_live|done|blocked] [--ready] [--compact]
gg task get TASK-ID [--review]
gg task start TASK-ID --owner "$GG_AGENT" --lease 30m
gg task renew TASK-ID --owner "$GG_AGENT" --lease 30m
gg task release TASK-ID --owner "$GG_AGENT"
gg task ready-for-live TASK-ID --plan "Reviewer: <check>; Evidence: commands=<cmds>; live=<smoke>; impact=<files>; gaps=<none|gap>; artifacts=<paths>" --from "$GG_ROLE"
gg task review TASK-ID --approve|--reject --by reviewer --notes "..."
gg task done TASK-ID "verified summary" --verifier reviewer
gg task block TASK-ID "reason"
gg task unblock TASK-ID --owner "$GG_AGENT"   # clear blocked -> back to in_progress (inverse of block)
gg task deps TASK-ID
gg task packet TASK-ID             # reviewer handoff packet; also available as gg task get --review
```

Evidence/handoff summaries belong in existing task/tell fields. Keep bulky logs
or screenshots outside gg and record only the summary plus path/reference.

## Bugs

```sh
gg bug report "title" --detail "..." --severity critical|high|medium|low --files path/to/file.go --symbols SymbolName
gg bug list [--status open|fixing|fixed|wontfix]
gg bug get BUG-ID
gg bug start BUG-ID
gg bug fix BUG-ID "summary" --root-cause "what caused it" --repro path/to/repro.sh --repro-broken-ref <sha> --files path/to/file.go --symbols SymbolName
gg bug wontfix BUG-ID "reason"
gg bug triage BUG-ID          # unified context bundle for fixing
```

## Messaging

```sh
gg tell "role" "message" --from architect
gg tell reviewer "TASK-123 ready. Evidence: commands run: <cmds>; live smoke: <result>; impacted files: <files>; known gaps: <none|gap>; artifacts: <paths>" --from "$GG_ROLE" --task TASK-123
gg inbox --role developer --peek
```

## Code index & impact

```sh
gg index --lang go|python|swift|typescript [--changed]
gg impact <file>          # downstream deps + exported symbols + related KB entries
gg check                  # pre-push: open tasks + unresolved discussions
```

Swift CodeGraph support requires an external compatible `scip-swift` binary;
`gg doctor --install-indexers` does not auto-install Swift because there is no
official maintained Sourcegraph Swift SCIP indexer. The binary must accept
`scip-swift index --output <scip-file> <project-root>` and emit document paths
relative to `<project-root>` or absolute paths under the gg project root.

See [docs/commands/impact.md](commands/impact.md) for the full `gg impact` semantic contract (hop depth, exit codes, KB selection, flags).

## Operations

```sh
gg init                              # initialize project + embedded SQLite stores
gg doctor                            # connectivity + indexer binary checks
gg doctor --install-indexers         # auto-install missing SCIP binaries
gg doctor --reconcile                # report incomplete graph writes
gg reembed --confirm                 # migrate collections to new embedding model
```

## Global flags

| Flag | Description |
|---|---|
| `--json` | Output as JSON (for agent consumption) |
| `--from <role>` | Override author (defaults to `$GG_ROLE`) |
