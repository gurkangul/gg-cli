# Smoke Tests — Fresh Install

## Purpose

`fresh-install.sh` verifies the full first-run flow for `gg` inside a clean
Ubuntu Docker container. It proves that a developer on a fresh machine can
build gg from source and drive the core workflow end-to-end.

## Prerequisites

| Requirement | Notes |
|---|---|
| Docker Desktop (or Engine) | Container image is `ubuntu:22.04` |
| Go source checked out | Repo root is mounted read-only at `/src` |
| Qdrant on host port 6334 | Server-backend smoke only — qdrant is no longer in the default compose; add a qdrant service to `~/.gg/docker-compose.yaml`, then `cd ~/.gg && docker compose up -d qdrant` |
| Ollama on host port 11434 | Run via `cd ~/.gg && docker compose up -d ollama` (or native `ollama serve`) |
| Memgraph on host port 7687 | Server-backend smoke only — memgraph is no longer in the default compose; add a memgraph service to `~/.gg/docker-compose.yaml`, then `cd ~/.gg && docker compose up -d memgraph` |

The script connects to host-side services via `host.docker.internal`
(automatically resolved on macOS Docker Desktop and Linux with
`--add-host=host.docker.internal:host-gateway`). Override with:

```sh
HOST_GATEWAY=172.17.0.1 make smoke-fresh
```

## What the script asserts

| AC | Assertion | Exit behaviour |
|---|---|---|
| AC-1 | `go install ./cmd/gg` completes; `which gg` returns a path | Fails immediately |
| AC-2 | `gg init` creates `.gg/` with `config.yaml`, `.gitignore`, `RULES.md` | Fails immediately |
| AC-3 | `gg doctor` exits 0, OR exits non-zero with a graceful service-unreachable message | See note below |
| AC-4a | `gg record "smoke test decision" ...` exits 0 | Continues, logs FAIL |
| AC-4b | `gg task create "smoke task" ...` exits 0 | Continues, logs FAIL |
| AC-4c | `gg search --compact "smoke test decision"` exits 0 AND surfaces the recorded decision | Continues, logs FAIL |
| AC-4d | `gg status` exits 0 | Continues, logs FAIL |

**AC-3 graceful-warning rule:** When host services are unreachable from inside
the container, `gg doctor` is expected to exit non-zero but print a human-readable
warning containing one of: `unreachable`, `connect`, `refused`, `timeout`, `warn`,
`cannot`, `not running`. A non-zero exit with *no* recognisable warning text is a
FAIL — it means gg panicked or emitted an opaque error.

## Running

```sh
# Standard run (services must be up on host)
make smoke-fresh

# Override gateway IP (Linux Docker Engine)
HOST_GATEWAY=172.17.0.1 make smoke-fresh

# Run the script directly (skips make)
./scripts/smoke/fresh-install.sh
```

Expected output (services running):

```
[smoke] Pulling ubuntu:22.04 (cached after first run)...
[smoke] Detecting host gateway for container→host service reach...
[smoke] Starting container gg-smoke-fresh-<PID>...
=== Environment ===
OS: Ubuntu 22.04.x LTS
...
=== AC-1: Build gg from source ===
[AC] PASS: AC-1: go install ./cmd/gg succeeded, which gg = /root/go/bin/gg
=== AC-2: gg init ===
[AC] PASS: AC-2: .gg/ directory exists with config.yaml
[AC] PASS: AC-2: expected initial files present (config.yaml, .gitignore, RULES.md)
=== AC-3: gg doctor ===
[AC] PASS: AC-3: gg doctor exited 0
=== AC-4: Happy-path commands ===
[AC] PASS: AC-4a: gg record exited 0
[AC] PASS: AC-4b: gg task create exited 0
[AC] PASS: AC-4c: gg search --compact exited 0
[AC] PASS: AC-4c: gg search surfaced the just-recorded decision
[AC] PASS: AC-4d: gg status exited 0
=== Smoke Results ===
PASS: 8
FAIL: 0
All assertions passed.
[smoke] smoke-fresh PASSED
```

Expected output (services **not** running — graceful warning path):

```
=== AC-3: gg doctor ===
...connection refused...
[AC] PASS: AC-3: gg doctor exited non-zero but emitted a graceful service-unreachable warning (expected when host services are down)
```

AC-4 commands will FAIL when services are unreachable (they need Qdrant to
store/retrieve data). This is expected — start the services and re-run.

## Debugging failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `docker: command not found` | Docker not installed or not in PATH | Install Docker Desktop |
| `AC-1 FAIL: which gg failed` | Go install path not in `$PATH` | Script sets `$GOPATH/bin` — check if build itself errored |
| `AC-2 FAIL: .gg/ missing` | `gg init --yes --no-index` returned an error | Check if `git init` ran (required for project detection) |
| `AC-3 FAIL: opaque error` | `gg doctor` panicked or errored without a human message | File a bug with the raw output |
| `AC-4 FAIL: gg record` | Qdrant unreachable (server-backend smoke) | Add a qdrant service to `~/.gg/docker-compose.yaml`, then `cd ~/.gg && docker compose up -d qdrant` |
| `AC-4c FAIL: search empty` | Ollama embedding service unreachable | Start Ollama: `cd ~/.gg && docker compose up -d ollama` |
| `host.docker.internal: no address` | Linux Docker Engine without host-gateway | Set `HOST_GATEWAY=$(ip route show | awk '/default/ {print $3}')` |

## CI integration

CI integration is out of scope for this script — tracked as a separate
follow-up. The script is designed to be CI-friendly (non-interactive,
deterministic exit codes) when services are available as job services.
