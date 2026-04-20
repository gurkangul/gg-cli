package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/agenthooks"
	"github.com/gurkangul/gg-cli/internal/config"
)

// runDoctorCheckContract checks the managed contract block in each agent's
// entry-point file and reports drift. With fix=true it repairs STALE and MISSING
// entries. DRIFTED entries require forceReset=true to overwrite.
// Returns nil if all agents are OK; returns a non-nil error with a message on drift
// (so the CLI exits non-zero) unless fix resolves everything.
func runDoctorCheckContract(fix, forceReset bool) error {
	projectRoot, err := config.FindRoot()
	if err != nil {
		return err
	}

	checks := agenthooks.CheckContract(projectRoot)

	if fix {
		lines, fixErr := agenthooks.FixContract(projectRoot, forceReset)
		for _, l := range lines {
			fmt.Println(l)
		}
		if fixErr != nil {
			return fixErr
		}
		// Re-check after fix to see if drift remains.
		checks = agenthooks.CheckContract(projectRoot)
	}

	allOK := true
	fmt.Printf("Contract check  (version %s)\n", agenthooks.ContractVersion()[:12])
	fmt.Println(strings.Repeat("─", 50))
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
