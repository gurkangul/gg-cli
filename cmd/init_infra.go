package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// infraStatus summarizes what provisionInfra found/did so init can print
// accurate, backend-aware guidance and decide whether indexing is feasible.
type infraStatus struct {
	VectorBackend string // resolved: "sqlite" (embedded) or "qdrant" (server)
	GraphBackend  string // resolved: "sqlite" (embedded) or "memgraph" (server)
	EmbedBackend  string // resolved: "ollama" or "voyage"
	// GraphReady is true when the graph store is usable for `gg index`: always
	// for the embedded backend; for the server backend, only when Memgraph is up.
	GraphReady bool
	// EmbedReady is true when embeddings can be produced now (Ollama reachable, or
	// Voyage selected). false ⇒ init printed warn-with-guidance, not a hard fail.
	EmbedReady bool
	// ComposeUp records whether a Docker compose bring-up was attempted AND
	// succeeded — only relevant when a server backend is selected.
	ComposeUp bool
	// NeedsServers is true when any server backend (qdrant/memgraph) is selected,
	// i.e. Docker is actually required for this project.
	NeedsServers bool
}

// provisionInfra is the backend-aware replacement for the old unconditional
// `docker compose up`. Docker is now OPTIONAL: it is only brought up when a
// server backend (qdrant/memgraph) is explicitly selected. For the embedded
// default it does no Docker work at all — the SQLite stores are created on first
// use. Embeddings never hard-fail init: an unreachable native Ollama endpoint
// produces a warning with install guidance.
//
// It loads the freshly-written project config to resolve the effective backends
// (env > config field > sqlite). On a config-load error it degrades to the
// built-in defaults so init still completes.
func provisionInfra(ctx context.Context, composePath, ggDir string) (*config.Config, infraStatus) {
	cfg, err := config.LoadFromGGDir(ggDir)
	if err != nil {
		fmt.Printf("⚠ Could not load project config for backend provisioning: %v\n", err)
		cfg = config.DefaultConfig()
	}
	st := infraStatus{
		VectorBackend: cfg.Qdrant.ResolveBackend(),
		GraphBackend:  cfg.Memgraph.ResolveBackend(),
		EmbedBackend:  cfg.Embedding.Backend,
		GraphReady:    cfg.Memgraph.UsesEmbedded(), // embedded graph is always ready
	}
	st.NeedsServers = !cfg.Qdrant.UsesEmbedded() || !cfg.Memgraph.UsesEmbedded()

	fmt.Println()
	fmt.Println("Storage backends:")
	fmt.Printf("  vector: %s   graph: %s   embeddings: %s\n", st.VectorBackend, st.GraphBackend, st.EmbedBackend)
	if cfg.Qdrant.UsesEmbedded() {
		fmt.Println("  ✓ embedded vector store (.gg/vectorstore.db) — no Docker needed")
	}
	if cfg.Memgraph.UsesEmbedded() {
		fmt.Println("  ✓ embedded graph store (.gg/graph.db) — no Docker needed")
	}

	// Only touch Docker when a server backend is actually selected.
	if st.NeedsServers {
		fmt.Println("\nServer backend selected — bringing up Docker services...")
		st.ComposeUp = startSharedServices(ctx, composePath)
		if !cfg.Memgraph.UsesEmbedded() {
			st.GraphReady = st.ComposeUp
		}
		if !st.ComposeUp {
			fmt.Println("⚠ Server backend selected but Docker services are not up.")
			fmt.Println("  Start them with: docker compose -f ~/.gg/docker-compose.yaml up -d")
			fmt.Println("  Or switch to the embedded default: set qdrant.backend: sqlite / memgraph.backend: sqlite in .gg/config.yaml")
		}
	}

	// Embeddings: warn-not-fail. Ollama (default) is probed; Voyage skips the
	// probe and just notes the API-key requirement.
	st.EmbedReady = provisionEmbeddings(ctx, composePath, cfg, st.ComposeUp)
	return cfg, st
}

// provisionEmbeddings verifies the configured embedding backend is usable, but
// NEVER fails init. Under Ollama it probes the endpoint and, when unreachable,
// prints native-install guidance (brew install ollama / ollama serve / pull) OR
// the Voyage alternative. Under Voyage it confirms the API-key env var is set.
// Returns true when embeddings can be produced now (informational only).
func provisionEmbeddings(ctx context.Context, composePath string, cfg *config.Config, composeUp bool) bool {
	fmt.Println("\nEmbeddings:")
	if cfg.Embedding.ResolvedBackendName() == config.BackendVoyage {
		keyEnv := cfg.Embedding.Voyage.APIKeyEnv
		if keyEnv == "" {
			keyEnv = config.DefaultVoyageAPIKeyEnv
		}
		if strings.TrimSpace(os.Getenv(keyEnv)) == "" {
			fmt.Printf("  ⚠ Voyage backend selected but %s is not set — export it before `gg reembed`.\n", keyEnv)
			return false
		}
		fmt.Printf("  ✓ Voyage backend configured (%s present)\n", keyEnv)
		return true
	}

	// Ollama path.
	host := cfg.Embedding.Host
	tagsURL := strings.TrimRight(host, "/") + "/api/tags"
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := waitForHTTP(probeCtx, tagsURL, 3*time.Second); err != nil {
		fmt.Printf("  ⚠ Ollama not reachable at %s — embeddings unavailable until you set one up.\n", host)
		fmt.Println("    Native Ollama (recommended):")
		fmt.Println("      brew install ollama          # or see https://ollama.com/download")
		fmt.Println("      ollama serve &")
		fmt.Println("      ollama pull nomic-embed-text")
		fmt.Println("    Or use the Voyage cloud backend: set embedding.backend: voyage in .gg/config.yaml + export VOYAGE_API_KEY.")
		fmt.Println("    Then populate the embedded vector store: gg reembed")
		return false
	}
	fmt.Printf("  ✓ Ollama reachable at %s\n", host)
	// If we have a running Docker Ollama, pull the model; otherwise the native
	// install is the user's responsibility (the warning above guides them).
	if composeUp {
		pullOllamaModel(ctx, composePath, host)
	}
	return true
}

