# Test Coverage

## Alpha thresholds (current)

| Package | Current | Required | Status |
|---------|---------|----------|--------|
| `./cmd` | 40.0% | ≥ 40% | ✓ |
| `./internal/store` | 51.2% | ≥ 50% | ✓ |
| `./internal/graph` | 48.4% | ≥ 40% | ✓ |

Thresholds are enforced in CI via the **Coverage thresholds** step in `.github/workflows/ci.yml`.

## v1.0 target (public release)

All three packages must reach **≥ 70%** before the v1.0 tag.

## Test strategy

Most tests run without a live backend. There are three test tiers:

### Tier 1 — no backend (always run)

Cobra arg-count validation, flag validation, and config-not-found paths.
These live in `cmd/cmd_test.go` and exercise the cobra layer only.

### Tier 2 — config present, embedded store (always run)

`cmd/fixtures_test.go` creates a minimal `.gg` directory via `setupGGDir(t)`.
Commands reach `loadDeps` / `loadDepsReadOnly` against the embedded SQLite
stores (always reachable — they are local files). Read-only commands (search,
context) also exercise the LKG cache-hit code paths.

### Tier 3 — live embedding endpoint (skip if unavailable)

Integration tests that require a running Ollama embedding endpoint (the stores
are embedded SQLite and need no external service). Currently none are
implemented in the alpha phase; they will be added in TASK-043 (v1.0 prep).

## Running coverage locally

```bash
# Per-package (matches CI check):
go test ./cmd             -coverprofile=/tmp/cmd.cov   -covermode=atomic && go tool cover -func=/tmp/cmd.cov   | tail -1
go test ./internal/store  -coverprofile=/tmp/store.cov -covermode=atomic && go tool cover -func=/tmp/store.cov | tail -1
go test ./internal/graph  -coverprofile=/tmp/graph.cov -covermode=atomic && go tool cover -func=/tmp/graph.cov | tail -1

# HTML report for a package:
go tool cover -html=/tmp/cmd.cov -o /tmp/cmd_coverage.html && open /tmp/cmd_coverage.html
```
