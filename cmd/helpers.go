package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/store"
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

// storeDownErr returns an ExitError with ExitStoreDown for use by write
// commands when Qdrant is unreachable.
func storeDownErr() error {
	return &ExitError{
		Code:    ExitStoreDown,
		Message: withServiceHint("vector store unavailable — writes blocked, reads served from cache", svcVectorStore),
	}
}

type deps struct {
	store      *store.Client
	embedder   *embedding.Generator
	qdrantDown bool // true when Qdrant is unreachable (connection refused / DNS failure)
	qdrantSlow bool // true when Qdrant health check timed out — reachable but slow
}

// resolveEmbeddingDim is the single seam that keeps the embedding-meta.json guard
// and the Generator's expectedDim in agreement. It resolves the authoritative dim
// and validates it against stored meta via CheckMeta — which also records the meta
// on first run. Returning ONE dim for both guard and generator prevents the false
// ErrModelMismatch loop a non-768 Ollama model used to trigger.
//
// On Ollama first run (no meta yet) this probes the live model. If the probe fails
// (Ollama down / model not pulled), it uses the 768 fallback for the in-process dim
// but does NOT call CheckMeta — so it does not stamp meta{model,768} with a wrong
// default that would cause a spurious mismatch once the model becomes available.
func resolveEmbeddingDim(cfg *config.Config, ggDir string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	var (
		dim         int
		authorative bool // false = probe failed, skip writing meta
	)

	if cfg.Embedding.ResolvedBackendName() == config.BackendVoyage {
		// Voyage: always deterministic — no meta or network needed.
		dim = embedding.EffectiveDim(ctx, &cfg.Embedding, ggDir, store.VectorSize)
		authorative = true
	} else {
		// Ollama: read existing meta first (no network, authoritative in steady state).
		if meta, err := embedding.ReadMeta(ggDir); err == nil && meta != nil && meta.Dim > 0 {
			dim = meta.Dim
			authorative = true
		} else {
			// First run — probe the live model so we record its true dim.
			// If the probe fails (Ollama unreachable / model not pulled), use the
			// 768 fallback but DON'T write meta: stamping meta{model,768} for a
			// 1024-dim model would cause a spurious mismatch on the next run.
			vec, probeErr := embedding.New(&cfg.Embedding, 0).Generate(ctx, "dimension probe")
			if probeErr == nil && len(vec) > 0 {
				dim = len(vec)
				authorative = true
			} else {
				dim = store.VectorSize // fallback; meta not written
				fmt.Fprintf(os.Stderr, "warning: embedding model unreachable — using %d-dim fallback; start Ollama and pull the model, then re-run or run `gg reembed`\n", dim)
			}
		}
	}

	// Only validate/write meta when the dim is authoritative. Skipping CheckMeta
	// on probe failure prevents persisting a wrong dim that would need `gg reembed`.
	if authorative {
		if err := embedding.CheckMeta(ggDir, embedding.EffectiveModelIdentity(&cfg.Embedding), dim); err != nil {
			return 0, err
		}
	}
	return dim, nil
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

	// Resolve the authoritative embedding dim and fail-fast if the configured
	// model/backend differs from the one the collections were built with — mixed-model
	// collections give broken recall (vectors from different models aren't comparable).
	dim, err := resolveEmbeddingDim(cfg, ggDir)
	if err != nil {
		return nil, err
	}

	client, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	d = &deps{store: client}
	defer func() {
		if err != nil {
			d.Close()
		}
	}()

	// Fail fast with a structured exit code if Qdrant is unreachable — write
	// commands must not silently succeed when the vector store is down.
	hctx, hcancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer hcancel()
	if hErr := client.HealthCheck(hctx); hErr != nil {
		return nil, storeDownErr()
	}

	if needEmbedding {
		d.embedder = embedding.New(&cfg.Embedding, dim)
	}
	return d, nil
}

// loadDepsOfflineSafe is like loadDepsReadOnly but intended for brain-write
// commands (record, task create, bug report) that implement JSONL-first writes.
// When Qdrant is down, d.qdrantDown=true is set and the caller writes to JSONL,
// queues an outbox entry, and returns exit 0 with a stderr note.
//
// Embedding is always required for write commands (the caller must generate a
// vector for the Qdrant upsert attempt, even if Qdrant is down, because the
// outbox replay needs it stored).  Pass needEmbedding=true.
func loadDepsOfflineSafe(needEmbedding bool) (d *deps, err error) {
	return loadDepsReadOnly(needEmbedding)
}

// loadDepsReadOnly is like loadDeps but tolerates Qdrant being unreachable.
// It sets d.qdrantDown=true instead of returning an error when the health
// check fails. Callers must print a degraded-mode banner and return empty
// results rather than propagating store errors.
//
// Use this for read-only commands (search, context) that should degrade
// gracefully. Write commands must use loadDeps, which hard-fails on Qdrant down.
func loadDepsReadOnly(needEmbedding bool) (d *deps, err error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return nil, err
	}

	dim, err := resolveEmbeddingDim(cfg, ggDir)
	if err != nil {
		return nil, err
	}

	client, err := store.New(ggDir, cfg.ProjectID)
	if err != nil {
		return nil, err
	}
	d = &deps{store: client}
	defer func() {
		if err != nil {
			d.Close()
		}
	}()

	hctx, hcancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer hcancel()
	if hErr := client.HealthCheck(hctx); hErr != nil {
		if isHealthTimeout(hErr) {
			d.qdrantSlow = true
		} else {
			d.qdrantDown = true
		}
	}

	if needEmbedding {
		d.embedder = embedding.New(&cfg.Embedding, dim)
	}
	return d, nil
}

// isHealthTimeout reports whether a health-check error is a timeout rather
// than a true connectivity failure. Timeouts mean Qdrant is reachable but slow;
// connectivity failures mean Qdrant is down.
func isHealthTimeout(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "deadline exceeded")
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

// addFromFlag attaches a --from flag. Runtime env is resolved in resolveAuthor
// so help/docs output remains deterministic across agent shells.
func addFromFlag(cmd *cobra.Command) {
	cmd.Flags().String("from", "", "author/role recording this (defaults to $GG_ROLE)")
}

// printProjectBanner prints a single-line "Recording to project: <name> (<uuid8>)"
// banner before write operations so users always know which project is receiving
// the data. Gated by GG_QUIET=1 for scripting / CI contexts.
func printProjectBanner() {
	if os.Getenv("GG_QUIET") == "1" {
		return
	}
	root, err := config.FindRoot()
	if err != nil {
		return
	}
	cfg, err := config.Load()
	if err != nil {
		return
	}
	name := filepath.Base(root)
	uuid8 := cfg.ProjectID
	if len(uuid8) > 8 {
		uuid8 = uuid8[:8]
	}
	fmt.Printf("→ Recording to project: %s (%s)\n", name, uuid8)
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
