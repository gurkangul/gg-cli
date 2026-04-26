package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/templates"
)

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

	if cfg.Memgraph.URI == "" {
		report.warn("memgraph.uri", "not configured — Memgraph features unavailable")
	}
	return cfg
}

// qdrantHealthChecker is a narrow interface for health-checking Qdrant.
// Production code uses *store.Client; tests inject a fake.
type qdrantHealthChecker interface {
	HealthCheck(ctx context.Context) error
	CollectionStatus(ctx context.Context) (present, missing []string, err error)
	Close() error
}

// doctorQdrantNewClient builds the real Qdrant client. Replaced in tests
// to inject a fake health checker without a real socket.
var doctorQdrantNewClient = func(cfg *config.Config, ggDir string) (qdrantHealthChecker, error) {
	return store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
}

// doctorCheckQdrant checks Qdrant connectivity and collection presence.
func doctorCheckQdrant(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	ggDir, _ := config.GGDir()
	c, err := doctorQdrantNewClient(cfg, ggDir)
	if err != nil {
		report.fail("qdrant", fmt.Sprintf("client init: %v", err))
		return
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := c.HealthCheck(ctx); err != nil {
		if hint := sandboxPermissionHint(err); hint != "" {
			fmt.Fprintf(os.Stderr, "  ✗ qdrant unreachable at %s:%d — operation not permitted (sandbox?)\n", cfg.Qdrant.Host, cfg.Qdrant.Port)
			fmt.Fprintf(os.Stderr, "    %s\n", hint)
			report.problems++
			return
		}
		report.fail("qdrant", fmt.Sprintf("unreachable at %s:%d — %v", cfg.Qdrant.Host, cfg.Qdrant.Port, err))
		return
	}
	report.ok("qdrant", fmt.Sprintf("reachable at %s:%d", cfg.Qdrant.Host, cfg.Qdrant.Port))

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
		if hint := sandboxPermissionHint(err); hint != "" {
			fmt.Fprintf(os.Stderr, "  ✗ memgraph unreachable at %s — operation not permitted (sandbox?)\n", cfg.Memgraph.URI)
			fmt.Fprintf(os.Stderr, "    %s\n", hint)
			report.problems++
			return
		}
		report.fail("memgraph", fmt.Sprintf("unreachable at %s — %v", cfg.Memgraph.URI, err))
		return
	}
	report.ok("memgraph", fmt.Sprintf("reachable at %s", cfg.Memgraph.URI))

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
		if hint := sandboxPermissionHint(err); hint != "" {
			fmt.Fprintf(os.Stderr, "  ✗ ollama unreachable at %s — operation not permitted (sandbox?)\n", cfg.Embedding.Host)
			fmt.Fprintf(os.Stderr, "    %s\n", hint)
			report.problems++
			return
		}
		report.fail("ollama", fmt.Sprintf("unreachable at %s", cfg.Embedding.Host))
		return
	}
	report.ok("ollama", fmt.Sprintf("reachable at %s (model: %s)", cfg.Embedding.Host, cfg.Embedding.Model))

	gen := embedding.New(&cfg.Embedding, store.VectorSize)
	dimCtx, dimCancel := withTimeout(cmd.Context())
	defer dimCancel()
	vec, err := gen.Generate(dimCtx, "dimension check")
	if err != nil {
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
		doctorCheckAgentsSchema(report, agentsPath)
	}

	indexState := filepath.Join(root, config.DirName, "index-state.json")
	if _, err := os.Stat(indexState); os.IsNotExist(err) {
		report.warn("index-state.json", "not present — run `gg index` to generate")
	} else if err == nil {
		report.ok("index-state.json", "present")
	}

	gitignorePath := filepath.Join(root, config.DirName, ".gitignore")
	if data, readErr := os.ReadFile(gitignorePath); readErr != nil { //nolint:gosec
		report.warn(".gg/.gitignore", "missing — run `gg init` or `gg doctor --heal` to create")
	} else {
		content := string(data)
		var missing []string
		for _, line := range gitignoreRequiredLines {
			if !strings.Contains(content, line) {
				missing = append(missing, line)
			}
		}
		if len(missing) > 0 {
			report.warn(".gg/.gitignore",
				fmt.Sprintf("stale — missing entries: %s (run `gg doctor --heal` to fix)", strings.Join(missing, ", ")))
		} else {
			report.ok(".gg/.gitignore", "aligned")
		}
	}
}

