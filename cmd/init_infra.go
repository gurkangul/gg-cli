package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
)

// infraStatus summarizes what provisionInfra found/did so init can print
// accurate guidance and decide whether indexing is feasible.
type infraStatus struct {
	EmbedBackend string // resolved: "ollama" or "voyage"
	// GraphReady is true when the graph store is usable for `gg index`. The
	// embedded SQLite graph is always ready (a local file created on first use).
	GraphReady bool
	// EmbedReady is true when embeddings can be produced now (Ollama reachable, or
	// Voyage selected). false ⇒ init printed warn-with-guidance, not a hard fail.
	EmbedReady bool
}

// provisionInfra prepares the embedded stores for a freshly-initialized project.
// There is NO Docker bring-up: the vector store (.gg/vectorstore.db) and code
// graph (.gg/graph.db) are embedded SQLite files created on first use. The only
// optional external service is the embedding engine; an unreachable native
// Ollama endpoint produces a warning with install guidance — it never hard-fails
// init.
//
// It loads the freshly-written project config to resolve the embedding backend.
// On a config-load error it degrades to the built-in defaults so init still
// completes.
func provisionInfra(ctx context.Context, ggDir string) (*config.Config, infraStatus) {
	cfg, err := config.LoadFromGGDir(ggDir)
	if err != nil {
		fmt.Printf("⚠ Could not load project config for provisioning: %v\n", err)
		cfg = config.DefaultConfig()
	}
	st := infraStatus{
		EmbedBackend: cfg.Embedding.Backend,
		GraphReady:   true, // embedded graph is always ready
	}

	fmt.Println()
	fmt.Println("Storage:")
	fmt.Printf("  embeddings: %s\n", st.EmbedBackend)
	fmt.Println("  ✓ embedded vector store (.gg/vectorstore.db) — no Docker needed")
	fmt.Println("  ✓ embedded code graph (.gg/graph.db) — no Docker needed")

	// Embeddings: warn-not-fail. Ollama (default) is probed; Voyage skips the
	// probe and just notes the API-key requirement.
	st.EmbedReady = provisionEmbeddings(ctx, cfg)
	return cfg, st
}

// provisionEmbeddings verifies the configured embedding backend is usable, but
// NEVER fails init. Under Ollama it probes the endpoint and, when unreachable,
// prints native-install guidance (brew install ollama / ollama serve / pull) OR
// the Voyage alternative. Under Voyage it confirms the API-key env var is set.
// Returns true when embeddings can be produced now (informational only).
func provisionEmbeddings(ctx context.Context, cfg *config.Config) bool {
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
		fmt.Printf("      ollama pull %s          # configured model (override per-shell with GG_EMBED_MODEL)\n", embedding.EffectiveModel(&cfg.Embedding))
		fmt.Println("    Or use the Voyage cloud backend: set embedding.backend: voyage in .gg/config.yaml + export VOYAGE_API_KEY.")
		fmt.Println("    Then populate the embedded vector store: gg reembed")
		return false
	}
	fmt.Printf("  ✓ Ollama reachable at %s\n", host)
	return true
}

// printInfraSummary prints the closing status + migration hints. For an existing
// user it points at the one-time populate commands (gg reembed for vectors,
// gg index for the code graph).
func printInfraSummary(projectID string, cfg *config.Config, st infraStatus) {
	_ = cfg
	_ = st
	fmt.Println()
	fmt.Printf("GG ready — project %s registered.\n", projectID)
	fmt.Println("Embedded stores are used by default (no Docker). To populate them from the")
	fmt.Println("committed JSONL brain (existing project) or build the code graph, run:")
	fmt.Println("  gg reembed                 # build .gg/vectorstore.db from .gg/brain/*.jsonl")
	fmt.Println("  gg index --lang <go|typescript|python|swift>   # build .gg/graph.db (code graph)")
}

func setupProjectCollections(ctx context.Context, projectID, ggDir string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load freshly-written project config: %w", err)
	}
	fmt.Println("Creating embedded vector store collections...")
	var client *store.Client
	var healthErr error
	for i := 0; i < 15; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		client, healthErr = store.New(ggDir, projectID)
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
		fmt.Println("⚠ embedded vector store not ready — collections will be created on first use")
		return nil
	}
	defer func() { _ = client.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	// Size the collections to the active embedding backend's dimension (audit
	// EMB-1) so vectors are not silently truncated/rejected at insert time. On a
	// fresh project this probes the configured model once, so a non-768 model
	// (e.g. qwen3-embedding:0.6b=1024) gets correctly-sized collections.
	dim := embedding.EffectiveDim(setupCtx, &cfg.Embedding, ggDir, store.VectorSize)
	if err := client.EnsureCollections(setupCtx, uint64(dim)); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Printf("✓ embedded vector store collections ready for project %s\n", projectID)
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
