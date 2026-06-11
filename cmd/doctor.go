package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/enforcement"
	"github.com/gurkangul/gg-cli/internal/session"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and repair gg configuration",
	Long: `Check service connectivity and verify that required indexer binaries
are available. Use --install-indexers to automatically install missing SCIP
binaries with maintained package-manager flows (go, TypeScript, Python).
Swift detection is supported, but Swift graph generation requires a user-provided
compatible scip-swift binary because gg does not bundle one.

Doctor reports the same CodeGraph freshness contract used by session-start,
next, impact, and index status. It never starts a background index daemon;
use --fix-index for explicit repair or gg index --watch / gg watch --index for
optional foreground active mode.

Exit codes:
  0  all checks passed
  1  one or more checks failed`,
	RunE: runDoctor,
}

var (
	doctorInstallIndexers      bool
	doctorReconcile            bool
	doctorInstallAgentHooks    bool
	doctorInstallAgentsMD      bool
	doctorHooksAgent           string
	doctorHooksDryRun          bool
	doctorHooksForce           bool
	doctorHeal                 bool
	doctorWipeBrain            bool
	doctorWipeBrainYes         bool
	doctorInstallTaskHooks     bool
	doctorInstallIndexHooks    bool
	doctorSyncArtifacts        bool
	doctorSyncApply            bool
	doctorBypassAudit          bool
	doctorBypassAuditSince     string
	doctorCheckContract        bool
	doctorContractFix          bool
	doctorContractForceReset   bool
	doctorSyncBaseline         bool
	doctorCaptureLintBaseline  bool
	doctorCheckBinary          bool
	doctorFixBinary            bool
	doctorInstallSecretScanner bool
	doctorCheckSecrets         bool
	doctorCheckSecretsStaged   bool
	doctorCheckSecretsHistory  bool
	doctorDiagnoseSandbox      bool
	doctorRefreshHooks         bool
	doctorRefreshHooksForce    bool
	doctorFixIndex             bool
	doctorFixGitIdentity       bool
)

