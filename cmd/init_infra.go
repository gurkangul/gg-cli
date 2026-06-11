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
	fmt.Println("Waiting for Qdrant...")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load freshly-written project config: %w", err)
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
		fmt.Println("⚠ Qdrant not ready — collections will be created on first use")
		return nil
	}
	defer func() { _ = client.Close() }()

	setupCtx, cancelSetup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelSetup()
	if err := client.EnsureCollections(setupCtx, store.VectorSize); err != nil {
		return fmt.Errorf("setup collections: %w", err)
	}
	fmt.Printf("✓ Qdrant collections ready for project %s\n", projectID)
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
