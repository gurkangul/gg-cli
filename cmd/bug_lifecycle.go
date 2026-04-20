package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
)

var bugFixCmd = &cobra.Command{
	Use:   `fix BUG-ID "summary"`,
	Short: "Mark a bug as fixed",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugFix,
}

var bugStartCmd = &cobra.Command{
	Use:   "start BUG-ID",
	Short: "Move a bug to 'fixing' status",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugStart,
}

var bugWontFixCmd = &cobra.Command{
	Use:   `wontfix BUG-ID "reason"`,
	Short: "Close a bug as won't-fix",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugWontFix,
}

var bugAttachReproCmd = &cobra.Command{
	Use:   "attach-repro BUG-ID <repro-path>",
	Short: "Attach a repro script to an already-fixed bug",
	Long:  "Backfill the repro_script field on an existing bug. Path must be executable or a *_test.go file.",
	Args:  cobra.ExactArgs(2),
	RunE:  runBugAttachRepro,
}

var (
	bugRootCause     string
	bugFixFiles      string
	bugFixSymbols    string
	bugFixRepro      string
	bugFixBrokenRef  string
	bugWontFixRepro  string
)

func init() {
	bugFixCmd.Flags().StringVar(&bugRootCause, "root-cause", "", "root cause identified during fix")
	bugFixCmd.Flags().StringVar(&bugFixFiles, "files", "", "comma-separated source file paths affected by this fix")
	bugFixCmd.Flags().StringVar(&bugFixSymbols, "symbols", "", "comma-separated symbol names affected by this fix")
	bugFixCmd.Flags().StringVar(&bugFixRepro, "repro", "", "path to repro script or *_test.go that guards against regression (required)")
	bugFixCmd.Flags().StringVar(&bugFixBrokenRef, "repro-broken-ref", "", "git SHA where the repro MUST fail — proves the bug existed before the fix")
	bugWontFixCmd.Flags().StringVar(&bugWontFixRepro, "repro", "", "path to repro script or *_test.go documenting the confirmed failure mode (required)")
	addFromFlag(bugFixCmd)
	addFromFlag(bugWontFixCmd)
	bugCmd.AddCommand(bugFixCmd)
	bugCmd.AddCommand(bugStartCmd)
	bugCmd.AddCommand(bugWontFixCmd)
	bugCmd.AddCommand(bugAttachReproCmd)
}

// validateReproPath checks that --repro names an existing file that is either
// executable (any +x bit) or a Go test file (*_test.go).
func validateReproPath(path string) error {
	if path == "" {
		return fmt.Errorf("--repro is required: provide a path to a repro script or *_test.go file")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("--repro %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("--repro %q is a directory, not a file", path)
	}
	isTestFile := strings.HasSuffix(path, "_test.go")
	isExecutable := info.Mode().Perm()&0111 != 0
	if !isTestFile && !isExecutable {
		return fmt.Errorf("--repro %q must be executable or a *_test.go file", path)
	}
	return nil
}

// verifyReproWithWorktree proves a repro fails at brokenRef (pre-fix) and
// passes at HEAD (post-fix). A repro that is green at the broken ref means it
// never actually exercised the failing path — rejected with ExitVerifyFailed.
func verifyReproWithWorktree(ctx context.Context, reproPath, brokenRef string) error {
	wtDir, err := os.MkdirTemp("", "gg-verify-*")
	if err != nil {
		return fmt.Errorf("broken-ref verify: create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(wtDir) }()

	// Create an isolated git worktree at the broken commit.
	addOut, err := gitCmd(ctx, "worktree", "add", "--detach", wtDir, brokenRef)
	if err != nil {
		return fmt.Errorf("broken-ref verify: git worktree add %s: %w\n%s", brokenRef, err, addOut)
	}
	defer func() {
		rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = gitCmd(rmCtx, "worktree", "remove", "--force", wtDir)
	}()

	// Resolve repro path: keep absolute paths as-is; make relative paths
	// absolute from the current working directory (project root at call time).
	absRepro := reproPath
	if !filepath.IsAbs(absRepro) {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			return fmt.Errorf("broken-ref verify: resolve cwd: %w", cwdErr)
		}
		absRepro = filepath.Join(cwd, absRepro)
	}

	// Build the repro command. Test files run via `go test`; shell scripts run directly.
	buildReproCmd := func(dir string) *exec.Cmd {
		if strings.HasSuffix(reproPath, "_test.go") {
			testName := extractGoTestName(absRepro)
			//nolint:gosec
			return exec.CommandContext(ctx, "go", "test", "-run", testName, "-count=1", "./...")
		}
		//nolint:gosec
		return exec.CommandContext(ctx, "sh", absRepro)
	}

	// Step 1: repro MUST fail at brokenRef.
	preCmd := buildReproCmd(wtDir)
	preCmd.Dir = wtDir
	preOut, preErr := preCmd.CombinedOutput()
	if preErr == nil {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"broken-ref verify FAILED: repro exited 0 at %s — it does not prove the bug existed pre-fix\nrepro output:\n%s",
				brokenRef, trimTail(string(preOut), 15),
			),
		}
	}

	// Step 2: repro MUST pass at HEAD (current working dir).
	postCmd := buildReproCmd(".")
	postOut, postErr := postCmd.CombinedOutput()
	if postErr != nil {
		return &ExitError{
			Code: ExitVerifyFailed,
			Message: fmt.Sprintf(
				"broken-ref verify FAILED: repro exited non-zero at HEAD — fix is not yet in place\nrepro output:\n%s",
				trimTail(string(postOut), 15),
			),
		}
	}

	return nil
}

