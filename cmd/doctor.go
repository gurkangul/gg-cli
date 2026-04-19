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
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/artifacts"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/embedding"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/runner"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/session"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/templates"
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
	doctorInstallIndexers   bool
	doctorReconcile         bool
	doctorInstallAgentHooks bool
	doctorInstallAgentsMD   bool
	doctorHooksAgent        string
	doctorHooksDryRun       bool
	doctorHooksForce        bool
	doctorHeal              bool
	doctorWipeBrain         bool
	doctorWipeBrainYes      bool
	doctorInstallTaskHooks  bool
	doctorSyncArtifacts     bool
	doctorSyncApply         bool
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorInstallIndexers, "install-indexers", false,
		"install missing SCIP indexer binaries (scip-go, scip-typescript, scip-python)")
	doctorCmd.Flags().BoolVar(&doctorReconcile, "reconcile", false,
		"scan the outbox for incomplete dual-store writes and report what needs repair")
	doctorCmd.Flags().BoolVar(&doctorInstallAgentHooks, "install-agent-hooks", false,
		"write agent-side config (SessionStart hook / alwaysApply rule / read-preload) to enforce gg usage")
	doctorCmd.Flags().StringVar(&doctorHooksAgent, "agent", "",
		"restrict --install-agent-hooks to a single agent (claude, cursor, aider, codex, zai)")
	doctorCmd.Flags().BoolVar(&doctorHooksDryRun, "dry-run", false,
		"with --install-agent-hooks: report what would change without writing anything")
	doctorCmd.Flags().BoolVar(&doctorHeal, "heal", false,
		"migrate legacy .gg/telemetry.jsonl and .gg/cache/ to ~/.gg/projects/<id>/ (idempotent)")
	doctorCmd.Flags().BoolVar(&doctorHooksForce, "force", false,
		"with --install-agent-hooks: bypass detection and install for the named agent(s)")
	doctorCmd.Flags().BoolVar(&doctorWipeBrain, "wipe-brain", false,
		"drop all Qdrant collections and Memgraph nodes for this project (destructive — use for testing)")
	doctorCmd.Flags().BoolVar(&doctorWipeBrainYes, "yes", false,
		"with --wipe-brain: skip interactive confirmation")
	doctorCmd.Flags().BoolVar(&doctorInstallTaskHooks, "install-task-hooks", false,
		"install verify-gate (pre-task-done.d) + post-done task-done.d hooks; auto-detects Go (go.mod) and/or Node/Bun (package.json)")
	doctorCmd.Flags().BoolVar(&doctorInstallAgentsMD, "install-agents-md", false,
		"inject the gg tracker-rules managed block into AGENTS.md (idempotent; alias for --install-agent-hooks --agent codex)")
	doctorCmd.Flags().BoolVar(&doctorSyncArtifacts, "sync-artifacts", false,
		"compare .gg/installed.json against the current CLI templates and show a drift table")
	doctorCmd.Flags().BoolVar(&doctorSyncApply, "apply", false,
		"with --sync-artifacts: re-install drifted or missing artifacts")
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
		Install: []string{"npm", "install", "-g", "@sourcegraph/scip-python"},
		Note:    "requires Node.js 18+",
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
	// --install-agent-hooks / --install-agents-md: agent-config installer mode.
	// Skips the normal diagnostics and runs the agenthooks package against the
	// project. --install-agents-md is a focused alias that restricts the run
	// to the codex (AGENTS.md) installer only.
	if doctorInstallAgentHooks {
		if !enforcement.Enabled() {
			fmt.Println("gg doctor --install-agent-hooks: " + enforcement.DisabledNotice)
			return nil
		}
		return runDoctorInstallAgentHooks(cmd)
	}
	if doctorInstallAgentsMD {
		if !enforcement.Enabled() {
			fmt.Println("gg doctor --install-agents-md: " + enforcement.DisabledNotice)
			return nil
		}
		doctorHooksAgent = "codex"
		return runDoctorInstallAgentHooks(cmd)
	}
	// --heal: migrate legacy .gg/ runtime state to ~/.gg/projects/<id>/.
	if doctorHeal {
		return runDoctorHeal()
	}
	// --wipe-brain: destructive reset of Qdrant + Memgraph for this project.
	if doctorWipeBrain {
		return runDoctorWipeBrain(cmd)
	}
	// --install-task-hooks: write Go project task-done hook.
	if doctorInstallTaskHooks {
		if !enforcement.Enabled() {
			fmt.Println("gg doctor --install-task-hooks: " + enforcement.DisabledNotice)
			return nil
		}
		return runDoctorInstallTaskHooks()
	}
	// --sync-artifacts: compare installed.json vs current CLI templates.
	if doctorSyncArtifacts {
		return runDoctorSyncArtifacts(doctorSyncApply)
	}

	fmt.Println("GG Doctor")
	fmt.Println(strings.Repeat("─", 50))
	if !enforcement.Enabled() {
		fmt.Printf("⚠ guards=off  (unset %s or set =on to re-enable: install-agent-hooks, gsd-guard, pre-task-done gate)\n", enforcement.EnvVar)
	}

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

	// Artifact drift check (advisory — does not count as a problem).
	driftCount := doctorCheckArtifactDrift()

	// Summary
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	if report.problems == 0 {
		fmt.Println("All checks passed.")
		if driftCount > 0 {
			fmt.Printf("  ⚠ %d artifact(s) drifted from CLI templates — run `gg doctor --sync-artifacts` to inspect\n", driftCount)
		}
		fmt.Println()
		fmt.Println("Paste this into your AI agent's chat (works for any agent):")
		fmt.Println()
		fmt.Println("────────────────────────────────────────────────────────────")
		fmt.Print(session.PasteBlock("claude-code"))
		fmt.Println("────────────────────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Install agent hooks: `gg doctor --install-agent-hooks`")
		return nil
	}
	return fmt.Errorf("%d problem(s) found — fix the issues above and re-run `gg doctor`", report.problems)
}

