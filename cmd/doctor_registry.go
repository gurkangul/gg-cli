package cmd

import (
	"fmt"

	"github.com/gurkangul/gg-cli/internal/config"
)

// doctorCheckRegistry surfaces the health of ~/.gg/projects.json: how many
// registered projects still resolve to a live root, how many are stale
// (missing root dir / .gg/config.yaml), and how many are invalid (config
// present but unreadable). Stale entries get an actionable prune hint pointing
// at the real command (`gg system register --prune`). Advisory only — a stale
// host registry never fails `gg doctor`, so it is a warn at worst.
func doctorCheckRegistry(report *doctorReport) {
	reg, err := config.LoadRegistry()
	if err != nil {
		report.warn("host registry", fmt.Sprintf("load failed: %v", err))
		return
	}
	if len(reg.Projects) == 0 {
		report.ok("host registry", "empty (no projects registered)")
		return
	}
	stats := registryStats(reg)
	detail := fmt.Sprintf("%d project(s): %d ok, %d stale, %d invalid",
		stats.Total, stats.OK, stats.Missing, stats.Invalid)
	switch {
	case stats.Missing > 0:
		report.warn("host registry",
			detail+" — run `gg system register --prune` to remove stale entries")
	case stats.Invalid > 0:
		report.warn("host registry",
			detail+" — inspect invalid entries (unreadable .gg/config.yaml; --prune won't remove them)")
	default:
		report.ok("host registry", detail)
	}
}
