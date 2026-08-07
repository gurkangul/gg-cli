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
	"github.com/gurkangul/gg-cli/internal/identity"
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

// resolveEmbedding is the single seam that keeps the embedding-meta.json guard,
// the query-embedding model, and the Generator's expectedDim in agreement. It
// resolves the corpus-aligned embedding config plus the authoritative dim and
// validates both against stored meta via CheckMeta — which also records the meta
// on first run. Returning ONE config+dim for guard and generator prevents the
// false ErrModelMismatch loop an off-default-dim Ollama model used to trigger, and stops a
// drifted config.yaml from embedding queries in a different vector space than the
// stored corpus (TASK-516).
//
// On Ollama first run (no meta yet) this probes the live model. If the probe fails
// (Ollama down / model not pulled), it uses store.VectorSize for the in-process dim
// but does NOT call CheckMeta — so it does not stamp meta with the default's dim,
// which would cause a spurious mismatch once the real model becomes available.
func resolveEmbedding(cfg *config.Config, ggDir string) (config.EmbeddingConfig, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()

	// TASK-516: align the query-embedding model to the model the corpus was
	// actually built with BEFORE anything else reads it, so the CheckMeta guard
	// and the Generator can never disagree — and so a config.yaml that has
	// drifted from embedding-meta.json no longer forces the operator to export
	// GG_EMBED_MODEL in every shell just to get recall back.
	embCfg, aligned := embedding.CorpusAlignedConfig(&cfg.Embedding, ggDir)
	if aligned && os.Getenv("GG_QUIET") != "1" {
		fmt.Fprintf(os.Stderr,
			"note: embedding model resolved from embedding-meta.json (corpus model %q); config.yaml says %q — queries use the corpus model so recall stays valid (align config.yaml, or run `gg reembed` to migrate)\n",
			embCfg.Model, cfg.Embedding.Model)
	}

	var (
		dim         int
		authorative bool // false = probe failed, skip writing meta
	)

	if embCfg.ResolvedBackendName() == config.BackendVoyage {
		// Voyage: always deterministic — no meta or network needed.
		dim = embedding.EffectiveDim(ctx, &embCfg, ggDir, store.VectorSize)
		authorative = true
	} else {
		// Ollama: read existing meta first (no network, authoritative in steady state).
		if meta, err := embedding.ReadMeta(ggDir); err == nil && meta != nil && meta.Dim > 0 {
			dim = meta.Dim
			authorative = true
		} else {
			// First run — probe the live model so we record its true dim.
			// If the probe fails (Ollama unreachable / model not pulled), use the
			// store.VectorSize fallback but DON'T write meta: stamping the
			// default's dim for a model of a different size would cause a
			// spurious mismatch on the next run.
			vec, probeErr := embedding.New(&embCfg, 0).Generate(ctx, "dimension probe")
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
		if err := embedding.CheckMeta(ggDir, embedding.EffectiveModelIdentity(&embCfg), dim); err != nil {
			return embCfg, 0, err
		}
	}
	return embCfg, dim, nil
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
	embCfg, dim, err := resolveEmbedding(cfg, ggDir)
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
		d.embedder = embedding.New(&embCfg, dim)
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

	embCfg, dim, err := resolveEmbedding(cfg, ggDir)
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
		d.embedder = embedding.New(&embCfg, dim)
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

// resolveAuthor resolves the provenance stamped on a durable write.
// Ladder: --from → $GG_ROLE → the runtime's agent identity.
//
// BUG-106: the ladder used to stop at GG_ROLE, so any runtime that never
// exported a role wrote author="" — silently, and indistinguishably from a
// record whose author simply was not printed. gg already knows who is calling:
// gg init writes GG_AGENT into the runtime's env and internal/identity sharpens
// it into a per-tab id (BUG-084/BUG-103). The task lifecycle has consumed that
// exact ladder since BUG-084 (resolveTaskOwner), so the same process in the same
// second attributed a task event to "claude-code-<sid>" and a decision to nobody.
//
// Role stays ahead of agent id deliberately: when an operator exports GG_ROLE,
// that role IS the provenance they mean ("master", "reviewer"); the agent id is
// merely the runtime that happened to execute the command. An explicitly empty
// --from is not a provenance statement either, so it falls through to the rest
// of the ladder rather than short-circuiting to "".
func resolveAuthor(cmd *cobra.Command) string {
	if f := cmd.Flags().Lookup("from"); f != nil && f.Changed {
		if from := strings.TrimSpace(f.Value.String()); from != "" {
			return from
		}
	}
	return resolveAuthorEnv()
}

// resolveAuthorEnv is the environment half of the ladder, for durable writes
// that have no *cobra.Command to read --from from (e.g. the bypass-rationale
// record written by emitGuardSkipEvent). It exists so those paths cannot drift
// into a private ladder of their own — which is exactly how gsdguard ended up
// reading raw GG_AGENT and missing the per-tab identity sharpening.
func resolveAuthorEnv() string {
	if role := strings.TrimSpace(os.Getenv("GG_ROLE")); role != "" {
		return role
	}
	return strings.TrimSpace(identity.Agent())
}

// requireAuthor is resolveAuthor plus the opt-in strict policy. Projects that
// adopted a written provenance convention set GG_REQUIRE_AUTHOR=1 so an
// unattributable write fails loudly instead of landing anonymously in the
// ledger. Default off, because after the resolveAuthor ladder an empty author
// means a bare human shell with no role and no agent env — and neither a human
// nor CI may be broken by a convention they never opted into.
func requireAuthor(cmd *cobra.Command) (string, error) {
	author := resolveAuthor(cmd)
	if author != "" || !strictAuthorRequired() {
		return author, nil
	}
	return "", fmt.Errorf("author could not be resolved and GG_REQUIRE_AUTHOR is set: " +
		"pass --from <role>, or export GG_ROLE / GG_AGENT")
}

// strictAuthorRequired reports whether GG_REQUIRE_AUTHOR asks for a hard failure
// on an unattributable write.
func strictAuthorRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_REQUIRE_AUTHOR"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// anonymousAuthorLabel marks a record whose provenance could not be resolved.
const anonymousAuthorLabel = "[anonymous]"

// authorLabel renders an author for display — never silently.
//
// BUG-106: an empty author used to render as omission (every call site guarded
// on `Author != ""`), so an anonymous record was indistinguishable from one
// whose author simply was not printed by that view. gg already prints an
// explicit marker for its other missing-provenance signal — absent evidence
// renders "[unverified]" via trustLabel — and author was the lone exception.
func authorLabel(author string) string {
	if a := strings.TrimSpace(author); a != "" {
		return a
	}
	return anonymousAuthorLabel
}

// addFromFlag attaches a --from flag. Runtime env is resolved in resolveAuthor
// so help/docs output remains deterministic across agent shells.
func addFromFlag(cmd *cobra.Command) {
	cmd.Flags().String("from", "", "author/role recording this (defaults to $GG_ROLE, then the agent identity)")
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

// requireTaskID validates a required TASK-ID positional argument. Empty or
// malformed input is an error.
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