// doctorCheckArtifactDrift reads installed.json and counts drifted/missing
// artifacts. Returns 0 when the manifest is missing (not yet initialized).
func doctorCheckArtifactDrift() int {
	root, err := config.FindRoot()
	if err != nil {
		return 0
	}
	ggDir := filepath.Join(root, ".gg")
	m, err := artifacts.Read(ggDir)
	if err != nil || len(m.Artifacts) == 0 {
		return 0
	}
	current := templates.ArtifactSHAs()
	entries := artifacts.Diff(m, current)
	n := 0
	for _, e := range entries {
		if e.Status != "ok" {
			n++
		}
	}
	return n
}

// runDoctorSyncArtifacts implements `gg doctor --sync-artifacts [--apply]`.
func runDoctorSyncArtifacts(apply bool) error {
	root, err := config.FindRoot()
	if err != nil {
		return fmt.Errorf("not inside a gg project: %w", err)
	}
	ggDir := filepath.Join(root, ".gg")
	m, err := artifacts.Read(ggDir)
	if err != nil {
		return fmt.Errorf("read installed.json: %w", err)
	}
	current := templates.ArtifactSHAs()
	entries := artifacts.Diff(m, current)

	fmt.Println("Artifact Drift Report")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  %-46s  %s\n", "Artifact", "Status")
	fmt.Println(strings.Repeat("─", 60))

	drifted := 0
	for _, e := range entries {
		marker := "✓"
		if e.Status == "drifted" {
			marker = "~"
			drifted++
		} else if e.Status == "missing" {
			marker = "?"
			drifted++
		}
		fmt.Printf("  %s %-44s  %s\n", marker, e.Key, e.Status)
	}
	fmt.Println(strings.Repeat("─", 60))

	if drifted == 0 {
		fmt.Println("All artifacts up to date.")
		return nil
	}
	fmt.Printf("%d artifact(s) need attention.\n", drifted)

	if !apply {
		fmt.Println("Run with --apply to re-install drifted artifacts.")
		return nil
	}

	applied := 0
	for _, e := range entries {
		if e.Status == "ok" {
			continue
		}
		if err := syncArtifactFile(ggDir, e.Key); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", e.Key, err)
			continue
		}
		if err := artifacts.Record(ggDir, e.Key, e.Current); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ record %s: %v\n", e.Key, err)
			continue
		}
		fmt.Printf("  ✓ restored %s\n", e.Key)
		applied++
	}
	fmt.Printf("%d artifact(s) restored.\n", applied)
	return nil
}