// gitCmd runs a git subcommand and returns combined output.
func gitCmd(ctx context.Context, args ...string) (string, error) {
	//nolint:gosec
	c := exec.CommandContext(ctx, "git", args...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// extractGoTestName returns the first exported Test* function name found in
// the given *_test.go file, or "." (match all) as fallback.
func extractGoTestName(path string) string {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return "."
	}
	re := regexp.MustCompile(`(?m)^func (Test\w+)\(`)
	if m := re.FindSubmatch(data); len(m) == 2 {
		return string(m[1])
	}
	return "."
}

// trimTail returns the last n lines of s, joined by newlines.
func trimTail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func runBugFix(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	summary, err := requireNonEmpty("summary", args[1])
	if err != nil {
		return err
	}
	rootCause, err := requireNonEmpty("--root-cause", bugRootCause)
	if err != nil {
		return fmt.Errorf("%w (root cause must be the underlying defect, not the symptom — required to close a bug)", err)
	}
	if err := validateReproPath(bugFixRepro); err != nil {
		return err
	}

	// Check config for require_broken_ref enforcement.
	brokenRef := bugFixBrokenRef
	if brokenRef == "" {
		if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil && cfg.Bugs.RequireBrokenRef {
			return fmt.Errorf("--repro-broken-ref is required: set bugs.require_broken_ref=false in .gg/config.yaml to disable (current config enforces pre-fix failure proof)")
		}
	}
	if brokenRef != "" {
		if err := verifyReproWithWorktree(cmd.Context(), bugFixRepro, brokenRef); err != nil {
			return err
		}
	}

	if err := requireAgentIdentity(); err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := runInboxGatePreflight(ctx, d.store, "bug-fix"); err != nil {
		return err
	}

	if err := d.store.FixBug(ctx, bugID, rootCause, summary, bugFixRepro); err != nil {
		return err
	}

	// Replace Bug→File and Bug→Symbol edges in Memgraph when provided.
	// ReplaceBugAffects deletes stale edges before re-creating them so scope
	// corrections don't accumulate alongside the original report's edges.
	fixFiles := normalizeBugFiles(parseTags(bugFixFiles))
	fixSymbols := parseTags(bugFixSymbols)
	if len(fixFiles) > 0 || len(fixSymbols) > 0 {
		if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil && cfg.Memgraph.URI != "" {
			if gc, gcErr := graph.New(&cfg.Memgraph, cfg.ProjectID); gcErr == nil {
				gctx, gcancel := withTimeout(cmd.Context())
				defer gcancel()
				// Fetch the bug title for the node label.
				bug, getErr := d.store.GetBug(ctx, bugID)
				title := bugID
				if getErr == nil && bug != nil {
					title = bug.Title
				}
				if mergeErr := gc.ReplaceBugAffects(gctx, bugID, title, fixFiles, fixSymbols); mergeErr != nil {
					fmt.Printf("~ graph edges skipped: %v\n", mergeErr)
				}
				_ = gc.Close(gctx)
			}
		}
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixed", "summary": summary, "root_cause": rootCause, "repro_script": bugFixRepro}, func() {
		fmt.Printf("✓ %s marked as fixed\n", bugID)
		fmt.Printf("  Root cause: %s\n", rootCause)
		fmt.Printf("  Repro: %s\n", bugFixRepro)
	})
}

func runBugStart(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.StartFixingBug(ctx, bugID); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "fixing"}, func() {
		fmt.Printf("→ %s marked as fixing\n", bugID)
	})
}

func runBugWontFix(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	reason, err := requireNonEmpty("reason", args[1])
	if err != nil {
		return err
	}
	if err := validateReproPath(bugWontFixRepro); err != nil {
		return err
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.WontFixBug(ctx, bugID, reason, bugWontFixRepro); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "status": "wontfix", "reason": reason, "repro_script": bugWontFixRepro}, func() {
		fmt.Printf("– %s marked as wontfix\n", bugID)
		fmt.Printf("  Repro: %s\n", bugWontFixRepro)
	})
}

func runBugAttachRepro(cmd *cobra.Command, args []string) error {
	bugID, err := requireBugID(args[0])
	if err != nil {
		return err
	}
	if err := validateReproPath(args[1]); err != nil {
		return err
	}
	reproPath := args[1]

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if err := d.store.SetBugReproScript(ctx, bugID, reproPath); err != nil {
		return err
	}

	return printJSON(map[string]any{"id": bugID, "repro_script": reproPath}, func() {
		fmt.Printf("✓ %s repro attached: %s\n", bugID, reproPath)
	})
}