// doctorCheckAgentsSchema parses agents_schema from the project's AGENTS.md frontmatter
// and compares the major version to the bundled template.
func doctorCheckAgentsSchema(report *doctorReport, agentsPath string) {
	data, err := os.ReadFile(agentsPath)
	if err != nil {
		report.warn("agents_schema", fmt.Sprintf("could not read AGENTS.md: %v", err))
		return
	}

	projectSchema, ok := parseAgentsSchema(string(data))
	if !ok {
		report.warn("agents_schema", "no agents_schema frontmatter — add '---\\nagents_schema: \"2.0\"\\n---' to AGENTS.md")
		return
	}

	bundledSchema, _ := parseAgentsSchema(templates.AgentsMD)
	if bundledSchema == "" {
		report.warn("agents_schema", fmt.Sprintf("project has schema %q; bundled template schema unknown", projectSchema))
		return
	}

	projectMajor := schemaMajor(projectSchema)
	bundledMajor := schemaMajor(bundledSchema)

	if projectMajor != bundledMajor {
		report.fail("agents_schema",
			fmt.Sprintf("major version mismatch: project=%q bundled=%q — re-run `gg init` to refresh AGENTS.md, or manually update agents_schema",
				projectSchema, bundledSchema))
		return
	}

	if projectSchema == bundledSchema {
		report.ok("agents_schema", fmt.Sprintf("up to date (%s)", projectSchema))
	} else {
		report.warn("agents_schema",
			fmt.Sprintf("minor drift: project=%q bundled=%q — consider updating AGENTS.md", projectSchema, bundledSchema))
	}
}

// parseAgentsSchema extracts the agents_schema value from YAML frontmatter.
// Returns ("", false) if no frontmatter or no agents_schema field.
func parseAgentsSchema(content string) (string, bool) {
	if !strings.HasPrefix(strings.TrimLeft(content, "\r\n"), "---") {
		return "", false
	}
	lines := strings.SplitN(content, "\n", 50)
	inFront := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if !inFront {
				inFront = true
				continue
			}
			break
		}
		if !inFront {
			continue
		}
		if strings.HasPrefix(trimmed, "agents_schema:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "agents_schema:"))
			val = strings.Trim(val, `"'`)
			return val, true
		}
	}
	return "", false
}

// schemaMajor returns the major version integer from a semver-like string ("2.1" → 2).
// Returns 0 if the string cannot be parsed.
func schemaMajor(schema string) int {
	parts := strings.SplitN(schema, ".", 2)
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return n
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

// runDoctorWipeBrain drops all Qdrant collections and Memgraph nodes for
// this project. Requires --yes to skip the interactive prompt.
func runDoctorWipeBrain(cmd *cobra.Command) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}

	if !doctorWipeBrainYes {
		fmt.Fprintf(os.Stderr,
			"⚠ --wipe-brain will DELETE all Qdrant collections and Memgraph nodes for project %s.\n"+
				"  This cannot be undone. Re-run with --yes to confirm.\n", cfg.ProjectID)
		return fmt.Errorf("--wipe-brain requires --yes")
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	fmt.Println("GG Doctor — wipe brain")
	fmt.Println(strings.Repeat("─", 50))

	sc, storeErr := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if storeErr != nil {
		return fmt.Errorf("store init: %w", storeErr)
	}
	defer func() { _ = sc.Close() }()

	hctx, hcancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer hcancel()
	if hErr := sc.HealthCheck(hctx); hErr != nil {
		return storeDownErr()
	}

	if dropErr := sc.DropAllCollections(ctx); dropErr != nil {
		return fmt.Errorf("drop qdrant collections: %w", dropErr)
	}
	fmt.Println("  ✓ Qdrant collections dropped")

	if cfg.Memgraph.URI != "" {
		gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID)
		if gcErr != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ Memgraph init failed (%v) — skipped\n", gcErr)
		} else {
			defer func() { _ = gc.Close(ctx) }()
			if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
				return fmt.Errorf("sweep memgraph: %w", sweepErr)
			}
			fmt.Println("  ✓ Memgraph project nodes swept")
		}
	}

	fmt.Printf("\nProject %s brain wiped. Run 'gg brain import' to restore from snapshot.\n", cfg.ProjectID)
	return nil
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
