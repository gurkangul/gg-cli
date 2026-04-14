package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and repair gg configuration",
	Long: `Check service connectivity and verify that required indexer binaries
are available. Use --install-indexers to automatically install missing SCIP
binaries using their native package managers (go install, npm, pip).

Exit codes:
  0  all checks passed
  1  one or more checks failed`,
	RunE: runDoctor,
}

var (
	doctorInstallIndexers bool
	doctorReconcile       bool
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorInstallIndexers, "install-indexers", false,
		"install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)")
	doctorCmd.Flags().BoolVar(&doctorReconcile, "reconcile", false,
		"scan the outbox for incomplete dual-store writes and report what needs repair")
	rootCmd.AddCommand(doctorCmd)
}

// indexerSpec describes how to install a SCIP binary.
type indexerSpec struct {
	Binary  string   // name passed to runner.ResolveIndexer
	Install []string // command + args for native package manager
	Note    string   // displayed after a failed install
}

// indexers lists the SCIP binaries gg needs, with install commands.
var indexers = []indexerSpec{
	{
		Binary:  "scip-go",
		Install: []string{"go", "install", "github.com/sourcegraph/scip-go/cmd/scip-go@latest"},
		Note:    "requires Go 1.21+",
	},
	{
		Binary:  "scip-typescript",
		Install: []string{"npm", "install", "-g", "@sourcegraph/scip-typescript"},
		Note:    "requires Node.js 18+",
	},
	{
		Binary:  "scip-python",
		Install: []string{"pip", "install", "scip-python"},
		Note:    "requires Python 3.9+",
	},
}

// doctorReport accumulates check results and tracks whether any problems were found.
type doctorReport struct {
	problems int
}

func (r *doctorReport) ok(label, detail string) {
	fmt.Printf("  ✓ %-28s %s\n", label, detail)
}

func (r *doctorReport) warn(label, detail string) {
	fmt.Printf("  ~ %-28s %s\n", label, detail)
}

func (r *doctorReport) fail(label, detail string) {
	r.problems++
	fmt.Printf("  ✗ %-28s %s\n", label, detail)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	// --reconcile: scan the outbox and report pending writes; skip normal checks.
	if doctorReconcile {
		return runDoctorReconcile(cmd)
	}

	fmt.Println("GG Doctor")
	fmt.Println(strings.Repeat("─", 50))

	report := &doctorReport{}

	// 1. Config validation
	cfg := doctorCheckConfig(report)

	// 2. Indexer binaries
	fmt.Println("\nIndexer Binaries:")
	checkIndexers(cmd, report)

	// 3. Service checks (only if config is valid)
	fmt.Println("\nService Connectivity:")
	if cfg != nil {
		doctorCheckQdrant(cmd, cfg, report)
		doctorCheckMemgraph(cmd, cfg, report)
		doctorCheckOllama(cmd, cfg, report)
	} else {
		report.warn("services", "skipped — config invalid")
	}

	// 4. Project structure
	fmt.Println("\nProject Structure:")
	doctorCheckProjectStructure(report)

	// 5. Outbox check — surface any pending index writes without repairing them.
	// Scope: the outbox tracks ONLY gg index (full/changed) operations, which
	// write to Memgraph. Commands like gg decide/task/note write to a single
	// Qdrant collection and need no outbox protection.
	fmt.Println("\nOutbox (index pipeline crash-safety):")
	doctorCheckOutbox(report)

	// Summary
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	if report.problems == 0 {
		fmt.Println("All checks passed.")
		fmt.Println()
		fmt.Println("Agent handoff prompt (paste into your AI agent's chat):")
		fmt.Println()
		fmt.Println("    Read AGENTS.md and follow the gg protocol from now on.")
		fmt.Println()
		fmt.Println("Works with any agent that can read files (Claude Code, GSD,")
		fmt.Println("Codex, Cursor, Aider, …). The agent will run gg status, search")
		fmt.Println("prior decisions, and persist new ones automatically.")
		return nil
	}
	return fmt.Errorf("%d problem(s) found — fix the issues above and re-run `gg doctor`", report.problems)
}