func init() {
	doctorCmd.Flags().BoolVar(&doctorInstallIndexers, "install-indexers", false,
		"install missing SCIP indexer binaries with maintained installers (scip-go, scip-typescript, scip-python)")
	doctorCmd.Flags().BoolVar(&doctorReconcile, "reconcile", false,
		"scan the outbox for incomplete dual-store writes and report what needs repair")
	doctorCmd.Flags().BoolVar(&doctorInstallAgentHooks, "install-agent-hooks", false,
		"write agent-side config (SessionStart hook / alwaysApply rule / read-preload) to enforce gg usage")
	doctorCmd.Flags().StringVar(&doctorHooksAgent, "agent", "",
		"restrict --install-agent-hooks to a single agent (claude, cursor, codex, bmad, gsd)")
	doctorCmd.Flags().BoolVar(&doctorHooksDryRun, "dry-run", false,
		"with --install-agent-hooks: report what would change without writing anything")
	doctorCmd.Flags().BoolVar(&doctorHeal, "heal", false,
		"migrate legacy .gg/telemetry.jsonl and .gg/cache/ to ~/.gg/projects/<id>/ (idempotent)")
	doctorCmd.Flags().BoolVar(&doctorHooksForce, "force", false,
		"with --install-agent-hooks: bypass detection and install for the named agent(s)")
	doctorCmd.Flags().BoolVar(&doctorWipeBrain, "wipe-brain", false,
		"drop all Qdrant collections and Memgraph nodes for this project (destructive — use for testing)")
	doctorCmd.Flags().BoolVar(&doctorWipeBrainYes, "yes", false,
		"auto-accept confirmations for state-changing doctor flows (--wipe-brain, --heal RULES.md re-render, --install-task-hooks Makefile test-tier); also honored via GG_YES=1")
	doctorCmd.Flags().BoolVar(&doctorInstallTaskHooks, "install-task-hooks", false,
		"install verify-gate (pre-task-done.d) + post-done task-done.d hooks; auto-detects Go (go.mod) and/or Node/Bun (package.json)")
	doctorCmd.Flags().BoolVar(&doctorInstallIndexHooks, "install-index-hooks", false,
		"install opt-in git hooks (pre-push + post-merge) that run gg index --changed to keep the local CodeGraph fresh; foreground + non-blocking, not a daemon")
	doctorCmd.Flags().BoolVar(&doctorInstallAgentsMD, "install-agents-md", false,
		"inject the gg tracker-rules managed block into AGENTS.md (idempotent; alias for --install-agent-hooks --agent codex)")
	doctorCmd.Flags().BoolVar(&doctorSyncArtifacts, "sync-artifacts", false,
		"compare .gg/installed.json against the current CLI templates and show a drift table")
	doctorCmd.Flags().BoolVar(&doctorSyncApply, "apply", false,
		"with --sync-artifacts: re-install drifted or missing artifacts")
	doctorCmd.Flags().BoolVar(&doctorBypassAudit, "bypass-audit", false,
		"list GG_ENFORCEMENT=off bypass events from ~/.gg/projects/<id>/state.json (default: last 7d)")
	doctorCmd.Flags().StringVar(&doctorBypassAuditSince, "bypass-since", "7d",
		"with --bypass-audit: time window (7d, 24h, 30d, or RFC3339 timestamp)")
	doctorCmd.Flags().BoolVar(&doctorCheckContract, "check-contract", false,
		"compare the managed contract block in each agent's entry-point file against the current template (exit 1 on drift)")
	doctorCmd.Flags().BoolVar(&doctorContractFix, "fix", false,
		"with --check-contract: repair STALE and MISSING entries; refuses DRIFTED without --force-reset")
	doctorCmd.Flags().BoolVar(&doctorContractForceReset, "force-reset", false,
		"with --check-contract --fix: overwrite manually-edited (DRIFTED) contract blocks")
	doctorCmd.Flags().BoolVar(&doctorSyncBaseline, "sync-baseline", false,
		"rescan project and refresh .gg/file-size-baseline.json with current line counts")
	doctorCmd.Flags().BoolVar(&doctorCaptureLintBaseline, "capture-lint-baseline", false,
		"run golangci-lint and write .gg/lint-baseline.json; the 60-lint-gate.sh pre-done hook uses this to block new warnings")
	doctorCmd.Flags().BoolVar(&doctorCheckBinary, "check-binary", false,
		"verify the installed gg binary is not older than the HEAD commit of the local gg-cli source")
	doctorCmd.Flags().BoolVar(&doctorFixBinary, "fix-binary", false,
		"with --check-binary: rebuild and reinstall gg from the local source checkout when the binary is stale")
	doctorCmd.Flags().BoolVar(&doctorInstallSecretScanner, "install-secret-scanner", false,
		"download and install the pinned gitleaks binary into ~/.gg/bin/gitleaks (checksum-verified)")
	doctorCmd.Flags().BoolVar(&doctorCheckSecrets, "check-secrets", false,
		"scan the repo for secrets using gitleaks (staged + history); falls back to narrow-regex scan when gitleaks is absent")
	doctorCmd.Flags().BoolVar(&doctorCheckSecretsStaged, "staged", false,
		"with --check-secrets: run staged/working-tree scan only (gitleaks detect --no-git)")
	doctorCmd.Flags().BoolVar(&doctorCheckSecretsHistory, "history", false,
		"with --check-secrets: run full git history scan only (gitleaks detect)")
	doctorCmd.Flags().BoolVar(&doctorDiagnoseSandbox, "diagnose-sandbox", false,
		"probe localhost TCP to detect sandbox restrictions; reports 'TCP localhost permitted' or 'TCP localhost BLOCKED'")
	doctorCmd.Flags().BoolVar(&doctorRefreshHooks, "refresh-hooks", false,
		"overwrite drifted gg-managed hook templates after backing up each stale copy")
	doctorCmd.Flags().BoolVar(&doctorRefreshHooksForce, "refresh-hooks-force", false,
		"with --refresh-hooks: also overwrite user-customized hooks that lack gg-template markers")
	doctorCmd.Flags().BoolVar(&doctorFixIndex, "fix-index", false,
		"refresh a missing or stale code graph by running the recommended gg index command(s)")
	doctorCmd.Flags().BoolVar(&doctorFixGitIdentity, "fix-git-identity", false,
		"reset a repo-local agent git identity so commits are attributed to you (the human)")
	rootCmd.AddCommand(doctorCmd)
}

// indexerSpec describes how to install a SCIP binary.
type indexerSpec struct {
	Binary  string   // name passed to runner.ResolveIndexer
	Lang    string   // language whose manifests make this indexer required
	Install []string // command + args for native package manager
	Note    string   // displayed after a failed install
}

