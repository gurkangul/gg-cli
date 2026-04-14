package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg/internal/config"
	"github.com/gurkangul/gg/internal/embedding"
	"github.com/gurkangul/gg/internal/store"
)

const (
	cmdTimeout         = 10 * time.Second
	healthCheckTimeout = 5 * time.Second
)

// withTimeout derives a timeout-scoped context from the parent (usually
// cmd.Context()), so Ctrl+C still cancels in-flight work.
func withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, cmdTimeout)
}

type deps struct {
	store    *store.Client
	embedder *embedding.Generator
}

func loadDeps(needEmbedding bool) (d *deps, err error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return nil, err
	}
	client, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	d = &deps{store: client}
	defer func() {
		if err != nil {
			d.Close()
		}
	}()

	// Fail fast with a clear message if Qdrant is unreachable — much better
	// than a cryptic gRPC error bubbling up from inside a store operation.
	hctx, hcancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer hcancel()
	if hErr := client.HealthCheck(hctx); hErr != nil {
		return nil, fmt.Errorf("qdrant health check failed (is Qdrant running?): %w", hErr)
	}

	if needEmbedding {
		d.embedder = embedding.New(&cfg.Embedding, store.VectorSize)
	}
	return d, nil
}

func (d *deps) Close() {
	if d == nil || d.store == nil {
		return
	}
	if closeErr := d.store.Close(); closeErr != nil {
		fmt.Fprintln(os.Stderr, "warning: store close:", closeErr)
	}
}

func parseTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			result = append(result, t)
		}
	}
	return result
}

func requireNonEmpty(name, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty", name)
	}
	// Strip accidental double-dash prefix that agents sometimes produce when
	// generating titles (e.g. "--fix broken auth" → "fix broken auth").
	// This handles TASK-009: CLI double-dash prefix handling.
	trimmed = strings.TrimPrefix(trimmed, "--")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty after stripping leading '--'", name)
	}
	return trimmed, nil
}

// normalizeTaskRef validates an optional TASK-ID flag value, uppercases and
// trims it. Empty input returns "" with no error.
func normalizeTaskRef(raw string) (string, error) {
	t := strings.ToUpper(strings.TrimSpace(raw))
	if t == "" {
		return "", nil
	}
	if _, err := store.ParseTaskID(t); err != nil {
		return "", err
	}
	return t, nil
}

// requireTaskID validates a required TASK-ID positional argument. Empty or
// malformed input is an error.
// resolveAuthor returns the --from flag value if set, falling back to the
// GG_ROLE environment variable. Returns "" if neither is set.
func resolveAuthor(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("from"); f != nil && f.Changed {
		return strings.TrimSpace(f.Value.String())
	}
	return strings.TrimSpace(os.Getenv("GG_ROLE"))
}

// addFromFlag attaches a --from flag to the command with a default from GG_ROLE.
func addFromFlag(cmd *cobra.Command) {
	defaultRole := strings.TrimSpace(os.Getenv("GG_ROLE"))
	cmd.Flags().String("from", defaultRole, "author/role recording this (defaults to $GG_ROLE)")
}

func requireTaskID(raw string) (string, error) {
	t := strings.ToUpper(strings.TrimSpace(raw))
	if t == "" {
		return "", fmt.Errorf("task ID is required (expected TASK-NNN)")
	}
	if _, err := store.ParseTaskID(t); err != nil {
		return "", err
	}
	return t, nil
}
