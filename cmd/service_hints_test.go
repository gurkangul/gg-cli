package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// expectedComposePath mirrors composePathForHint so the test asserts the hint
// uses the REAL shared compose path gg provisions (~/.gg/docker-compose.yaml),
// derived — not hardcoded.
func expectedComposePath(t *testing.T) string {
	t.Helper()
	shared, err := config.SharedDir()
	if err != nil {
		return "~/.gg/docker-compose.yaml"
	}
	return filepath.Join(shared, "docker-compose.yaml")
}

func TestServiceRecoveryHint(t *testing.T) {
	compose := expectedComposePath(t)

	cases := []struct {
		name      string
		service   string
		wantLabel string
		wantSvc   string // service token in the compose command
	}{
		{"qdrant", svcQdrant, "Qdrant is unreachable", "qdrant"},
		{"memgraph", svcMemgraph, "Memgraph is unreachable", "memgraph"},
		{"ollama", svcOllama, "Ollama is unreachable", "ollama"},
		// Case-insensitive / padded input still resolves.
		{"qdrant-upper", "QDRANT", "Qdrant is unreachable", "qdrant"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serviceRecoveryHint(tc.service)

			if !strings.Contains(got, tc.wantLabel) {
				t.Fatalf("hint for %q missing label %q:\n%s", tc.service, tc.wantLabel, got)
			}

			wantUp := "docker compose -f " + compose + " up -d " + tc.wantSvc
			if !strings.Contains(got, wantUp) {
				t.Fatalf("hint for %q missing up command %q:\n%s", tc.service, wantUp, got)
			}

			wantLogs := "docker compose -f " + compose + " logs -f " + tc.wantSvc
			if !strings.Contains(got, wantLogs) {
				t.Fatalf("hint for %q missing logs command %q:\n%s", tc.service, wantLogs, got)
			}
		})
	}
}

func TestServiceRecoveryHintUnknownService(t *testing.T) {
	compose := expectedComposePath(t)
	got := serviceRecoveryHint("postgres")

	// Unknown service must not panic and must still give an actionable
	// compose-up hint (generic, no service token).
	wantUp := "docker compose -f " + compose + " up -d"
	if !strings.Contains(got, wantUp) {
		t.Fatalf("unknown-service hint missing generic up command %q:\n%s", wantUp, got)
	}
	if !strings.Contains(got, "unreachable") {
		t.Fatalf("unknown-service hint missing 'unreachable':\n%s", got)
	}
}

func TestWithServiceHint(t *testing.T) {
	compose := expectedComposePath(t)
	got := withServiceHint("Qdrant unreachable — writes blocked", svcQdrant)

	if !strings.HasPrefix(got, "Qdrant unreachable — writes blocked\n") {
		t.Fatalf("withServiceHint must preserve the raw message first:\n%s", got)
	}
	if !strings.Contains(got, "docker compose -f "+compose+" up -d qdrant") {
		t.Fatalf("withServiceHint missing qdrant recovery command:\n%s", got)
	}
}

func TestEmbedErrAttachesOllamaHintOnlyOnConnectivity(t *testing.T) {
	compose := expectedComposePath(t)

	// Connectivity failure → Ollama recovery hint attached.
	connErr := embedErr("generate embedding", errConnRefused{})
	if !strings.Contains(connErr.Error(), "docker compose -f "+compose+" up -d ollama") {
		t.Fatalf("connectivity embed error should carry the Ollama hint:\n%s", connErr.Error())
	}

	// Model/dimension error (not an outage) → raw error, no service hint.
	dimErr := embedErr("generate embedding", errDimMismatch{})
	if strings.Contains(dimErr.Error(), "docker compose") {
		t.Fatalf("non-connectivity embed error must not be misattributed to Ollama:\n%s", dimErr.Error())
	}
	if !strings.Contains(dimErr.Error(), "dimension mismatch") {
		t.Fatalf("non-connectivity embed error should preserve the raw error:\n%s", dimErr.Error())
	}
}

func TestMemgraphDownErrCarriesHint(t *testing.T) {
	compose := expectedComposePath(t)
	err := memgraphDownErr("schema init", errConnRefused{})

	if !strings.Contains(err.Error(), "schema init") {
		t.Fatalf("memgraph error must preserve raw context:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "docker compose -f "+compose+" up -d memgraph") {
		t.Fatalf("memgraph error must carry recovery hint:\n%s", err.Error())
	}
	// Must be a service-class ExitError so exit codes stay unchanged.
	var ee *ExitError
	if !asExitError(err, &ee) {
		t.Fatalf("memgraphDownErr should return an *ExitError, got %T", err)
	}
	if ee.Code != ExitService {
		t.Fatalf("memgraphDownErr exit code = %d, want %d (ExitService)", ee.Code, ExitService)
	}
}

// --- test error doubles ---

type errConnRefused struct{}

func (errConnRefused) Error() string {
	return "ollama API call failed: dial tcp 127.0.0.1:11434: connect: connection refused"
}

type errDimMismatch struct{}

func (errDimMismatch) Error() string {
	return `embedding dimension mismatch: model "x" returned 384 dimensions, expected 768`
}

func asExitError(err error, target **ExitError) bool {
	if ee, ok := err.(*ExitError); ok {
		*target = ee
		return true
	}
	return false
}
