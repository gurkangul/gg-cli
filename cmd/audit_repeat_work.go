package cmd

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

// Default thresholds for `gg audit repeat-work`. Overridden by
// audit.repeat_work.* in .gg/config.yaml. See config.RepeatWorkConfig for
// the rationale behind each value.
const (
	repeatWorkDefaultWindowDays        = 7
	repeatWorkDefaultRecordsThreshold  = 5
	repeatWorkDefaultRecordsWindowDays = 3
	repeatWorkDefaultReopensThreshold  = 2
	repeatWorkDefaultFilesThreshold    = 3
	repeatWorkDefaultTagThreshold      = 5
)

// trivialTags are skipped in the tier-C tag-cluster signal — they're too
// generic to indicate a real hotspot. Refined as dogfood surfaces more.
var trivialTags = map[string]bool{
	"": true, "gg": true, "v0.3": true, "v0.2": true,
	"impl": true, "design": true, "refactor": true, "chore": true,
}

var auditRepeatWorkCmd = &cobra.Command{
	Use:   "repeat-work",
	Short: "Surface multi-iteration patterns that likely indicate a bug loop",
	Long: `Scan the knowledge base for hotspots where the same task, bug, file, or
tag cluster has been touched many times in a short window. Advisory only —
exits 0 even when hotspots are found. The goal is to surface 'we've patched
this N times' before a 9-iteration bug loop becomes obvious.

Signals are grouped into three tiers:

  Tier A (strong): same task with >= RECORDS records in RECORDS_WINDOW days,
                   or a bug reopened >= REOPENS times
  Tier B (medium): same source file appears in >= FILES bug reports within
                   WINDOW days
  Tier C (weak):   same non-trivial tag attached to >= TAG records within
                   WINDOW days

Thresholds are configurable under audit.repeat_work.* in .gg/config.yaml.`,
	Args: cobra.NoArgs,
	RunE: runAuditRepeatWork,
}

var auditRepeatWorkCompact bool

func init() {
	auditRepeatWorkCmd.Flags().BoolVar(&auditRepeatWorkCompact, "compact", false,
		"one line per hotspot — drops tier headers and hint lines to preserve agent context window")
	auditCmd.AddCommand(auditRepeatWorkCmd)
}

// repeatWorkConfig resolves the effective thresholds from config + defaults.
// Zero values fall back to package defaults so a fresh project with no
// audit.repeat_work block behaves sanely.
type repeatWorkConfig struct {
	WindowDays        int
	RecordsThreshold  int
	RecordsWindowDays int
	ReopensThreshold  int
	FilesThreshold    int
	TagThreshold      int
}

func resolveRepeatWorkConfig(cfg *config.Config) repeatWorkConfig {
	c := repeatWorkConfig{
		WindowDays:        repeatWorkDefaultWindowDays,
		RecordsThreshold:  repeatWorkDefaultRecordsThreshold,
		RecordsWindowDays: repeatWorkDefaultRecordsWindowDays,
		ReopensThreshold:  repeatWorkDefaultReopensThreshold,
		FilesThreshold:    repeatWorkDefaultFilesThreshold,
		TagThreshold:      repeatWorkDefaultTagThreshold,
	}
	if cfg == nil {
		return c
	}
	r := cfg.Audit.RepeatWork
	if r.WindowDays > 0 {
		c.WindowDays = r.WindowDays
	}
	if r.RecordsThreshold > 0 {
		c.RecordsThreshold = r.RecordsThreshold
	}
	if r.RecordsWindowDays > 0 {
		c.RecordsWindowDays = r.RecordsWindowDays
	}
	if r.ReopensThreshold > 0 {
		c.ReopensThreshold = r.ReopensThreshold
	}
	if r.FilesThreshold > 0 {
		c.FilesThreshold = r.FilesThreshold
	}
	if r.TagThreshold > 0 {
		c.TagThreshold = r.TagThreshold
	}
	return c
}

// repeatWorkHotspot is one line in the report.
type repeatWorkHotspot struct {
	Tier   string `json:"tier"`   // "A" | "B" | "C"
	Kind   string `json:"kind"`   // "task" | "bug" | "file" | "tag"
	Key    string `json:"key"`    // task ID, bug ID, file path, or tag name
	Count  int    `json:"count"`  // records / reopens / bugs / occurrences
	Detail string `json:"detail"` // rendered hint line ("9 records, spans 3d")
}

