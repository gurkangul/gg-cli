package cmd

import (
	"fmt"
	"os"
	"strings"

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

var (
	bugRootCause    string
	bugFixFiles     string
	bugFixSymbols   string
	bugFixRepro     string
	bugWontFixRepro string
)

func init() {
	bugFixCmd.Flags().StringVar(&bugRootCause, "root-cause", "", "root cause identified during fix")
	bugFixCmd.Flags().StringVar(&bugFixFiles, "files", "", "comma-separated source file paths affected by this fix")
	bugFixCmd.Flags().StringVar(&bugFixSymbols, "symbols", "", "comma-separated symbol names affected by this fix")
	bugFixCmd.Flags().StringVar(&bugFixRepro, "repro", "", "path to repro script or *_test.go that guards against regression (required)")
	bugWontFixCmd.Flags().StringVar(&bugWontFixRepro, "repro", "", "path to repro script or *_test.go documenting the confirmed failure mode (required)")
	addFromFlag(bugFixCmd)
	addFromFlag(bugWontFixCmd)
	bugCmd.AddCommand(bugFixCmd)
	bugCmd.AddCommand(bugStartCmd)
	bugCmd.AddCommand(bugWontFixCmd)
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

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

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
