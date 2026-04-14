# Codebase Map

Generated: 2026-04-14T11:16:12Z | Files: 30 | Described: 0/30
<!-- gsd:codebase-meta {"generatedAt":"2026-04-14T11:16:12Z","fingerprint":"c5591888143f4ef251b16618436d569d37ab2c29","fileCount":30,"truncated":false} -->

### (root)/
- `.gitignore`
- `AGENTS.md`
- `gg-plan.md`
- `go.mod`
- `go.sum`
- `main.go`

### cmd/
- `cmd/decide.go`
- `cmd/helpers.go`
- `cmd/inbox.go`
- `cmd/init.go`
- `cmd/reject.go`
- `cmd/root.go`
- `cmd/search.go`
- `cmd/status.go`
- `cmd/task.go`
- `cmd/tell.go`

### internal/config/
- `internal/config/config.go`

### internal/embedding/
- `internal/embedding/ollama.go`

### internal/store/
- `internal/store/client.go`
- `internal/store/decisions.go`
- `internal/store/flock_unix.go`
- `internal/store/flock_windows.go`
- `internal/store/messages.go`
- `internal/store/rejections.go`
- `internal/store/tasks.go`
- `internal/store/taskseq.go`

### internal/templates/
- `internal/templates/config.yaml`
- `internal/templates/docker-compose.yaml`
- `internal/templates/rules.md`
- `internal/templates/templates.go`
