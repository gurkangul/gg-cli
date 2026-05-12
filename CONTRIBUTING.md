# Contributing to gg

Thanks for your interest in contributing.

## Development setup

**Prerequisites:**

- [Go 1.26.2+](https://golang.org/dl/)
- Docker (for Qdrant and Memgraph)
- [Ollama](https://ollama.ai) when using custom/manual service setup

**Start services:**

For normal development, run `gg init` in a test project first. It writes
`~/.gg/docker-compose.yaml`, starts Qdrant/Memgraph/Ollama through Docker, and
pulls the embedding model for the managed stack.

```sh
# From the repo root, after gg init has created ~/.gg/docker-compose.yaml
docker compose -f ~/.gg/docker-compose.yaml up -d
```

Or run services individually:

```sh
# Qdrant
docker run -p 6334:6334 qdrant/qdrant

# Memgraph (optional — only needed for graph/integration tests)
docker run -p 7687:7687 memgraph/memgraph
```

**Build and verify:**

```sh
git clone https://github.com/gurkangul/gg-cli.git
cd gg-cli
go build ./...
go vet ./...
```

**Initialize a test project:**

```sh
mkdir /tmp/gg-test && cd /tmp/gg-test
gg init
gg doctor
```

`gg init` writes `~/.gg/docker-compose.yaml` the first time it runs — if the
Qdrant/Memgraph containers aren't already up, it will start them. You only
need the standalone `docker run` commands above if you want to run services
outside gg's managed compose file.

## Agent identity

Most write commands (`gg task create/done`, `gg record`, `gg tell`, …) refuse
to run without an agent identity, so telemetry can distinguish human calls
from agent ones. Before running them, set:

```sh
export GG_AGENT=claude-code   # or cursor, codex, gsd, ...
```

Use your real agent name when contributing with an AI assistant — the
identity lands in commit trails via `--from` and in decision records.

## Running tests

Unit tests (no services required):

```sh
go test ./... -race -count=1 -timeout=120s
```

Integration tests (requires Qdrant + Memgraph running):

```sh
go test -tags integration ./... -race -count=1 -timeout=120s
```

Lint:

```sh
golangci-lint run --timeout=5m
```

Vulnerability check:

```sh
govulncheck ./...
```

## Tracing

Set `GG_TRACE=1` to record operation latencies to `.gg/traces/YYYY-MM-DD.jsonl`. Run `gg status` to see p50/p95/p99 from the last 100 spans.

## Decision flow

Before starting significant work, check for existing decisions:

```sh
gg search "<topic>"
```

Record design decisions as you work:

```sh
gg record "decision text" --reason "why" --tags "..."
```

This project uses `gg` for all architectural decisions. See `AGENTS.md` for the full protocol.

## Commit message style

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <short description>

[optional body]
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `chore`, `ci`

Examples:

```
feat(inbox): add --dismiss-all flag
fix(store): prevent task ID collision under concurrent allocators
docs: update AGENTS.md with bug lifecycle rule
test(graph): add cross-project isolation integration test
```

## Pull request process

1. Fork and create a branch from `main`.
2. Make changes and add or update tests where appropriate.
3. Run `go test ./... -race -count=1 -timeout=120s` and `golangci-lint run` — both must pass.
4. Open a PR using the [pull request template](.github/PULL_REQUEST_TEMPLATE.md).
5. A maintainer will review. Feedback goes in PR comments.

## Test expectations

- New store methods: add a unit test in `internal/store/`.
- New CLI commands: add a cobra `ExecuteC` integration test in `cmd/cmd_test.go`.
- Bug fixes: reproduce the failure in a test before fixing it.
- Cross-project isolation: any new Qdrant or Memgraph writes must go through the choke-point wrappers (`store.qdrantUpsert`/`qdrantQuery` and `graph.runQuery`). Adding a new caller to the `runQueryNoPID` allowlist (in `internal/graph/chokepoint_test.go`) requires explicit PR review — that function bypasses automatic `project_id` injection and is reserved for DDL / schema-level queries only.

## Code style

- `gofmt` and `golangci-lint` are enforced in CI.
- Prefer small, composable functions over large method bodies.
- No `TODO` stubs in submitted code — finish what you start.
- Secrets and credentials must never appear in source or history. See [SECURITY.md](SECURITY.md).

**File size limits** (enforced via `gg audit file-size` and the pre-task-done
hook `30-file-size.sh`):

- Source files (`.go`/`.ts`/`.js`/`.py`/`.rs`/`.java`): **500 lines max**.
- Test files (`*_test.go`, `*.test.*`, `*.spec.*`): **800 lines max**.

Oversized files must be split into cohesive modules — extract helpers, split
by concern, no god-objects. Set `GG_FILE_SIZE_GATE=block` to turn the
advisory hook into a hard fail locally.

## Verify gates

Before a task transitions to done, gg runs every executable `*.sh` under
`.gg/hooks/pre-task-done.d/` in order. Any non-zero exit aborts the
transition (exit code 7) and the task stays in its current state. Starter
hooks are installed by `gg doctor --install-task-hooks` — detects Go
(`go.mod`) and Node/Bun (`package.json`) and writes appropriate checks
(`gofmt`, `go vet`, `go test`, type-check, lint).

PostToolUse also runs `gg verify --file <path>` on writes — fast gofmt+vet
per-file, budget ≤2s. Wired via `gg doctor --install-agent-hooks`.

## Code of Conduct

Be direct and respectful. Focus on the work, not the person.