// syncArtifactFile writes the current CLI template content for key to
// <ggDir>/<key>, creating parent directories as needed.
func syncArtifactFile(ggDir, key string) error {
	content, ok := artifactContent(key)
	if !ok {
		return fmt.Errorf("unknown artifact key %q", key)
	}
	path := filepath.Join(ggDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if strings.HasSuffix(key, ".sh") {
		perm = 0o755
	}
	return os.WriteFile(path, []byte(content), perm) //nolint:gosec
}

// artifactContent returns the bundled template body for a given key.
func artifactContent(key string) (string, bool) {
	switch key {
	case "RULES.md":
		return templates.RulesMD, true
	case "AGENTS.md":
		return templates.AgentsMD, true
	case "hooks/pre-task-done.d/05-smoke-e2e.sh":
		return templates.SmokeE2EHook, true
	case "hooks/task-done.d/90-bug-repros.sh":
		return templates.BugReprosHook, true
	case "hooks/pre-task-done.d/10-go-verify.sh":
		return templates.PreTaskDoneGoHook, true
	case "hooks/pre-task-done.d/10-node-verify.sh":
		return templates.PreTaskDoneNodeHook, true
	case "hooks/task-done.d/80-task-done-go.sh":
		return templates.TaskDoneGoHook, true
	case "templates/makefile-test-tiers.mk":
		return templates.MakefileTestTiers, true
	}
	return "", false
}

// runDoctorInstallAgentHooks is the subcommand dispatched by
// `gg doctor --install-agent-hooks`. It runs the agenthooks package against
// the project root and prints a report. --agent restricts the run to a
// single installer; without it, auto-detect installs all present agents.
func runDoctorInstallAgentHooks(_ *cobra.Command) error {
	root, err := config.FindRoot()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}

	opts := agenthooks.Options{DryRun: doctorHooksDryRun, Force: doctorHooksForce}

	var results []agenthooks.Result
	switch {
	case strings.TrimSpace(doctorHooksAgent) != "":
		names := splitCSV(doctorHooksAgent)
		results = agenthooks.InstallNamed(root, names, opts)
	default:
		results = agenthooks.InstallDetected(root, opts)
	}

	fmt.Println("Agent Hooks")
	fmt.Println(strings.Repeat("─", 50))
	agenthooks.RenderReport(os.Stdout, results)
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println(agenthooks.Summary(results))

	if agenthooks.HasProblems(results) {
		return fmt.Errorf("one or more installers failed — see report above")
	}
	return nil
}

// splitCSV parses a comma-separated flag value into a cleaned slice.
// Empty tokens are dropped so --agent="claude,," still yields ["claude"].
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
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
		// Schema version check — compare major version of project AGENTS.md vs bundled template.
		doctorCheckAgentsSchema(report, agentsPath)
	}

	indexState := filepath.Join(root, config.DirName, "index-state.json")
	if _, err := os.Stat(indexState); os.IsNotExist(err) {
		report.warn("index-state.json", "not present — run `gg index` to generate")
	} else if err == nil {
		report.ok("index-state.json", "present")
	}

	// .gg/.gitignore alignment check.
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
// and compares the major version to the bundled template. A major version mismatch
// means the project's AGENTS.md is incompatible with the current CLI — agents will
// be missing protocol rules. Minor version differences are just warnings.
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
		// Bundled template missing schema — internal inconsistency.
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
		// Same major, different minor — agent is still compatible but old.
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
			break // closing ---
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

