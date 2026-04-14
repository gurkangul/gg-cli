package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/adapters"
)

var adaptCmd = &cobra.Command{
	Use:   "adapt",
	Short: "Inject gg rules into AI coding agent convention files",
	Long: `Inject the gg protocol from AGENTS.md into the convention files of
supported AI coding agents.

Supported agents: claude-code, cursor, aider, cline, windsurf, roo, zed, codex, continue

Format modes:
  managed-block   Inserts between <!-- gg-managed-start --> / <!-- gg-managed-end -->
                  markers. User-authored content outside the block is preserved.
  full-replace    gg owns the entire file (marked with a header comment).
  symlink         Target IS AGENTS.md; no injection needed (codex only).

Examples:
  gg adapt --list                  # list supported agents
  gg adapt --agent cursor          # inject into .cursor/rules or .cursorrules
  gg adapt --agent aider           # inject managed block into CONVENTIONS.md
  gg adapt --all                   # auto-detect and inject all present agents
  gg adapt --check                 # check all adapted files for drift`,
	RunE: runAdapt,
}

var (
	adaptAgent string
	adaptAll   bool
	adaptList  bool
	adaptCheck bool
)

func init() {
	adaptCmd.Flags().StringVar(&adaptAgent, "agent", "", "inject for a specific agent")
	adaptCmd.Flags().BoolVar(&adaptAll, "all", false, "inject for all auto-detected agents")
	adaptCmd.Flags().BoolVar(&adaptList, "list", false, "list supported agents and their target files")
	adaptCmd.Flags().BoolVar(&adaptCheck, "check", false, "check adapted files for drift from AGENTS.md")
	rootCmd.AddCommand(adaptCmd)
}

func runAdapt(cmd *cobra.Command, _ []string) error {
	if adaptList {
		return runAdaptList()
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	agentsPath := filepath.Join(cwd, "AGENTS.md")
	agentsContent, err := os.ReadFile(agentsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("AGENTS.md not found — run 'gg init' first to create it")
		}
		return fmt.Errorf("read AGENTS.md: %w", err)
	}

	if adaptCheck {
		return runAdaptCheck(cwd, string(agentsContent))
	}

	var targets []adapters.Adapter
	switch {
	case adaptAgent != "":
		a := adapters.ByName(adaptAgent)
		if a == nil {
			return fmt.Errorf("unknown agent %q — run 'gg adapt --list' to see supported agents", adaptAgent)
		}
		targets = []adapters.Adapter{*a}
	case adaptAll:
		targets = adapters.Detect(cwd)
		if len(targets) == 0 {
			fmt.Println("No agent config files detected in this project.")
			fmt.Println("Use --agent <name> to inject for a specific agent.")
			return nil
		}
	default:
		return fmt.Errorf("specify --agent <name>, --all, --list, or --check")
	}

	issues := 0
	for _, a := range targets {
		targetPath := a.ResolveTarget(cwd)
		if targetPath == "" {
			fmt.Fprintf(os.Stderr, "  %s  skipped (no target path)\n", a.Name)
			continue
		}

		result, injectErr := adapters.Inject(&a, targetPath, string(agentsContent))
		if injectErr != nil {
			fmt.Fprintf(os.Stderr, "  %s  error: %v\n", a.Name, injectErr)
			issues++
			continue
		}

		rel, _ := filepath.Rel(cwd, result.Path)
		switch {
		case result.Created:
			fmt.Printf("  ✓ %-16s created %s\n", a.Name, rel)
		case result.Updated:
			fmt.Printf("  ✓ %-16s updated %s\n", a.Name, rel)
		default:
			fmt.Printf("  = %-16s up-to-date %s\n", a.Name, rel)
		}
	}

	if issues > 0 {
		return fmt.Errorf("%d agent(s) failed to inject", issues)
	}
	return nil
}

func runAdaptList() error {
	all := adapters.All()
	fmt.Printf("%-16s  %-40s  %s\n", "AGENT", "TARGET FILE(S)", "FORMAT")
	fmt.Println(strings.Repeat("─", 72))
	for _, a := range all {
		fmt.Printf("%-16s  %-40s  %s\n",
			a.Name,
			strings.Join(a.Targets, ", "),
			formatName(a.Format),
		)
	}
	return nil
}

func runAdaptCheck(cwd, agentsContent string) error {
	all := adapters.All()
	issues := 0
	for _, a := range all {
		targetPath := a.ResolveTarget(cwd)
		if targetPath == "" {
			continue
		}
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			continue // file not present — not adapted, not our concern
		}

		upToDate, drift, err := adapters.Check(&a, targetPath, agentsContent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s  check error: %v\n", a.Name, err)
			issues++
			continue
		}
		rel, _ := filepath.Rel(cwd, targetPath)
		if upToDate {
			fmt.Printf("  ✓ %-16s %s (up-to-date)\n", a.Name, rel)
		} else {
			fmt.Printf("  ✗ %-16s %s — %s\n", a.Name, rel, drift)
			issues++
		}
	}
	if issues > 0 {
		return fmt.Errorf("%d file(s) have drifted — run 'gg adapt --all' to sync", issues)
	}
	if issues == 0 {
		fmt.Println("All adapted files are up-to-date.")
	}
	return nil
}

func formatName(f adapters.Format) string {
	switch f {
	case adapters.FullReplace:
		return "full-replace"
	case adapters.ManagedBlock:
		return "managed-block"
	case adapters.Symlink:
		return "symlink"
	default:
		return "unknown"
	}
}
