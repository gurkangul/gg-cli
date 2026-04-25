# Testing

The canonical command for running the full test suite in this project is:

```sh
go test ./... -count=1 -race -timeout=120s
```

Every flag is load-bearing — drop one and you lose a guarantee that has bitten us in production before.

## Why each flag

| Flag | Why |
|---|---|
| `./...` | Run every package; partial runs hide cross-package regressions. |
| `-count=1` | Disable Go's test result cache. Without it, a green cache hit can mask a regression introduced by a change Go didn't fingerprint (e.g. a non-Go fixture change). |
| `-race` | Enable the race detector. Race bugs in this codebase are frequent enough (TASK-308 was reopened twice) that running without it routinely is dangerous. |
| `-timeout=120s` | Per-test timeout cap. Default Go timeout is 10 minutes per test; 120s catches hangs much earlier. |

## What about `-short`?

`-short` toggles `testing.Short()` inside individual tests. A test that calls `t.Skip(...)` when `-short` is true gets skipped.

Two contexts:

- **Local dev (yours):** `go test -short ./...` is fine for fast feedback when you don't need the heavy regression tests (e.g. the `bootstrapAgentInPane` 3-second sleep test). Use it freely.
- **CI:** `-short` is **never** used. The full suite runs every push to `main` and every PR. The whole point of CI is to catch what local `-short` skipped. Adding `-short` to CI silently drops coverage and is treated as a regression.

If you write a test that needs to be `-short`-skipped, document the reason in the skip message:

```go
if testing.Short() {
    t.Skip("3s sleep between agent launch and prompt makes this slow")
}
```

That way a future reader can see whether the skip is still warranted.

## Where this is enforced

- `.github/workflows/ci.yml` test step uses the canonical command (no `-short`).
- The `pre-task-done` verify gate runs the same suite. The `GG_INSIDE_HOOK=1` env var forces the hook to use a hook-safe variant when invoked from inside another `gg` process (see `internal/templates/10-go-verify.sh`).
- BUG-021/022 regression repros (`testdata/regression/bug-021.sh`, `bug-022.sh`) explicitly invoke `go test -count=1 -timeout=30s` on specific test names — they do **not** pass `-short`, ensuring the bootstrap sequence test that guards them always runs.

## Related

- `AGENTS.md` § PRE-DONE VERIFY GATE — describes the hooks that wrap test execution before `gg task done`.
- `docs/verify-gate.md` — gate-by-gate reference.