// printInfraSummary prints the closing backend-aware status + migration hints.
// For an existing user switching to the embedded default, it points at the
// one-time populate commands (gg reembed for vectors, gg index for the graph).
func printInfraSummary(projectID string, cfg *config.Config, st infraStatus) {
	fmt.Println()
	fmt.Printf("GG ready — project %s registered.\n", projectID)
	if cfg.Qdrant.UsesEmbedded() || cfg.Memgraph.UsesEmbedded() {
		fmt.Println("Embedded stores are used by default (no Docker). To populate them from the")
		fmt.Println("committed JSONL brain (existing project) or build the code graph, run:")
		if cfg.Qdrant.UsesEmbedded() {
			fmt.Println("  gg reembed                 # build .gg/vectorstore.db from .gg/brain/*.jsonl")
		}
		fmt.Println("  gg index --lang <go|typescript|python|swift>   # build .gg/graph.db (code graph)")
	}
	if st.NeedsServers && !st.ComposeUp {
		fmt.Println("Server backend(s) selected but not up — start them, or flip to the embedded default.")
	}
}

// startSharedServices brings the shared Docker stack up. Before invoking
// `docker compose up` it probes the Docker daemon (AC-1) so a stopped daemon /
// missing binary fails fast with an actionable hint instead of a slow compose
// timeout. When compose itself fails, the REAL underlying stderr is surfaced
// (not a generic "start manually" line). Returns true only when the stack came
// up; a false return is always accompanied by a printed, actionable reason.
func startSharedServices(ctx context.Context, composePath string) bool {
	switch res, detail := probeDockerDaemon(ctx); res {
	case dockerMissing:
		fmt.Println("⚠ " + dockerMissingMsg())
		return false
	case dockerDown:
		fmt.Println("⚠ " + dockerDaemonDownMsg(runtimeGOOS()))
		if strings.TrimSpace(detail) != "" {
			fmt.Println("  Docker reported: " + strings.TrimSpace(detail))
		}
		return false
	}

	fmt.Println("Starting shared Docker services...")
	composeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	compose := exec.CommandContext(composeCtx, "docker", "compose", "-f", composePath, "up", "-d")
	// Tee stderr so the user still sees compose progress live, but we also keep a
	// copy to surface the real failure cause (AC-1) when Run() returns an error.
	var stderrBuf bytes.Buffer
	compose.Stdout = os.Stdout
	compose.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	if err := compose.Run(); err != nil {
		raw := strings.TrimSpace(stderrBuf.String())
		if raw == "" {
			raw = err.Error()
		}
		fmt.Println("⚠ " + composeFailureMsg(composePath, raw))
		return false
	}
	fmt.Println("✓ Shared Docker services running")
	return true
}

func pullOllamaModel(ctx context.Context, composePath, ollamaHost string) {
	tagsURL := strings.TrimRight(ollamaHost, "/") + "/api/tags"
	if err := waitForHTTP(ctx, tagsURL, 60*time.Second); err != nil {
		fmt.Println("⚠ Ollama not reachable within 60s — pull model manually:")
		fmt.Println("  docker compose -f", composePath, "exec ollama ollama pull nomic-embed-text")
		return
	}
	fmt.Println("Pulling nomic-embed-text model (first time only, shared across all projects)...")
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	pull := exec.CommandContext(pullCtx, "docker", "compose", "-f", composePath,
		"exec", "-T", "ollama", "ollama", "pull", "nomic-embed-text")
	pull.Stdout = os.Stdout
	pull.Stderr = os.Stderr
	if err := pull.Run(); err != nil {
		fmt.Println("⚠ Model pull failed — retry manually: docker compose -f", composePath, "exec ollama ollama pull nomic-embed-text")
		return
	}
	fmt.Println("✓ nomic-embed-text model ready")
}

func setupProjectCollections(ctx context.Context, projectID, ggDir string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load freshly-written project config: %w", err)
	}
	embedded := cfg.Qdrant.UsesEmbedded()
	storeLabel := "Qdrant"
	if embedded {
		storeLabel = "embedded vector store"
		fmt.Println("Creating embedded vector store collections...")
	} else {
		fmt.Println("Waiting for Qdrant...")
	}
	var client *store.Client
	var healthErr error
	for i := 0; i < 15; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		client, healthErr = store.New(&cfg.Qdrant, ggDir, projectID)
		if healthErr == nil {
			hctx, hcancel := context.WithTimeout(ctx, 2*time.Second)
			healthErr = client.HealthCheck(hctx)
			hcancel()
			if healthErr == nil {
				break
			}
			_ = client.Close()
			client = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	if healthErr != nil || client == nil {
		fmt.Printf("⚠ %s not ready — collections will be created on first use\n", storeLabel)
		return nil
	}
	defer func() { _ = client.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	if err := client.EnsureCollections(setupCtx, store.VectorSize); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Printf("✓ %s collections ready for project %s\n", storeLabel, projectID)
	return nil
}

// waitForHTTP polls a URL with GET until a 2xx response or timeout.
func waitForHTTP(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for %s", url)
}