// indexers lists the SCIP binaries gg needs, with install commands.
var indexers = []indexerSpec{
	{
		Binary:  "scip-go",
		Lang:    "go",
		Install: []string{"go", "install", "github.com/sourcegraph/scip-go/cmd/scip-go@latest"},
		Note:    "requires Go 1.21+",
	},
	{
		Binary:  "scip-typescript",
		Lang:    "typescript",
		Install: []string{"npm", "install", "-g", "@sourcegraph/scip-typescript"},
		Note:    "requires Node.js 18+",
	},
	{
		Binary:  "scip-python",
		Lang:    "python",
		Install: []string{"npm", "install", "-g", "@sourcegraph/scip-python"},
		Note:    "requires Node.js 18+",
	},
	{
		Binary: "scip-swift",
		Lang:   "swift",
		Note:   "Swift CodeGraph support is externally backed: provide a compatible scip-swift on PATH or ~/.gg/bin. Contract: scip-swift index --output <file> <project-root>",
	},
}

// doctorCheck is one structured check result for the --json payload.
type doctorCheck struct {
	Label  string `json:"label"`
	Status string `json:"status"` // pass | warn | fail
	Detail string `json:"detail"`
}

// doctorReport accumulates check results and tracks whether any problems were
// found. Every check is recorded in checks so `gg doctor --json` can emit a
// stable machine-readable object; human text is suppressed under --json so
// stdout stays valid JSON.
type doctorReport struct {
	problems int
	checks   []doctorCheck
}

func (r *doctorReport) ok(label, detail string) {
	r.checks = append(r.checks, doctorCheck{Label: label, Status: "pass", Detail: detail})
	if jsonOutput {
		return
	}
	fmt.Printf("  ✓ %-28s %s\n", label, detail)
}

func (r *doctorReport) warn(label, detail string) {
	r.checks = append(r.checks, doctorCheck{Label: label, Status: "warn", Detail: detail})
	if jsonOutput {
		return
	}
	fmt.Printf("  ~ %-28s %s\n", label, detail)
}

