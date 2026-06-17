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

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/templates"
)

// doctorCheckConfig validates and reports on the project config.
// Returns the config if valid, nil if invalid.
func doctorCheckConfig(report *doctorReport) *config.Config {
	doctorPrintln("\nConfiguration:")
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

	return cfg
}

// qdrantHealthChecker is a narrow interface for health-checking the vector
// store. Production code uses *store.Client; tests inject a fake.
type qdrantHealthChecker interface {
	HealthCheck(ctx context.Context) error
	CollectionStatus(ctx context.Context) (present, missing []string, err error)
	Close() error
}

// doctorQdrantNewClient builds the real embedded vector-store client. Replaced
// in tests to inject a fake health checker without touching disk.
var doctorQdrantNewClient = func(cfg *config.Config, ggDir string) (qdrantHealthChecker, error) {
	return store.New(ggDir, cfg.ProjectID)
}

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

// doctorCheckCodeGraphFreshness verifies that impact/blast-radius answers are
// backed by a populated embedded code graph (.gg/graph.db) aligned with HEAD,
// not merely by an index-state.json file existing on disk.
func doctorCheckCodeGraphFreshness(cmd *cobra.Command, cfg *config.Config, report *doctorReport) {
	if cfg == nil {
		report.warn("code graph", "skipped — config invalid")
		return
	}
	root, err := config.FindRoot()
	if err != nil {
		report.warn("code graph", fmt.Sprintf("project root not found: %v", err))
		return
	}
	ggDir := filepath.Join(root, config.DirName)
	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()
	status := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	detail := status.Detail
	if detail == "" {
		detail = status.Status
	}
	if notice := codeGraphNoticeOneLine(status); notice != "" {
		detail = notice
	}
	fresh := status.freshnessContract()
	_, indexStateErr := os.Stat(filepath.Join(ggDir, "index-state.json"))
	neverIndexed := indexStateErr != nil
	switch fresh.Status {
	case codeGraphFreshnessReady:
		report.ok("code graph", detail)
	case codeGraphFreshnessMissing:
		// A never-indexed project (no index-state.json) is the expected fresh-init
		// state — `gg init` itself points the user at `gg index` — so this is an
		// advisory warn, not a hard fail. A "missing" graph WITH an index-state.json
		// means the recorded index was lost/corrupted and stays a fail (audit INFRA-4).
		if neverIndexed {
			report.warn("code graph", detail+" — run `gg index` to build the code graph")
		} else {
			report.fail("code graph", detail)
		}
	case codeGraphFreshnessStale, codeGraphFreshnessUnavailable:
		report.fail("code graph", detail)
	default:
		report.warn("code graph", detail)
	}
	doctorCheckIndexHooks(root, fresh, report)
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
	projectRoot, rootErr := config.FindRoot()
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

		required, requireErr := indexerRequiredForProject(projectRoot, rootErr, spec)
		if requireErr != nil {
			report.warn(spec.Binary, fmt.Sprintf("could not determine language manifests: %v", requireErr))
			required = true
		}

		if !doctorInstallIndexers {
			if !required {
				report.warn(spec.Binary, fmt.Sprintf("not found — optional; no %s manifest detected", spec.Lang))
				continue
			}
			label := "install"
			if !missing.AutoInstall {
				label = "external setup"
			}
			report.fail(spec.Binary, fmt.Sprintf("not found — %s: %s", label, missing.Hint))
			continue
		}
		if !required {
			report.warn(spec.Binary, fmt.Sprintf("not found — optional; no %s manifest detected", spec.Lang))
			continue
		}
		if len(spec.Install) == 0 {
			report.fail(spec.Binary, fmt.Sprintf("not found — automatic install unavailable: %s", missing.Hint))
			if spec.Note != "" {
				fmt.Printf("    note: %s\n", spec.Note)
			}
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

func indexerRequiredForProject(projectRoot string, rootErr error, spec indexerSpec) (bool, error) {
	if rootErr != nil {
		return false, rootErr
	}
	moduleDirs, err := discoverModuleDirs(projectRoot, runner.Lang(spec.Lang))
	if err != nil {
		return false, err
	}
	return len(moduleDirs) > 0, nil
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
			"⚠ --wipe-brain will DELETE all vector collections and code-graph nodes for project %s.\n"+
				"  This cannot be undone. Re-run with --yes to confirm.\n", cfg.ProjectID)
		return fmt.Errorf("--wipe-brain requires --yes")
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	fmt.Println("GG Doctor — wipe brain")
	fmt.Println(strings.Repeat("─", 50))

	sc, storeErr := store.New(ggDir, cfg.ProjectID)
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
		return fmt.Errorf("drop vector collections: %w", dropErr)
	}
	fmt.Println("  ✓ Vector collections dropped")

	gc, gcErr := graph.New(cfg.DataDir, cfg.ProjectID)
	if gcErr != nil {
		fmt.Fprintf(os.Stderr, "  ⚠ code graph init failed (%v) — skipped\n", gcErr)
	} else {
		defer func() { _ = gc.Close(ctx) }()
		if sweepErr := gc.SweepProject(ctx); sweepErr != nil {
			return fmt.Errorf("sweep code graph: %w", sweepErr)
		}
		fmt.Println("  ✓ Code-graph project nodes swept")
	}

	fmt.Printf("\nProject %s brain wiped. Run 'gg brain import' to restore from snapshot.\n", cfg.ProjectID)
	return nil
}

// runInstall executes the install command for the given spec.
func runInstall(cmd *cobra.Command, spec indexerSpec) error {
	args := spec.Install
	if len(args) == 0 {
		return fmt.Errorf("no automatic installer configured for %s", spec.Binary)
	}
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
