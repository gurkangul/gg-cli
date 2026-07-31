package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gurkangul/gg-cli/internal/config"
)

// doctorCheckGateArming reports gates that are installed but cannot block.
//
// A gate has two independent states that look identical from the outside:
// deployed, and actually armed. 60-lint-gate.sh exits 0 with "no baseline —
// skipping gate" when .gg/lint-baseline.json is absent, which is the right
// runtime behaviour (a fresh project must not be blocked by debt it has not
// measured) but leaves a project carrying the hook for months without ever
// blocking a single regression. Nothing reported that: lint-baseline appeared
// only in the install description and the --capture-lint-baseline flag, never
// in a check.
//
// Deliberately NOT symmetric with the file-size baseline. A missing
// .gg/file-size-baseline.json makes 30-file-size.sh stricter, not inert — it
// simply grandfathers nothing — so its absence is not a finding.
func doctorCheckGateArming(report *doctorReport) {
	root, err := config.FindRoot()
	if err != nil {
		return
	}

	gate := filepath.Join(root, config.DirName, "hooks", "pre-task-done.d", "60-lint-gate.sh")
	if _, statErr := os.Stat(gate); statErr != nil {
		return // gate not installed — nothing to arm
	}

	if _, lookErr := exec.LookPath("golangci-lint"); lookErr != nil {
		report.warn("lint gate", "installed but golangci-lint is not on PATH — the gate skips every run "+
			"(install: https://golangci-lint.run/usage/install/)")
		return
	}

	baselinePath := filepath.Join(root, config.DirName, "lint-baseline.json")
	data, readErr := os.ReadFile(baselinePath) //nolint:gosec // path is derived from the project root
	if readErr != nil {
		report.warn("lint gate", "installed but NOT armed — no .gg/lint-baseline.json, so the gate exits 0 on "+
			"every run and no lint regression can block `gg task done`; arm it with `gg doctor --capture-lint-baseline`")
		return
	}

	var baseline struct {
		IssueCount *int `json:"issue_count"`
	}
	if jsonErr := json.Unmarshal(data, &baseline); jsonErr != nil || baseline.IssueCount == nil {
		report.warn("lint gate", "installed but .gg/lint-baseline.json has no readable issue_count — the gate "+
			"skips the comparison; re-arm with `gg doctor --capture-lint-baseline`")
		return
	}

	report.ok("lint gate", fmt.Sprintf("armed (baseline %d issue(s))", *baseline.IssueCount))
}
