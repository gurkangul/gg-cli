package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/config"
)

// expectedComposePath mirrors ollamaComposePath so the Ollama hint test asserts
// the real OPTIONAL Ollama-in-Docker compose path gg writes (~/.gg/docker-compose.yaml),
// derived — not hardcoded.
func expectedComposePath(t *testing.T) string {
	t.Helper()
	shared, err := config.SharedDir()
	if err != nil {
		return "~/.gg/docker-compose.yaml"
	}
	return filepath.Join(shared, "docker-compose.yaml")
}

// TestServiceRecoveryHint verifies the backend-neutral hints: the vector store and
// code graph are embedded SQLite files (disk/permissions + gg doctor), while Ollama
// is the only true external service and still offers a Docker option.
func TestServiceRecoveryHint(t *testing.T) {
	// Embedded local stores: hint points at disk/permissions + gg doctor, never a
	// "docker compose" command (there is no server to bring up).
	t.Run("vector store", func(t *testing.T) {
		got := serviceRecoveryHint(svcVectorStore)
		if !strings.Contains(got, "embedded vector store at .gg/vectorstore.db") {
			t.Fatalf("vector store hint missing embedded-store message:\n%s", got)
		}
		if !strings.Contains(got, "gg doctor") {
			t.Fatalf("vector store hint missing 'gg doctor':\n%s", got)
		}
		if strings.Contains(got, "docker compose") {
			t.Fatalf("embedded vector store hint must not suggest docker compose:\n%s", got)
		}
	})

	t.Run("vector-store-upper-case-and-padded", func(t *testing.T) {
		// Case-insensitive / padded input still resolves to the embedded-store hint.
		got := serviceRecoveryHint("  VECTOR STORE ")
		if !strings.Contains(got, "embedded vector store at .gg/vectorstore.db") {
			t.Fatalf("padded/upper vector store hint did not resolve:\n%s", got)
		}
	})

	t.Run("code graph", func(t *testing.T) {
		got := serviceRecoveryHint(svcCodeGraph)
		if !strings.Contains(got, "embedded code graph at .gg/graph.db") {
			t.Fatalf("code graph hint missing embedded-graph message:\n%s", got)
		}
		if !strings.Contains(got, "gg doctor") {
			t.Fatalf("code graph hint missing 'gg doctor':\n%s", got)
		}
		if strings.Contains(got, "docker compose") {
			t.Fatalf("embedded code graph hint must not suggest docker compose:\n%s", got)
		}
	})

	t.Run("ollama", func(t *testing.T) {
		compose := expectedComposePath(t)
		got := serviceRecoveryHint(svcOllama)
		if !strings.Contains(got, "Ollama is unreachable") {
			t.Fatalf("ollama hint missing label:\n%s", got)
		}
		wantUp := "docker compose -f " + compose + " up -d ollama"
		if !strings.Contains(got, wantUp) {
			t.Fatalf("ollama hint missing docker option %q:\n%s", wantUp, got)
		}
	})
}

func TestServiceRecoveryHintUnknownService(t *testing.T) {
	got := serviceRecoveryHint("postgres")

	// Unknown service must not panic and must still give an actionable
	// generic local-store hint (disk/permissions + gg doctor).
	if !strings.Contains(got, "embedded store under .gg/") {
		t.Fatalf("unknown-service hint missing generic local-store message:\n%s", got)
	}
	if !strings.Contains(got, "gg doctor") {
		t.Fatalf("unknown-service hint missing 'gg doctor':\n%s", got)
	}
}

func TestWithServiceHint(t *testing.T) {
	got := withServiceHint("Vector store unavailable — writes blocked", svcVectorStore)

	if !strings.HasPrefix(got, "Vector store unavailable — writes blocked\n") {
		t.Fatalf("withServiceHint must preserve the raw message first:\n%s", got)
	}
	if !strings.Contains(got, "embedded vector store at .gg/vectorstore.db") {
		t.Fatalf("withServiceHint missing embedded vector-store recovery hint:\n%s", got)
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
	err := memgraphDownErr("schema init", errConnRefused{})

	if !strings.Contains(err.Error(), "schema init") {
		t.Fatalf("memgraph error must preserve raw context:\n%s", err.Error())
	}
	if !strings.Contains(err.Error(), "embedded code graph at .gg/graph.db") {
		t.Fatalf("memgraph error must carry the embedded code-graph recovery hint:\n%s", err.Error())
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
