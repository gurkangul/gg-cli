# Contributing to gg

Thanks for your interest in contributing.

## Dev setup

1. Install prerequisites:
   - [Go 1.22+](https://golang.org/dl/)
   - [Qdrant](https://qdrant.tech): `docker run -p 6334:6334 qdrant/qdrant`
   - [Ollama](https://ollama.ai): `ollama pull nomic-embed-text`
   - [Memgraph](https://memgraph.com) _(optional, for graph tests)_: `docker run -p 7687:7687 memgraph/memgraph`

2. Clone and build:
   ```sh
   git clone https://github.com/gurkangul/gg.git
   cd gg
   go build ./...
   ```

3. Initialize a test project:
   ```sh
   mkdir /tmp/gg-test && cd /tmp/gg-test
   gg init
   gg doctor
   ```

## Running tests

```sh
go test ./... -race -count=1
```

Lint:
```sh
golangci-lint run --timeout=5m
```

## Decision flow

Before starting significant work, check whether the area already has a recorded decision:

```sh
gg search "<topic>"
```

If you reach a design decision while contributing, record it:

```sh
gg record "decision text" --reason "why" --tags "..."
```

This project uses `gg` for all architectural decisions. Check `AGENTS.md` for the full protocol.

## Commit message style

Use the [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>: <short description>

[optional body]
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `perf`, `chore`, `ci`

Examples:
- `feat(inbox): add --dismiss-all flag`
- `fix(store): prevent task ID collision under concurrent allocators`
- `docs: update AGENTS.md with bug lifecycle rule`

## Pull request process

1. Fork the repo and create a branch from `main`.
2. Make your changes. Add or update tests where appropriate.
3. Run `go test ./... -race` and `golangci-lint run` — both must pass.
4. Open a PR. Describe what changed and why.
5. A maintainer will review. Feedback goes in PR comments.

## Test bar

- New store methods: add a unit test.
- New CLI commands: add a cobra `ExecuteC` integration test in `cmd/cmd_test.go`.
- Bug fixes: reproduce the failure in a test before fixing it.

## Code of Conduct

See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