// runDoctorHeal migrates legacy runtime state from .gg/ to the per-project
// runtime directory (~/.gg/projects/<project_id>/). It is safe to run multiple
// times — each step is idempotent:
//
//   - .gg/telemetry.jsonl → appended to runtimeDir/telemetry.jsonl, then removed.
//   - .gg/cache/          → moved to runtimeDir/cache/, then removed.
//
// The function prints git rm --cached suggestions but never runs git itself.
func runDoctorHeal() error {
	cfg, err := config.Load()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	ggDir, err := config.GGDir()
	if err != nil {
		return configErr("run `gg init` first: " + err.Error())
	}
	rtDir, err := cfg.RuntimeDir()
	if err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	fmt.Println("GG Doctor — heal (runtime state migration)")
	fmt.Println(strings.Repeat("─", 50))

	migrated := false
	var gitRmTargets []string

	// ── Telemetry migration ──────────────────────────────────────────────────
	srcTelemetry := filepath.Join(ggDir, "telemetry.jsonl")
	if info, statErr := os.Stat(srcTelemetry); statErr == nil && !info.IsDir() {
		dstTelemetry := filepath.Join(rtDir, "telemetry.jsonl")
		if appendErr := appendFile(dstTelemetry, srcTelemetry); appendErr != nil {
			fmt.Printf("  ✗ telemetry.jsonl: append failed: %v\n", appendErr)
		} else if rmErr := os.Remove(srcTelemetry); rmErr != nil {
			fmt.Printf("  ~ telemetry.jsonl: appended but could not remove source: %v\n", rmErr)
		} else {
			fmt.Printf("  ✓ telemetry.jsonl → %s\n", dstTelemetry)
			gitRmTargets = append(gitRmTargets, ".gg/telemetry.jsonl")
			migrated = true
		}
	} else {
		fmt.Println("  ✓ telemetry.jsonl  (not in .gg/ — nothing to migrate)")
	}

	// ── Cache migration ──────────────────────────────────────────────────────
	srcCache := filepath.Join(ggDir, "cache")
	if info, statErr := os.Stat(srcCache); statErr == nil && info.IsDir() {
		dstCache := filepath.Join(rtDir, "cache")
		if renameErr := os.Rename(srcCache, dstCache); renameErr != nil {
			// Cross-device rename can fail — fall back to copy-then-remove.
			if copyErr := copyDir(srcCache, dstCache); copyErr != nil {
				fmt.Printf("  ✗ cache/: migration failed: %v\n", copyErr)
			} else if rmErr := os.RemoveAll(srcCache); rmErr != nil {
				fmt.Printf("  ~ cache/: copied but could not remove source: %v\n", rmErr)
			} else {
				fmt.Printf("  ✓ cache/ → %s\n", dstCache)
				gitRmTargets = append(gitRmTargets, ".gg/cache/")
				migrated = true
			}
		} else {
			fmt.Printf("  ✓ cache/ → %s\n", dstCache)
			gitRmTargets = append(gitRmTargets, ".gg/cache/")
			migrated = true
		}
	} else {
		fmt.Println("  ✓ cache/           (not in .gg/ — nothing to migrate)")
	}

	// ── .gitignore alignment ────────────────────────────────────────────────
	gitignorePath := filepath.Join(ggDir, ".gitignore")
	if data, readErr := os.ReadFile(gitignorePath); readErr != nil { //nolint:gosec
		// File missing — write from canonical template.
		if writeErr := os.WriteFile(gitignorePath, []byte(ggGitignoreContent), 0644); writeErr != nil {
			fmt.Printf("  ✗ .gitignore: could not create: %v\n", writeErr)
		} else {
			fmt.Printf("  ✓ .gg/.gitignore created\n")
			migrated = true
		}
	} else {
		content := string(data)
		var missing []string
		for _, line := range gitignoreRequiredLines {
			if !strings.Contains(content, line) {
				missing = append(missing, line)
			}
		}
		if len(missing) > 0 {
			// Append missing entries — preserve existing content.
			f, appendErr := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644) //nolint:gosec
			if appendErr != nil {
				fmt.Printf("  ✗ .gitignore: could not update: %v\n", appendErr)
			} else {
				for _, line := range missing {
					_, _ = fmt.Fprintln(f, line)
				}
				_ = f.Close()
				fmt.Printf("  ✓ .gg/.gitignore updated (added: %s)\n", strings.Join(missing, ", "))
				migrated = true
			}
		} else {
			fmt.Println("  ✓ .gg/.gitignore   (aligned — nothing to do)")
		}
	}

	// ── RULES.md template-drift check ──────────────────────────────────────
	rulesPath := filepath.Join(ggDir, "RULES.md")
	rulesStale, rulesErr := isTemplateDrifted(rulesPath, templates.RulesMD)
	switch {
	case rulesErr != nil:
		fmt.Printf("  ⚠ RULES.md: could not read file: %v\n", rulesErr)
	case rulesStale:
		migrated = true
		fmt.Println()
		fmt.Println("RULES.md template drift detected:")
		fmt.Printf("  Current file: %s\n", rulesPath)
		fmt.Printf("  Template:     embedded (internal/templates/rules.md)\n")
		fmt.Println()
		fmt.Print("Re-render .gg/RULES.md from current template? [y/N] ")
		var answer string
		if _, scanErr := fmt.Scanln(&answer); scanErr != nil || strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Skipped.")
		} else {
			if healErr := healRulesFromTemplate(rulesPath, templates.RulesMD); healErr != nil {
				fmt.Printf("  ✗ RULES.md: re-render failed: %v\n", healErr)
			} else {
				fmt.Printf("  ✓ RULES.md re-rendered from template (backup: %s.bak)\n", rulesPath)
			}
		}
	default:
		fmt.Println("  ✓ RULES.md         (matches current template — nothing to do)")
	}

	fmt.Println()
	if !migrated {
		fmt.Println("Nothing to migrate — runtime state is already in the correct location.")
		return nil
	}

	fmt.Println("Migration complete.")
	if len(gitRmTargets) > 0 {
		fmt.Println()
		fmt.Println("If these paths were tracked by git, un-track them now:")
		fmt.Printf("  git rm --cached %s\n", strings.Join(gitRmTargets, " "))
		fmt.Println()
		fmt.Println("(gg does not commit for you — run the command above manually.)")
	}
	return nil
}