func (r *doctorReport) fail(label, detail string) {
	r.problems++
	r.checks = append(r.checks, doctorCheck{Label: label, Status: "fail", Detail: detail})
	if jsonOutput {
		return
	}
	fmt.Printf("  ✗ %-28s %s\n", label, detail)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	if doctorReconcile {
		return runDoctorReconcile(cmd)
	}
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
	if doctorHeal {
		return runDoctorHeal()
	}
	if doctorWipeBrain {
		return runDoctorWipeBrain(cmd)
	}
	if doctorInstallTaskHooks {
		if !enforcement.Enabled() {
			fmt.Println("gg doctor --install-task-hooks: " + enforcement.DisabledNotice)
			return nil
		}
		return runDoctorInstallTaskHooks()
	}
	if doctorInstallIndexHooks {
		root, err := config.FindRoot()
		if err != nil {
			return err
		}
		fmt.Println("GG Doctor — Install CodeGraph index hooks")
		fmt.Println(strings.Repeat("─", 50))
		return installGitIndexHooks(root)
	}
	if doctorSyncArtifacts {
		return runDoctorSyncArtifacts(doctorSyncApply)
	}
	if doctorBypassAudit {
		return runDoctorBypassAudit(doctorBypassAuditSince)
	}
	if doctorCheckContract {
		return runDoctorCheckContract(doctorContractFix, doctorContractForceReset)
	}
	if doctorSyncBaseline {
		return runDoctorSyncBaseline()
	}
	if doctorCaptureLintBaseline {
		return runDoctorCaptureLintBaseline()
	}
	if doctorCheckBinary {
		return runDoctorCheckBinary(doctorFixBinary)
	}
	if doctorInstallSecretScanner {
		return runDoctorInstallSecretScanner()
	}
	if doctorCheckSecrets {
		return runDoctorCheckSecrets(doctorCheckSecretsStaged, doctorCheckSecretsHistory)
	}
	if doctorDiagnoseSandbox {
		return runDoctorDiagnoseSandbox(cmd)
	}
	if doctorRefreshHooks || doctorRefreshHooksForce {
		return runDoctorRefreshHooks(doctorRefreshHooksForce)
	}
	if doctorFixIndex {
		return runDoctorFixIndex(cmd)
	}
	if doctorFixGitIdentity {
		return runDoctorFixGitIdentity(cmd)
	}

	doctorPrintln("GG Doctor")
	doctorPrintln(strings.Repeat("─", 50))
	if !enforcement.Enabled() {
		doctorPrintf("⚠ guards=off  (unset %s or set =on to re-enable: install-agent-hooks, gsd-guard, pre-task-done gate)\n", enforcement.EnvVar)
	}

	report := &doctorReport{}

	// 1. Config validation
	cfg := doctorCheckConfig(report)

	// 1b. Git commit identity (TASK-480): warn if commits would be attributed to
	// an agent instead of the human. Routed through the report so it lands in the
	// --json checks array too, with a bespoke human ⚠ line preserved.
	if warn := gitCommitIdentityWarning(); warn != "" {
		report.checks = append(report.checks, doctorCheck{Label: "git commit identity", Status: "warn", Detail: warn})
		doctorPrintf("  ⚠ git commit identity     %s\n", warn)
	} else {
		report.checks = append(report.checks, doctorCheck{Label: "git commit identity", Status: "pass", Detail: "human (" + gitConfigValue("user.name") + ")"})
		doctorPrintf("  ✓ %-28s %s\n", "git commit identity", "human ("+gitConfigValue("user.name")+")")
	}

	// 2. Indexer binaries
	doctorPrintln("\nIndexer Binaries:")
	checkIndexers(cmd, report)

	// 3. Service checks (only if config is valid)
	doctorPrintln("\nService Connectivity:")
	if cfg != nil {
		doctorCheckQdrant(cmd, cfg, report)
		doctorCheckMemgraph(cmd, cfg, report)
		doctorCheckOllama(cmd, cfg, report)
	} else {
		report.warn("services", "skipped — config invalid")
	}

	// 4. Project structure
	doctorPrintln("\nProject Structure:")
	doctorCheckProjectStructure(report)

	// 4b. Code graph freshness — doctor OK means this is ready too, not just file presence.
	doctorPrintln("\nCode graph freshness:")
	doctorCheckCodeGraphFreshness(cmd, cfg, report)

	// 5. Outbox check — surface pending derived-store replay without repairing it.
	doctorPrintln("\nOutbox (derived-store replay):")
	doctorCheckOutbox(report)

	// 5b. JSONL integrity — warn on malformed lines in brain files.
	doctorPrintln("\nBrain JSONL integrity:")
	doctorCheckBrainJSONL(report)

	// 6. Binary freshness — advisory warn, never blocks default run.
	doctorPrintln("\nBinary:")
	doctorCheckBinaryAdvisory(report)

	// 7. Hook template drift — marker-backed, blocks on drift but not customization.
	doctorPrintln("\nHook templates:")
	doctorCheckHookTemplates(report)

	// Artifact drift check (advisory — does not count as a problem).
	driftCount := doctorCheckArtifactDrift()

	// --json: emit the stable machine-readable object and stop. No human text,
	// no paste-block, so stdout is valid JSON only.
	if jsonOutput {
		return writeJSON(buildDoctorJSON(report, driftCount))
	}

	// Summary
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	if report.problems == 0 {
		fmt.Println("Core checks passed (connectivity, configured services, code graph freshness, JSONL integrity, and placeholder-vector scan).")
		fmt.Println("Note: deep JSONL↔Qdrant reconciliation is not part of default doctor; run `gg doctor --reconcile` after crashes or suspected drift.")
		if driftCount > 0 {
			fmt.Printf("  ⚠ %d artifact(s) drifted from CLI templates — run `gg doctor --sync-artifacts` to inspect\n", driftCount)
		}
		// Discoverability tips (TASK-503): advisory one-liners. The GG_TRACE tip
		// is shown only when tracing is OFF so it never nags users who already
		// opted in. The outbox tip fires only when there is a real backlog.
		renderDoctorTips(report)
		fmt.Println()
		fmt.Println("Paste this into your AI agent's chat (works for any agent):")
		fmt.Println()
		fmt.Println("────────────────────────────────────────────────────────────")
		fmt.Print(session.PasteBlock("agent"))
		fmt.Println("────────────────────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Install agent hooks: `gg doctor --install-agent-hooks`")
		return nil
	}
	renderDoctorTips(report)
	return fmt.Errorf("%d problem(s) found — fix the issues above and re-run `gg doctor`", report.problems)
}

// doctorPrintln/doctorPrintf write human text to stdout only when --json is
// off, so the JSON path never interleaves prose into the JSON object.
func doctorPrintln(a ...any) {
	if jsonOutput {
		return
	}
	fmt.Println(a...)
}

func doctorPrintf(format string, a ...any) {
	if jsonOutput {
		return
	}
	fmt.Printf(format, a...)
}
