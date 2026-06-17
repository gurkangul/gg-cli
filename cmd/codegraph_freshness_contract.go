package cmd

import (
	"fmt"
	"strings"
)

const (
	codeGraphFreshnessReady         = "ready"
	codeGraphFreshnessMissing       = "missing"
	codeGraphFreshnessStale         = "stale"
	codeGraphFreshnessUnavailable   = "unavailable"
	codeGraphFreshnessUnknown       = "unknown"
	codeGraphFreshnessNotApplicable = "not_applicable"

	codeGraphReasonMissingGraph          = "missing_graph"
	codeGraphReasonEmptyGraph            = "empty_graph"
	codeGraphReasonGraphUnavailable      = "graph_unavailable"
	codeGraphReasonLanguageMissing       = "language_missing"
	codeGraphReasonNonAncestor           = "non_ancestor"
	codeGraphReasonChangedFiles          = "changed_files"
	codeGraphReasonModuleManifestChanged = "module_manifest_changed"
	codeGraphReasonFingerprintMismatch   = "fingerprint_mismatch"
	codeGraphReasonNotApplicable         = "not_applicable"
	codeGraphReasonUnknown               = "unknown"

	codeGraphRepairCommand = "gg doctor --fix-index"
)

type codeGraphFreshness struct {
	Status                   string   `json:"status"`
	Reason                   string   `json:"reason"`
	DetectedLanguages        []string `json:"detected_languages,omitempty"`
	IndexedLanguages         []string `json:"indexed_languages,omitempty"`
	ChangedFiles             int      `json:"changed_files"`
	NewFiles                 int      `json:"new_files"`
	DeletedFiles             int      `json:"deleted_files"`
	ModuleFiles              int      `json:"module_files"`
	SuggestedCommand         string   `json:"suggested_command,omitempty"`
	BackgroundRefresh        bool     `json:"background_refresh"`
	ForegroundWatchAvailable bool     `json:"foreground_watch_available"`
	ForegroundWatchCommand   string   `json:"foreground_watch_command,omitempty"`
}

func (f codeGraphFreshness) NeedsNotice() bool {
	switch f.Status {
	case codeGraphFreshnessMissing, codeGraphFreshnessStale, codeGraphFreshnessUnavailable, codeGraphFreshnessUnknown:
		return true
	default:
		return false
	}
}

func (s *codeGraphStatus) refreshFreshnessContract() {
	if s == nil {
		return
	}
	s.CodeGraph = s.freshnessContract()
}

func (s codeGraphStatus) freshnessContract() codeGraphFreshness {
	status := codeGraphContractStatus(s)
	reason := codeGraphContractReason(s, status)
	langs := codeGraphContractLanguages(s)
	watchAvailable := codeGraphWatchApplicable(status, langs)
	f := codeGraphFreshness{
		Status:                   status,
		Reason:                   reason,
		DetectedLanguages:        append([]string(nil), s.DetectedLanguages...),
		IndexedLanguages:         append([]string(nil), s.IndexedLanguages...),
		ChangedFiles:             s.ChangedFiles,
		NewFiles:                 s.NewFiles,
		DeletedFiles:             s.DeletedFiles,
		ModuleFiles:              s.ModuleFiles,
		BackgroundRefresh:        false,
		ForegroundWatchAvailable: watchAvailable,
	}
	if codeGraphRepairApplicable(status, langs, s.SuggestedCommand) {
		f.SuggestedCommand = codeGraphRepairCommand
	}
	if watchAvailable {
		f.ForegroundWatchCommand = codeGraphWatchCommand(langs)
	}
	return f
}

func codeGraphContractStatus(s codeGraphStatus) string {
	if s.Status == "not_applicable" {
		return codeGraphFreshnessNotApplicable
	}
	if codeGraphMemgraphUnavailable(s) {
		return codeGraphFreshnessUnavailable
	}
	switch s.Status {
	case "ready":
		return codeGraphFreshnessReady
	case "missing":
		return codeGraphFreshnessMissing
	case "stale", "partial", "non_ancestor":
		return codeGraphFreshnessStale
	case "unknown":
		return codeGraphFreshnessUnknown
	default:
		if s.Status == "" {
			return codeGraphFreshnessUnknown
		}
		return s.Status
	}
}

