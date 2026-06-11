package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/config"
)

// Service identifiers for serviceRecoveryHint. These match the service names in
// the shared docker-compose.yaml (internal/templates/docker-compose.yaml).
const (
	svcQdrant   = "qdrant"
	svcMemgraph = "memgraph"
	svcOllama   = "ollama"
)

// composePathForHint returns the path to the shared docker-compose.yaml that gg
// provisions in ~/.gg/ (see cmd/init.go startSharedServices). When the home
// directory cannot be resolved we fall back to the canonical literal so the hint
// is still actionable rather than empty.
func composePathForHint() string {
	sharedDir, err := config.SharedDir()
	if err != nil {
		return "~/.gg/docker-compose.yaml"
	}
	return filepath.Join(sharedDir, "docker-compose.yaml")
}

// serviceRecoveryHint returns a consistent, actionable recovery hint for a
// backing service that is down or unreachable. The hint names the exact
// docker-compose commands a user can run to bring the service back up and to
// tail its logs. All three services (Qdrant, Memgraph, Ollama) are provisioned
// by the same shared compose file, so the recovery shape is identical.
//
// service must be one of svcQdrant, svcMemgraph, svcOllama. An unknown service
// yields a generic compose-up hint (never a panic) so new call sites degrade
// safely rather than swallowing the situation.
func serviceRecoveryHint(service string) string {
	compose := composePathForHint()
	svc := strings.ToLower(strings.TrimSpace(service))

	var label string
	switch svc {
	case svcQdrant:
		label = "Qdrant"
	case svcMemgraph:
		label = "Memgraph"
	case svcOllama:
		label = "Ollama"
	default:
		// Unknown service: still give the generic compose-up shape.
		return fmt.Sprintf(
			"A backing service is unreachable. Start it:\n"+
				"  docker compose -f %s up -d\n"+
				"  Logs:  docker compose -f %s logs -f",
			compose, compose,
		)
	}

	return fmt.Sprintf(
		"%s is unreachable. Start it:\n"+
			"  docker compose -f %s up -d %s\n"+
			"  Logs:  docker compose -f %s logs -f %s",
		label, compose, svc, compose, svc,
	)
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
	return serviceErr(withServiceHint(raw, svcMemgraph))
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
