package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/config"
)

// runDoctorCheckContract checks the managed contract block in each agent's
// entry-point file and reports drift. With fix=true it repairs STALE and
// MISSING entries. DRIFTED entries require forceReset=true to overwrite.
func runDoctorCheckContract(fix, forceReset bool) error {
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	checks := agenthooks.CheckContract(projectRoot)

	if fix {
		cleanupLines, cleanupErrs := agenthooks.RemoveObsoleteBlocks(projectRoot)
		for _, l := range cleanupLines {
			fmt.Println(l)
		}
		if len(cleanupErrs) > 0 {
			return cleanupErrs[0]
		}

		hookLines, hookErrs := agenthooks.RemoveObsoleteHooks(projectRoot)
		for _, l := range hookLines {
			fmt.Println(l)
		}
		// Deliberately NOT fatal, unlike the obsolete-block cleanup above. The
		// difference is target vs bystander, not likelihood: RemoveObsoleteBlocks
		// edits the very markdown files FixContract is about to rewrite, so a
		// failure there leaves the write target in an unknown state and refusing
		// to continue is defensible. RemoveObsoleteHooks edits
		// .claude/settings.json, which FixContract never touches — a bystander.
		// Its errors fire on any JSON syntax error in a file users hand-edit
		// constantly (a trailing comma in permissions.allow is enough), and this
		// command's job — the very instruction printed on contract drift — is
		// repairing the CONTRACT. Aborting that because an unrelated file will
		// not parse strands the drift with no way to fix it, and `gg system sync`
		// reads the non-zero exit as "skip this project's remaining stages".
		// Report loudly, then carry on.
		for _, e := range hookErrs {
			fmt.Fprintf(os.Stderr, "  ✗ obsolete-hook cleanup skipped: %v\n", e)
		}

		lines, fixErr := agenthooks.FixContract(projectRoot, forceReset)
		for _, l := range lines {
			fmt.Println(l)
		}
		if fixErr != nil {
			return fixErr
		}
		checks = agenthooks.CheckContract(projectRoot)
	}

	allOK := true
	fmt.Printf("Contract check  (version %s)\n", agenthooks.ContractVersion()[:12])
	fmt.Println(strings.Repeat("-", 50))
	for _, r := range checks {
		marker := "✓"
		if r.Status != agenthooks.ContractOK {
			marker = "✗"
			allOK = false
		}
		shortPath := r.Path
		if rel, relErr := filepath.Rel(projectRoot, r.Path); relErr == nil {
			shortPath = rel
		}
		fmt.Printf("  %s  %-8s  %-10s  %s\n", marker, r.Status, r.AgentName, shortPath)
	}

	if !allOK {
		return fmt.Errorf("contract drift detected — run `gg doctor --check-contract --fix` to repair")
	}
	return nil
}