// repeatWorkReport is the JSON payload emitted with --json.
type repeatWorkReport struct {
	TierA      []repeatWorkHotspot `json:"tier_a"`
	TierB      []repeatWorkHotspot `json:"tier_b"`
	TierC      []repeatWorkHotspot `json:"tier_c"`
	Thresholds repeatWorkConfig    `json:"thresholds"`
	WindowFrom string              `json:"window_from"` // RFC3339 cutoff timestamp
}

func runAuditRepeatWork(cmd *cobra.Command, _ []string) error {
	cfg, _ := config.Load()
	rwCfg := resolveRepeatWorkConfig(cfg)

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ Qdrant unreachable — repeat-work check skipped (advisory only).")
		return nil
	}

	// Pull all decisions, tasks, and bugs into memory. The window is short
	// (7 days by default) but Qdrant returns unsorted so we filter in code.
	// For projects with >10k decisions this is still <1s.
	decisions, err := d.store.ListDecisions(ctx, 0, true)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}
	bugs, err := d.store.ListBugs(ctx, "")
	if err != nil {
		return fmt.Errorf("list bugs: %w", err)
	}

	cutoff := time.Now().UTC().Add(-time.Duration(rwCfg.WindowDays) * 24 * time.Hour)
	innerCutoff := time.Now().UTC().Add(-time.Duration(rwCfg.RecordsWindowDays) * 24 * time.Hour)

	report := repeatWorkReport{
		Thresholds: rwCfg,
		WindowFrom: cutoff.Format(time.RFC3339),
	}
	report.TierA = collectTierA(decisions, bugs, innerCutoff, rwCfg)
	report.TierB = collectTierB(bugs, cutoff, rwCfg)
	report.TierC = collectTierC(decisions, cutoff, rwCfg)

	return printJSON(report, func() {
		if isCompactActive(cmd) {
			emitCompact(cmd, "repeat-work",
				func(w io.Writer) { renderRepeatWorkDefault(w, report, rwCfg) },
				func(w io.Writer) { renderRepeatWorkCompact(w, report) },
				compactRendererV_repeatWork,
			)
			return
		}
		renderRepeatWorkDefault(os.Stdout, report, rwCfg)
	})
}

// ── Signal collection ────────────────────────────────────────────────────────

// collectTierA finds (a) tasks with >= RecordsThreshold decisions in the
// inner window, and (b) bugs with >= ReopensThreshold reopens.
func collectTierA(decisions []store.Decision, bugs []store.Bug, innerCutoff time.Time, cfg repeatWorkConfig) []repeatWorkHotspot {
	// (a) records-per-task
	perTask := map[string]int{}
	perTaskSpan := map[string]time.Duration{}
	perTaskFirst := map[string]time.Time{}
	perTaskLast := map[string]time.Time{}
	for _, d := range decisions {
		if d.TaskID == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, d.CreatedAt)
		if err != nil || ts.Before(innerCutoff) {
			continue
		}
		perTask[d.TaskID]++
		if first, ok := perTaskFirst[d.TaskID]; !ok || ts.Before(first) {
			perTaskFirst[d.TaskID] = ts
		}
		if last, ok := perTaskLast[d.TaskID]; !ok || ts.After(last) {
			perTaskLast[d.TaskID] = ts
		}
		perTaskSpan[d.TaskID] = perTaskLast[d.TaskID].Sub(perTaskFirst[d.TaskID])
	}
	var out []repeatWorkHotspot
	for taskID, n := range perTask {
		if n < cfg.RecordsThreshold {
			continue
		}
		span := perTaskSpan[taskID]
		out = append(out, repeatWorkHotspot{
			Tier:  "A",
			Kind:  "task",
			Key:   taskID,
			Count: n,
			Detail: fmt.Sprintf("%d records, spans %s (consider re-architect)",
				n, humaniseSpan(span)),
		})
	}

	// (b) reopened bugs
	for _, b := range bugs {
		if b.ReopenCount < cfg.ReopensThreshold {
			continue
		}
		out = append(out, repeatWorkHotspot{
			Tier:   "A",
			Kind:   "bug",
			Key:    b.ID,
			Count:  b.ReopenCount,
			Detail: fmt.Sprintf("reopened %dx — fix isn't sticking", b.ReopenCount),
		})
	}

	// Stable sort: highest count first, then key asc for determinism.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// collectTierB finds source files appearing in >= FilesThreshold bugs