func codeGraphContractReason(s codeGraphStatus, status string) string {
	if status == codeGraphFreshnessNotApplicable {
		return codeGraphReasonNotApplicable
	}
	if codeGraphMemgraphUnavailable(s) {
		return codeGraphReasonGraphUnavailable
	}
	if status == codeGraphFreshnessReady {
		return ""
	}
	if s.GraphEmpty {
		return codeGraphReasonEmptyGraph
	}
	if s.Status == "non_ancestor" {
		return codeGraphReasonNonAncestor
	}
	detail := strings.ToLower(s.Detail)
	if strings.Contains(detail, "unindexed language") {
		return codeGraphReasonLanguageMissing
	}
	if s.ModuleFiles > 0 {
		return codeGraphReasonModuleManifestChanged
	}
	if s.ChangedFiles+s.NewFiles+s.DeletedFiles > 0 {
		return codeGraphReasonChangedFiles
	}
	if strings.Contains(detail, "fingerprint") || strings.Contains(detail, "dirty indexed content") {
		return codeGraphReasonFingerprintMismatch
	}
	if status == codeGraphFreshnessMissing {
		return codeGraphReasonMissingGraph
	}
	return codeGraphReasonUnknown
}

func codeGraphMemgraphUnavailable(s codeGraphStatus) bool {
	if s.Status == "not_applicable" {
		return false
	}
	detail := strings.ToLower(s.GraphDetail)
	return strings.Contains(detail, "unavailable") || (s.GraphConfigured && !s.GraphAvailable && s.GraphDetail != "not checked")
}

func codeGraphContractLanguages(s codeGraphStatus) []string {
	if len(s.DetectedLanguages) > 0 {
		return append([]string(nil), s.DetectedLanguages...)
	}
	if len(s.IndexedLanguages) > 0 {
		return append([]string(nil), s.IndexedLanguages...)
	}
	return nil
}

func codeGraphRepairApplicable(status string, langs []string, legacySuggested string) bool {
	switch status {
	case codeGraphFreshnessMissing, codeGraphFreshnessStale, codeGraphFreshnessUnavailable:
		return len(langs) > 0 || legacySuggested != ""
	default:
		return false
	}
}

func codeGraphWatchApplicable(status string, langs []string) bool {
	if status == codeGraphFreshnessNotApplicable {
		return false
	}
	return len(langs) > 0
}

func codeGraphWatchCommand(langs []string) string {
	if len(langs) == 1 {
		return "gg index --watch --lang " + langs[0]
	}
	return "gg index --watch"
}

func codeGraphNoticeOneLine(s codeGraphStatus) string {
	f := s.freshnessContract()
	if !f.NeedsNotice() {
		return ""
	}
	parts := []string{fmt.Sprintf("CodeGraph: %s (%s).", f.Status, f.Reason)}
	if f.SuggestedCommand != "" {
		parts = append(parts, "Run: "+f.SuggestedCommand+".")
	} else if s.Detail != "" {
		parts = append(parts, "Detail: "+s.Detail+".")
	}
	if f.ForegroundWatchAvailable && f.ForegroundWatchCommand != "" {
		parts = append(parts, "Optional active mode: "+f.ForegroundWatchCommand+".")
	}
	parts = append(parts, "No background index daemon.")
	return strings.Join(parts, " ")
}

func codeGraphNoticeLines(s codeGraphStatus) []string {
	f := s.freshnessContract()
	if !f.NeedsNotice() {
		return nil
	}
	lines := []string{fmt.Sprintf("CodeGraph: %s (%s).", f.Status, f.Reason)}
	if s.Detail != "" {
		lines = append(lines, "Detail: "+s.Detail)
	}
	if f.SuggestedCommand != "" {
		lines = append(lines, "Run: "+f.SuggestedCommand)
	}
	if f.ForegroundWatchAvailable && f.ForegroundWatchCommand != "" {
		lines = append(lines, "Optional active mode: "+f.ForegroundWatchCommand)
	}
	lines = append(lines, "Note: gg does not run a background index daemon.")
	return lines
}