// runDoctorWipeBrain drops all Qdrant collections and Memgraph nodes for
// this project. Intended for round-trip testing (TASK-139, TASK-140).
// Requires --yes to skip the interactive prompt.
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

	// Wipe Qdrant.
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

	// Wipe Memgraph (optional — skip when not configured).
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

// isTemplateDrifted reports whether the file at path differs from the given
// expected content. Returns false (no drift) when the file doesn't exist.
func isTemplateDrifted(path, expected string) (bool, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return string(raw) != expected, nil
}

// healRulesFromTemplate backs up path to path+".bak" (overwriting any previous
// backup) then writes expected verbatim.
func healRulesFromTemplate(path, expected string) error {
	bak := path + ".bak"
	if err := os.Rename(path, bak); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(expected), 0o644); err != nil {
		// Try to restore backup before propagating error.
		_ = os.Rename(bak, path)
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// appendFile reads src and appends its contents to dst (creating dst if needed).
func appendFile(dst, src string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(data)
	return err
}

// copyDir copies a directory tree from src to dst.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, readErr := os.ReadFile(path) //nolint:gosec
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, data, 0o600) //nolint:gosec
	})
}

// hookInstallSettings resolves the installer's walk depth and skip-directory
// list. Precedence:
//
//  1. Values explicitly set in .gg/config.yaml under doctor.hook_install.
//  2. Built-in defaults (config.DefaultHookInstallSkipDirs /
//     config.DefaultHookInstallMaxDepth).
//
// Returning a lookup map keeps the per-directory prune check O(1).
func hookInstallSettings() (skipDirs map[string]bool, maxDepth int) {
	maxDepth = config.DefaultHookInstallMaxDepth
	dirs := config.DefaultHookInstallSkipDirs

	if cfg, err := config.Load(); err == nil {
		if cfg.Doctor.HookInstall.MaxDepth > 0 {
			maxDepth = cfg.Doctor.HookInstall.MaxDepth
		}
		if len(cfg.Doctor.HookInstall.SkipDirs) > 0 {
			dirs = cfg.Doctor.HookInstall.SkipDirs
		}
	}

	skipDirs = make(map[string]bool, len(dirs))
	for _, d := range dirs {
		skipDirs[d] = true
	}
	return skipDirs, maxDepth
}

