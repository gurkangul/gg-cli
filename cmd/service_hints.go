package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
)

// Service identifiers for serviceRecoveryHint. The vector store and code graph
// are embedded SQLite files (.gg/vectorstore.db / .gg/graph.db); only Ollama is
// a real external service.
const (
	svcVectorStore = "vector store" // embedded vector store (.gg/vectorstore.db)
	svcCodeGraph   = "code graph"   // embedded code graph (.gg/graph.db)
	svcOllama      = "ollama"       // external embedding engine
)

// ollamaComposePath returns the path to the OPTIONAL Ollama-in-Docker compose
// file gg writes in ~/.gg/. When the home directory cannot be resolved we fall
// back to the canonical literal so the hint is still actionable rather than
// empty.
func ollamaComposePath() string {
	sharedDir, err := config.SharedDir()
	if err != nil {
		return "~/.gg/docker-compose.yaml"
	}
	return filepath.Join(sharedDir, "docker-compose.yaml")
}

// serviceRecoveryHint returns a consistent, actionable recovery hint for a
// backing service that is down or unreachable. The vector store and code graph
// are local SQLite files, so their hint points at disk/permissions + gg doctor;
// Ollama is the only true external service and offers Docker/native/cloud
// options.
//
// service must be one of svcVectorStore, svcCodeGraph, svcOllama. An unknown service
// yields a generic local-store hint (never a panic) so new call sites degrade
// safely rather than swallowing the situation.
func serviceRecoveryHint(service string) string {
	svc := strings.ToLower(strings.TrimSpace(service))

	// Ollama is the embedding engine, not a stored-data backend: it can run in
	// Docker, natively, or be replaced by the Voyage cloud backend. Offer all
	// three so the hint is correct regardless of how the user runs embeddings.
	if svc == svcOllama {
		compose := ollamaComposePath()
		return fmt.Sprintf(
			"Ollama is unreachable. Restore embeddings any one way:\n"+
				"  Native:  brew install ollama && ollama serve && ollama pull <embedding.model>   # e.g. qwen3-embedding:0.6b; GG_EMBED_MODEL overrides per-shell\n"+
				"  Cloud:   set embedding.backend: voyage in .gg/config.yaml + export VOYAGE_API_KEY\n"+
				"  Docker:  docker compose -f %s up -d ollama",
			compose,
		)
	}

	switch svc {
	case svcVectorStore:
		return "The embedded vector store at .gg/vectorstore.db could not be opened.\n" +
			"  Check disk space and file permissions on .gg/, then run: gg doctor"
	case svcCodeGraph:
		return "The embedded code graph at .gg/graph.db could not be opened.\n" +
			"  Check disk space and file permissions on .gg/, then run: gg doctor"
	default:
		return "The embedded store under .gg/ could not be opened.\n" +
			"  Check disk space and file permissions on .gg/, then run: gg doctor"
	}
}

// withServiceHint appends the recovery hint for service onto an existing
// human-readable message, separated by a newline. It is the single formatter
// used at the down-service choke points so the raw error and the hint always
// travel together in a consistent shape.
func withServiceHint(msg, service string) string {
	return msg + "\n" + serviceRecoveryHint(service)
}

// memgraphDownErr wraps a Memgraph connectivity failure (raw err) in a service
// ExitError carrying the raw error plus the recovery hint. Used at the index /
// code-graph choke points where Memgraph being down hard-fails the command.
func memgraphDownErr(context string, err error) error {
	raw := fmt.Sprintf("%s: %v", context, err)
	return serviceErr(withServiceHint(raw, svcCodeGraph))
}

// isOllamaConnectivityErr reports whether err looks like Ollama being down /
// unreachable (the embedding HTTP endpoint refusing or failing the connection)
// rather than a model/dimension error. Used to decide whether to attach the
// Ollama recovery hint at the embedding choke points.
func isOllamaConnectivityErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, p := range []string{
		"connection refused",
		"no such host",
		"network is unreachable",
		"dial tcp",
		"connection reset",
		"eof",
		"ollama api call failed",
		"no route to host",
		"timeout",
		"timed out",
	} {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// embedErr wraps an embedding-generation failure. When the failure looks like
// Ollama being unreachable it attaches the Ollama recovery hint; otherwise it
// returns the raw error unchanged (model/dimension errors are not service
// outages and must not be misattributed to a down service).
func embedErr(context string, err error) error {
	raw := fmt.Sprintf("%s: %v", context, err)
	if isOllamaConnectivityErr(err) {
		return fmt.Errorf("%s", withServiceHint(raw, svcOllama))
	}
	return fmt.Errorf("%s", raw)
}
