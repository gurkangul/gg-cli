# Contributing to gg

Thanks for your interest in contributing.

## Development setup

**Prerequisites:**

- [Go 1.26.2+](https://golang.org/dl/)
- Docker (for Qdrant and Memgraph)
- [Ollama](https://ollama.ai) — `ollama pull nomic-embed-text`

**Start services:**

```sh
# From the repo root — starts Qdrant + Memgraph
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

## Running tests

Unit tests (no services required):

```sh
go test ./... -race -count=1
```

Integration tests (requires Qdrant + Memgraph running):

```sh
go test -tags integration ./... -race -count=1
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
3. Run `go test ./... -race` and `golangci-lint run` — both must pass.
4. Open a PR using the [pull request template](.github/PULL_REQUEST_TEMPLATE.md).
5. A maintainer will review. Feedback goes in PR comments.

## Test expectations

- New store methods: add a unit test in `internal/store/`.
- New CLI commands: add a cobra `ExecuteC` integration test in `cmd/cmd_test.go`.
- Bug fixes: reproduce the failure in a test before fixing it.
- Cross-project isolation: any new Qdrant or Memgraph writes must go through the choke-point wrappers (`store.qdrantUpsert`/`qdrantQuery` and `graph.runQuery`).

## Code style

- `gofmt` and `golangci-lint` are enforced in CI.
- Prefer small, composable functions over large method bodies.
- No `TODO` stubs in submitted code — finish what you start.
- Secrets and credentials must never appear in source or history. See [SECURITY.md](SECURITY.md).

## Code of Conduct

Be direct and respectful. Focus on the work, not the person.