// within the outer window (using CreatedAt, or UpdatedAt if present).
func collectTierB(bugs []store.Bug, cutoff time.Time, cfg repeatWorkConfig) []repeatWorkHotspot {
	perFile := map[string]map[string]bool{} // file → set of bug IDs (dedup)
	for _, b := range bugs {
		ts, err := time.Parse(time.RFC3339, b.UpdatedAt)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, b.CreatedAt)
			if err != nil {
				continue
			}
		}
		if ts.Before(cutoff) {
			continue
		}
		for _, f := range b.AffectedFiles {
			if f == "" {
				continue
			}
			if perFile[f] == nil {
				perFile[f] = map[string]bool{}
			}
			perFile[f][b.ID] = true
		}
	}
	var out []repeatWorkHotspot
	for file, bugSet := range perFile {
		n := len(bugSet)
		if n < cfg.FilesThreshold {
			continue
		}
		out = append(out, repeatWorkHotspot{
			Tier:   "B",
			Kind:   "file",
			Key:    file,
			Count:  n,
			Detail: fmt.Sprintf("touched by %d bug fixes — centralise or rewrite", n),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// collectTierC finds non-trivial tags appearing on >= TagThreshold decisions
// within the outer window.
func collectTierC(decisions []store.Decision, cutoff time.Time, cfg repeatWorkConfig) []repeatWorkHotspot {
	perTag := map[string]int{}
	for _, d := range decisions {
		ts, err := time.Parse(time.RFC3339, d.CreatedAt)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		for _, t := range d.Tags {
			tag := strings.ToLower(strings.TrimSpace(t))
			if trivialTags[tag] {
				continue
			}
			perTag[tag]++
		}
	}
	var out []repeatWorkHotspot
	for tag, n := range perTag {
		if n < cfg.TagThreshold {
			continue
		}
		out = append(out, repeatWorkHotspot{
			Tier:   "C",
			Kind:   "tag",
			Key:    tag,
			Count:  n,
			Detail: fmt.Sprintf("%d records share this tag — a cluster is forming", n),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// ── Rendering ────────────────────────────────────────────────────────────────

func renderRepeatWorkDefault(w io.Writer, r repeatWorkReport, cfg repeatWorkConfig) {
	total := len(r.TierA) + len(r.TierB) + len(r.TierC)
	if total == 0 {
		fmt.Fprintf(w, "No repeat-work hotspots in the last %dd (thresholds: records=%d/%dd, reopens=%d, files=%d, tag=%d).\n",
			cfg.WindowDays, cfg.RecordsThreshold, cfg.RecordsWindowDays,
			cfg.ReopensThreshold, cfg.FilesThreshold, cfg.TagThreshold)
		return
	}
	fmt.Fprintf(w, "⚠ REPEAT-WORK HOTSPOTS (last %dd):\n\n", cfg.WindowDays)
	if len(r.TierA) > 0 {
		fmt.Fprintln(w, "Tier A (strong — act on these):")
		for _, h := range r.TierA {
			fmt.Fprintf(w, "  %-10s %-12s %s\n", strings.ToUpper(h.Kind), h.Key, h.Detail)
		}
		fmt.Fprintln(w)
	}
	if len(r.TierB) > 0 {
		fmt.Fprintln(w, "Tier B (moderate):")
		for _, h := range r.TierB {
			fmt.Fprintf(w, "  %-10s %-40s %s\n", strings.ToUpper(h.Kind), compactTrim(h.Key, 40), h.Detail)
		}
		fmt.Fprintln(w)
	}
	if len(r.TierC) > 0 {
		fmt.Fprintln(w, "Tier C (weak):")
		for _, h := range r.TierC {
			fmt.Fprintf(w, "  %-10s %-20s %s\n", strings.ToUpper(h.Kind), h.Key, h.Detail)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Hint: tier-A hotspots are where a bug loop forms. Step back before the next patch.")
}

func renderRepeatWorkCompact(w io.Writer, r repeatWorkReport) {
	total := len(r.TierA) + len(r.TierB) + len(r.TierC)
	fmt.Fprintf(w, "repeat-work — %dA %dB %dC\n", len(r.TierA), len(r.TierB), len(r.TierC))
	if total == 0 {
		return
	}
	for _, h := range r.TierA {
		fmt.Fprintf(w, "A  %s  %s  %d  %s\n", h.Kind, h.Key, h.Count, h.Detail)
	}
	for _, h := range r.TierB {
		fmt.Fprintf(w, "B  %s  %s  %d\n", h.Kind, compactTrim(h.Key, compactLineWidth), h.Count)
	}
	for _, h := range r.TierC {
		fmt.Fprintf(w, "C  %s  %s  %d\n", h.Kind, h.Key, h.Count)
	}
}

// humaniseSpan renders a duration like "2.3d" or "5h" — compact enough to
// fit in a one-line hotspot row without losing resolution below a day.
func humaniseSpan(d time.Duration) string {
	if d <= 0 {
		return "<1h"
	}
	if d < time.Hour {
		return "<1h"
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}
