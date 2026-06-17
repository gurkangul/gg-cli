package cmd

import (
	"net"
	"testing"
	"time"
)

// requireOllamaOrSkip skips integration tests that exercise the real embedding
// path when the local Ollama endpoint is unreachable. These tests embed via the
// default Ollama backend (localhost:11434); on CI or machines without Ollama
// they must SKIP rather than FAIL. The release workflow runs `go test ./...`
// with no Ollama present, so without this guard every live-embedding test fails
// with "connect: connection refused" and blocks the release (see CHANGELOG 2.1.0).
func requireOllamaOrSkip(t *testing.T) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "localhost:11434", 750*time.Millisecond)
	if err != nil {
		t.Skipf("Ollama (localhost:11434) unreachable (%v) — skipping live-embedding integration test", err)
	}
	_ = conn.Close()
}