// runDoctorReconcile scans the outbox for incomplete dual-store writes and
// reports what needs to be re-run to restore consistency.
//
// For index operations the repair is: re-run `gg index` (the idempotent
// UpsertNode/UpsertEdge writes make this safe). The user is shown the exact
// command rather than running it automatically, since indexing may take minutes.
func runDoctorReconcile(cmd *cobra.Command) error {
	fmt.Println("GG Doctor — Reconcile")
	fmt.Println(strings.Repeat("─", 50))

	ggDir, err := config.GGDir()
	if err != nil {
		return fmt.Errorf("find .gg dir: %w", err)
	}

	entries, err := outbox.List(ggDir)
	if err != nil {
		return fmt.Errorf("read outbox: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("Outbox is empty — index pipeline is consistent.")
		fmt.Println("Note: gg decide/task/note/etc write to a single Qdrant collection")
		fmt.Println("and are not tracked here (single-store, no outbox needed).")
		return nil
	}

	fmt.Printf("Found %d pending outbox entry(ies):\n\n", len(entries))
	needsAction := false
	for _, e := range entries {
		fmt.Printf("  ID:      %s\n", e.ID)
		fmt.Printf("  Kind:    %s\n", e.Kind)
		fmt.Printf("  Created: %s\n", e.CreatedAt)
		if e.Retries > 0 {
			fmt.Printf("  Retries: %d\n", e.Retries)
		}

		switch e.Kind {
		case "full-index", "changed-index":
			var p struct {
				Root string `json:"root"`
				Lang string `json:"lang"`
				SHA  string `json:"sha"`
			}
			if jsonErr := json.Unmarshal(e.Payload, &p); jsonErr == nil {
				shortSHA := p.SHA
				if len(shortSHA) > 8 {
					shortSHA = shortSHA[:8]
				}
				fmt.Printf("  → Memgraph write for sha=%s may be incomplete.\n", shortSHA)
				fmt.Printf("    Repair: gg index --lang %s\n", p.Lang)
			} else {
				fmt.Printf("  → Payload unreadable: %v\n", jsonErr)
			}
			needsAction = true
		default:
			fmt.Printf("  → Unknown kind %q — manual inspection required.\n", e.Kind)
			needsAction = true
		}
		fmt.Println()
	}

	if needsAction {
		return fmt.Errorf("%d pending outbox entry(ies) — run the repair commands shown above, then re-run `gg doctor --reconcile`", len(entries))
	}
	return nil
}

// doctorCheckOutbox reports whether there are unresolved outbox entries.
func doctorCheckOutbox(report *doctorReport) {
	ggDir, err := config.GGDir()
	if err != nil {
		report.warn("outbox", "could not locate .gg dir")
		return
	}
	entries, err := outbox.List(ggDir)
	if err != nil {
		report.warn("outbox", fmt.Sprintf("read failed: %v", err))
		return
	}
	if len(entries) == 0 {
		report.ok("outbox", "empty — stores consistent")
		return
	}
	report.fail("outbox", fmt.Sprintf("%d pending entry(ies) — run `gg doctor --reconcile`", len(entries)))
}

// doctorCheckConfig validates and reports on the project config.
// Returns the config if valid, nil if invalid.
func doctorCheckConfig(report *doctorReport) *config.Config {
	fmt.Println("\nConfiguration:")
	cfg, err := config.Load()
	if err != nil {
		if errors.Is(err, config.ErrMissingProjectID) {
			report.fail("config.yaml", "project_id missing — run `gg init`")
		} else {
			report.fail("config.yaml", err.Error())
		}
		return nil
	}
	report.ok("config.yaml", "valid")
	report.ok("project_id", cfg.ProjectID)

	// Check Memgraph URI is set (it's optional in Validate today, but doctor flags it)
	if cfg.Memgraph.URI == "" {
		report.warn("memgraph.uri", "not configured — Memgraph features unavailable")
	}
	return cfg
}

// doctorCheckQdrant checks Qdrant connectivity and collection presence.
func doctorCheckQdrant(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	ggDir, _ := config.GGDir()
	c, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		report.fail("qdrant", fmt.Sprintf("client init: %v", err))
		return
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := c.HealthCheck(ctx); err != nil {
		report.fail("qdrant", fmt.Sprintf("unreachable at %s:%d — %v", cfg.Qdrant.Host, cfg.Qdrant.Port, err))
		return
	}
	report.ok("qdrant", fmt.Sprintf("reachable at %s:%d", cfg.Qdrant.Host, cfg.Qdrant.Port))

	// Collection presence
	present, missing, err := c.CollectionStatus(ctx)
	if err != nil {
		report.fail("qdrant collections", err.Error())
		return
	}
	_ = present
	if len(missing) > 0 {
		report.fail("qdrant collections", fmt.Sprintf("%d missing: %s — run `gg init`", len(missing), strings.Join(missing, ", ")))
	} else {
		report.ok("qdrant collections", "all 7 collections present")
	}
}

// doctorCheckMemgraph checks Memgraph connectivity and schema indexes.
func doctorCheckMemgraph(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	if cfg.Memgraph.URI == "" {
		report.warn("memgraph", "not configured — skipped")
		return
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		report.fail("memgraph", fmt.Sprintf("client init: %v", err))
		return
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := gc.HealthCheck(ctx); err != nil {
		report.fail("memgraph", fmt.Sprintf("unreachable at %s — %v", cfg.Memgraph.URI, err))
		return
	}
	report.ok("memgraph", fmt.Sprintf("reachable at %s", cfg.Memgraph.URI))

	// Schema check — call SchemaInit which is idempotent.
	// If it succeeds, indexes are guaranteed to exist.
	if err := gc.SchemaInit(ctx); err != nil {
		report.warn("memgraph schema", fmt.Sprintf("schema init failed: %v", err))
	} else {
		report.ok("memgraph schema", "indexes present")
	}
}

// doctorCheckOllama checks Ollama connectivity and validates the embedding
// dimension matches the Qdrant VectorSize (768).
func doctorCheckOllama(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	ollamaURL := cfg.Embedding.Host + "/api/tags"
	curlCtx, curlCancel := context.WithTimeout(cmd.Context(), 3*time.Second)
	defer curlCancel()
	c := exec.CommandContext(curlCtx, "curl", "-sf", "--max-time", "3", ollamaURL)
	if err := c.Run(); err != nil {
		report.fail("ollama", fmt.Sprintf("unreachable at %s", cfg.Embedding.Host))
		return
	}
	report.ok("ollama", fmt.Sprintf("reachable at %s (model: %s)", cfg.Embedding.Host, cfg.Embedding.Model))

	// Dimension check — generate a small test embedding.
	gen := embedding.New(&cfg.Embedding, store.VectorSize)
	dimCtx, dimCancel := withTimeout(cmd.Context())
	defer dimCancel()
	vec, err := gen.Generate(dimCtx, "dimension check")
	if err != nil {
		// Dimension mismatch is returned as error by the generator.
		if strings.Contains(err.Error(), "dimension mismatch") {
			report.fail("ollama embedding dim", err.Error())
		} else {
			report.warn("ollama embedding dim", fmt.Sprintf("could not verify: %v", err))
		}
		return
	}
	if len(vec) != store.VectorSize {
		report.fail("ollama embedding dim", fmt.Sprintf("model %q returns %d-dim vectors, Qdrant expects %d", cfg.Embedding.Model, len(vec), store.VectorSize))
	} else {
		report.ok("ollama embedding dim", fmt.Sprintf("%d-dim ✓ (model: %s)", len(vec), cfg.Embedding.Model))
	}
}

// doctorCheckProjectStructure verifies the presence of AGENTS.md and the index state file.
func doctorCheckProjectStructure(report *doctorReport) {
	root, err := config.FindRoot()
	if err != nil {
		report.warn("project root", "not found (run outside gg project?)")
		return
	}

	agentsPath := filepath.Join(root, "AGENTS.md")
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		report.fail("AGENTS.md", "missing — agents lack instructions for this project")
	} else if err != nil {
		report.warn("AGENTS.md", fmt.Sprintf("stat error: %v", err))
	} else {
		report.ok("AGENTS.md", agentsPath)
	}

	indexState := filepath.Join(root, config.DirName, "index-state.json")
	if _, err := os.Stat(indexState); os.IsNotExist(err) {
		report.warn("index-state.json", "not present — run `gg index` to generate")
	} else if err == nil {
		report.ok("index-state.json", "present")
	}
}

// checkIndexers verifies each SCIP binary. With --install-indexers, missing
// ones are installed via their native package manager.
func checkIndexers(cmd *cobra.Command, report *doctorReport) {
	for _, spec := range indexers {
		path, err := runner.ResolveIndexer(spec.Binary)
		if err == nil {
			report.ok(spec.Binary, path)
			continue
		}

		var missing *runner.ErrIndexerMissing
		if !errors.As(err, &missing) {
			report.warn(spec.Binary, fmt.Sprintf("unexpected error: %v", err))
			continue
		}

		if !doctorInstallIndexers {
			report.fail(spec.Binary, fmt.Sprintf("not found — install: %s", missing.Hint))
			continue
		}

		fmt.Printf("  ↓ %-28s installing via %s ...\n", spec.Binary, spec.Install[0])
		if installErr := runInstall(cmd, spec); installErr != nil {
			report.fail(spec.Binary, fmt.Sprintf("install failed: %v", installErr))
			if spec.Note != "" {
				fmt.Printf("    note: %s\n", spec.Note)
			}
		} else {
			if finalPath, resolveErr := runner.ResolveIndexer(spec.Binary); resolveErr == nil {
				report.ok(spec.Binary, "installed at "+finalPath)
			} else {
				report.warn(spec.Binary, "installed (ensure install dir is on PATH)")
				if spec.Note != "" {
					fmt.Printf("    note: %s\n", spec.Note)
				}
			}
		}
	}
}

// runInstall executes the install command for the given spec.
func runInstall(cmd *cobra.Command, spec indexerSpec) error {
	args := spec.Install
	if runtime.GOOS == "windows" {
		args = append([]string{"cmd", "/C"}, args...)
	}
	c := exec.CommandContext(cmd.Context(), args[0], args[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(spec.Install, " "), err)
	}
	return nil
}
