# Launch Readiness — v0.1.0

**Progress: 8/14 (57%)**

Last updated: 2026-04-14

---

## Checklist

### OSS Foundations

- [x] `LICENSE` — MIT License
- [x] `README.md` — hero pitch, quickstart, prerequisites, architecture
- [x] `CHANGELOG.md` — Keep a Changelog format, v0.1.0 section
- [x] `git tag v0.1.0` — annotated tag on main branch
- [ ] `CONTRIBUTING.md` — contribution guide, dev setup, PR process
- [ ] `SECURITY.md` — vulnerability reporting process

### Distribution

- [ ] GitHub Release — release notes, binary attachments, `v0.1.0` tag published
- [ ] GitHub Release Workflow — Goreleaser + `.github/workflows/release.yml`
- [ ] `go install` smoke test (3 OS: linux/amd64, darwin/arm64, windows/amd64)
- [ ] Single-line install (`brew tap` or equivalent) — optional for v0.1.0

### Documentation

- [x] `docs/integrations/*` — agent inject docs (Claude Code, Cursor, Codex, Aider, GSD)
- [ ] Announcement draft — blog post or GitHub Discussion for initial launch

### Repository Health

- [ ] Issue templates — bug report, feature request (`.github/ISSUE_TEMPLATE/`)
- [x] GitHub Actions CI — ubuntu/macos/windows matrix, race detector, golangci-lint

---

## Remaining work (by task)

| Task | Description | Priority |
|------|-------------|----------|
| TASK-059 | Semantic dedup guard on create operations | high |
| TASK-060 | Dogfood measurement — agent call-rate telemetry | high |
| TASK-061 | Outbox stress test + convergence p50/p99 metrics | high |
| TASK-062 | Inbox UX: dismiss-all, since, group-by, summary | high |
| TASK-066 | GitHub release workflow + Goreleaser | high |
| — | `CONTRIBUTING.md` | medium |
| — | `SECURITY.md` | medium |
| — | Issue templates | medium |
| — | Announcement draft | low |

---

## Update cadence

Update this file at the completion of each launch-related task. Increment the progress counter in the heading.