// runDoctorInstallTaskHooks installs the verify-gate pre-task-done hooks and
// the advisory post-done hook(s) for every language/manifest it finds inside
// the project — including nested monorepo packages. Existing files are
// preserved so repeated runs are idempotent.
//
// Detection (relative paths up to config.DefaultHookInstallMaxDepth, overridable
// via doctor.hook_install.max_depth in .gg/config.yaml):
//   - go.mod         → Go pre-hook (blocking: build + vet + test) + legacy post hook
//   - package.json   → Node/Bun/pnpm/Yarn pre-hook (install + typecheck + build + test)
//
// Scripts are named per detected location so a monorepo with `api/go.mod` and
// `cli/go.mod` produces two separate gate scripts that each cd into their
// own directory before running the checks.
//
// Nothing matches → print a copy-pasteable manual snippet and return without
// error so the caller's exit code stays 0 (no-op is not a failure).
func runDoctorInstallTaskHooks() error {
	ggDir, err := config.GGDir()
	if err != nil {
		return err
	}
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	preDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	postDir := filepath.Join(ggDir, "hooks", "task-done.d")
	if mkErr := os.MkdirAll(preDir, 0o755); mkErr != nil {
		return fmt.Errorf("create pre-hook dir: %w", mkErr)
	}
	if mkErr := os.MkdirAll(postDir, 0o755); mkErr != nil {
		return fmt.Errorf("create post-hook dir: %w", mkErr)
	}

	skipDirs, maxDepth := hookInstallSettings()
	goDirs, err := findManifestDirs(projectRoot, "go.mod", skipDirs, maxDepth)
	if err != nil {
		return fmt.Errorf("walk for go.mod: %w", err)
	}
	nodeDirs, err := findManifestDirs(projectRoot, "package.json", skipDirs, maxDepth)
	if err != nil {
		return fmt.Errorf("walk for package.json: %w", err)
	}

	installed := 0

	for _, sub := range goDirs {
		prePath := filepath.Join(preDir, hookFileName("10-go-verify", sub))
		postPath := filepath.Join(postDir, hookFileName("10-go-quality", sub))

		preBody := strings.ReplaceAll(templates.PreTaskDoneGoHook, "__GG_SUBDIR__", sub)
		// Advisory post-hook is the legacy inline template — rewrite its shell
		// block with the cd prefix if it's running in a subdirectory. For now
		// the template assumes root; wrap it inside a cd subshell when needed.
		postBody := wrapLegacyPostHook(templates.TaskDoneGoHook, sub)

		if n, err := installHookIfAbsent(prePath, preBody,
			fmt.Sprintf("Go verify gate at %s — blocking (build + vet + test)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
		if n, err := installHookIfAbsent(postPath, postBody,
			fmt.Sprintf("Go post-done at %s — advisory (vet + test + lint)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
	}

	for _, sub := range nodeDirs {
		prePath := filepath.Join(preDir, hookFileName("10-node-verify", sub))
		preBody := strings.ReplaceAll(templates.PreTaskDoneNodeHook, "__GG_SUBDIR__", sub)
		if n, err := installHookIfAbsent(prePath, preBody,
			fmt.Sprintf("Node verify gate at %s — blocking (install + typecheck + build + test)", sub)); err != nil {
			return err
		} else {
			installed += n
		}
	}

	// Regression gate: always install 90-bug-repros.sh regardless of language.
	// Off by default (GG_ENFORCEMENT=off); enable with GG_ENFORCEMENT=on.
	bugReprosPath := filepath.Join(preDir, "90-bug-repros.sh")
	if n, err := installHookIfAbsent(bugReprosPath, templates.BugReprosHook,
		"regression gate — runs all repro scripts for fixed bugs (GG_ENFORCEMENT controls blocking)"); err != nil {
		return err
	} else {
		installed += n
	}

	// Test-tier smoke gate (TASK-222): install the hook everywhere but have
	// it self-skip until the project adopts the test-tier Makefile pattern.
	// Filename prefix 05 runs BEFORE language-specific 10-* hooks so a broken
	// test-smoke fails fast, before spending the Go/Node verify-suite budget.
	smokeHookPath := filepath.Join(preDir, "05-smoke-e2e.sh")
	if n, err := installHookIfAbsent(smokeHookPath, templates.SmokeE2EHook,
		"smoke gate — runs `make test-smoke` when the target exists (skips quietly otherwise)"); err != nil {
		return err
	} else {
		installed += n
	}

	// Makefile tier template: written to .gg/templates/ so humans can discover
	// + opt-in via `include .gg/templates/makefile-test-tiers.mk`. Never
	// auto-edits the project Makefile — opt-in is the whole point.
	tmplDir := filepath.Join(ggDir, "templates")
	if mkErr := os.MkdirAll(tmplDir, 0o755); mkErr != nil {
		return fmt.Errorf("create templates dir: %w", mkErr)
	}
	mkTierPath := filepath.Join(tmplDir, "makefile-test-tiers.mk")
	if _, statErr := os.Stat(mkTierPath); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(mkTierPath, []byte(templates.MakefileTestTiers), 0o644); writeErr != nil {
			return fmt.Errorf("write %s: %w", mkTierPath, writeErr)
		}
		fmt.Printf("  ✓ template .gg/templates/makefile-test-tiers.mk written — add `include` line to adopt\n")
		installed++
	}

	// Offer to add the include line to an existing Makefile. The prompt is
	// non-destructive: we only append, and only when the user says yes.
	makefilePath := filepath.Join(projectRoot, "Makefile")
	if _, statErr := os.Stat(makefilePath); statErr == nil {
		if err := offerMakefileTestTierInclude(makefilePath, mkTierPath, projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ Makefile update skipped: %v\n", err)
		}
	}

	if len(goDirs) == 0 && len(nodeDirs) == 0 {
		fmt.Println("No go.mod or package.json found in the project (walked up to depth", maxDepth, ").")
		fmt.Println("Write your own verify gate at:")
		fmt.Printf("  %s/10-custom-verify.sh\n", preDir)
		fmt.Println("Env vars your script receives: GG_TASK_ID, GG_TASK_SUMMARY, GG_PROJECT_ID, GG_ACTOR.")
		fmt.Println("Non-zero exit blocks `gg task done` with exit code 7.")
		return nil
	}

	// Record installed artifacts in .gg/installed.json so `gg doctor --sync-artifacts`
	// can detect drift when the CLI is upgraded.
	recordTaskHookArtifacts(ggDir)

	if installed == 0 {
		fmt.Println("All detected hooks already present — nothing to do (remove files manually to reinstall).")
		return nil
	}

	fmt.Println()
	fmt.Println("Verify gate is live. Next `gg task done` run will:")
	fmt.Printf("  1. execute %s/*.sh in order (any non-zero exit → block, exit 7)\n", preDir)
	fmt.Printf("  2. write the new state to the store on success\n")
	fmt.Printf("  3. run %s/*.sh as advisory (warnings only unless hooks.strict=true)\n", postDir)
	fmt.Println()
	fmt.Println("Edit the scripts freely — they ship as starting points, not contracts.")
	return nil
}

// recordTaskHookArtifacts stamps .gg/installed.json with the current CLI
// template SHAs for all task-hook artifacts. Called after install so that
// `gg doctor --sync-artifacts` knows what version was installed.
// Errors are silently swallowed — manifest drift is advisory, never fatal.
func recordTaskHookArtifacts(ggDir string) {
	current := templates.ArtifactSHAs()
	m, err := artifacts.Read(ggDir)
	if err != nil {
		return
	}
	for k, sha := range current {
		m.Artifacts[k] = sha
	}
	_ = artifacts.Write(ggDir, m)
}

// findManifestDirs walks root up to maxDepth and returns the list of
// directories (relative to root, using '/' as separator) that contain a file
// named manifestName. Slash-delimited "." represents the root. Directories
// whose base name is a key in skipDirs are pruned from the walk.
//
// filepath.WalkDir does not follow symbolic links, so a symlinked package
// directory (e.g. `packages/foo -> ../../shared/foo`) is not descended into;
// write a manual hook if your monorepo depends on symlinked subprojects.
// Directories in skipDirs (typically including ".gg") are also pruned, so a
// nested gg project living at `services/api/.gg/` is invisible to the outer
// walk by design — each nested project has its own installer run.
func findManifestDirs(root, manifestName string, skipDirs map[string]bool, maxDepth int) ([]string, error) {
	var out []string
	rootClean := filepath.Clean(root)

	err := filepath.WalkDir(rootClean, func(path string, de os.DirEntry, err error) error {
		if err != nil {
			// Permission denied on a subtree should not abort the whole walk;
			// skip it and continue so the user still gets partial results.
			if de != nil && de.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if de.IsDir() {
			if path == rootClean {
				return nil
			}
			rel, relErr := filepath.Rel(rootClean, path)
			if relErr != nil {
				return nil
			}
			// Prune expensive / irrelevant directories by name.
			if skipDirs[de.Name()] {
				return filepath.SkipDir
			}
			depth := len(strings.Split(filepath.ToSlash(rel), "/"))
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if de.Name() != manifestName {
			return nil
		}
		rel, relErr := filepath.Rel(rootClean, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// hookFileName builds the target script filename for a given relative dir.
// Root (".") keeps the bare base name so existing installs don't churn;
// nested paths get a slug suffix so two Go modules land in distinct files.
func hookFileName(base, sub string) string {
	if sub == "." || sub == "" {
		return base + ".sh"
	}
	slug := strings.ReplaceAll(sub, "/", "-")
	return base + "-" + slug + ".sh"
}

// wrapLegacyPostHook returns the post-hook body adjusted so commands run from
// the manifest directory. The legacy template (task-done-go.sh) assumes root;
// if the manifest lives in a subdir we wrap the body with a cd line so the
// same template keeps working without churn.
func wrapLegacyPostHook(body, sub string) string {
	if sub == "." || sub == "" {
		return body
	}
	header := "#!/bin/sh\n# Auto-wrapped by gg installer — original body runs inside " + sub + "\nset -e\ncd \"$(dirname \"$0\")/../../..\"\ncd \"" + sub + "\"\n\n"
	// Drop the original shebang and any leading blank so the wrapped script
	// has exactly one shebang line.
	trimmed := body
	if strings.HasPrefix(trimmed, "#!/") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
	}
	return header + trimmed
}

// offerMakefileTestTierInclude checks whether makefilePath already includes the
// test-tier template. If not, it prompts the user interactively and appends the
// include line on confirmation. The function is intentionally non-destructive:
// it never modifies existing content — only appends — and always asks first.
func offerMakefileTestTierInclude(makefilePath, tierTemplatePath, projectRoot string) error {
	data, err := os.ReadFile(makefilePath) //nolint:gosec
	if err != nil {
		return fmt.Errorf("read Makefile: %w", err)
	}
	if strings.Contains(string(data), "makefile-test-tiers.mk") {
		fmt.Println("  ✓ Makefile already includes test-tier template — nothing to do")
		return nil
	}

	rel, _ := filepath.Rel(projectRoot, tierTemplatePath)
	includeLine := "include " + filepath.ToSlash(rel)

	fmt.Printf("\nMakefile found at %s.\n", makefilePath)
	fmt.Printf("Add test-tier targets (test-unit / test-integration / test-smoke / test-e2e)? [y/N] ")
	fmt.Printf("  This appends: %s\n", includeLine)

	var answer string
	if _, scanErr := fmt.Scanln(&answer); scanErr != nil || strings.ToLower(strings.TrimSpace(answer)) != "y" {
		fmt.Println("  Skipped — add the include manually when ready.")
		return nil
	}

	f, openErr := os.OpenFile(makefilePath, os.O_APPEND|os.O_WRONLY, 0o644) //nolint:gosec
	if openErr != nil {
		return fmt.Errorf("open Makefile: %w", openErr)
	}
	defer func() { _ = f.Close() }()
	if _, writeErr := fmt.Fprintf(f, "\n# gg test-tier targets\n%s\n", includeLine); writeErr != nil {
		return fmt.Errorf("append to Makefile: %w", writeErr)
	}
	fmt.Printf("  ✓ Added '%s' to Makefile\n", includeLine)
	return nil
}

// installHookIfAbsent writes body to path with 0755 permissions, unless a file
// already exists there. Returns 1 if the file was written, 0 if skipped.
// Prints a line for each outcome so the user can see what happened.
func installHookIfAbsent(path, body, summary string) (int, error) {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("⚠ %s already exists — skipping (remove it manually to reinstall)\n", path)
		return 0, nil
	}
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		return 0, fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Printf("✓ Installed %s\n", path)
	fmt.Printf("  %s\n", summary)
	return 1, nil
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
