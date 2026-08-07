package telemetry

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"
)

// WeeklySummary reads the telemetry log and returns a breakdown of verb calls
// in the last 7 days.
type WeeklySummary struct {
	Total      int            `json:"total"`
	AgentCalls int            `json:"agent_calls"`
	HumanCalls int            `json:"human_calls"`
	VerbCounts map[string]int `json:"verb_counts"`
	// CompactCalls counts entries with Compact=true. The two byte totals are
	// summed across those entries; subtracting gives total bytes saved.
	CompactCalls        int `json:"compact_calls"`
	CompactBytesOut     int `json:"compact_bytes_out"`
	CompactBytesDefault int `json:"compact_bytes_default"`
	// CompactTokensSaved is a rough estimate (bytes/4) so agents and humans
	// can read the dogfood benefit in the unit they actually care about.
	// Computed at summarize time — not stored per-entry.
	CompactTokensSaved int `json:"compact_tokens_saved"`
	// WithContextCalls counts gg get --with-context invocations.
	WithContextCalls      int `json:"with_context_calls"`
	WithContextBytesTotal int `json:"with_context_bytes_total"`
	// TaskStartContextCalls counts the memory packets pushed by `gg task start`
	// (TASK-538). Kept separate from WithContextCalls because the two answer
	// different questions: WithContext measures what agents chose to pull,
	// TaskStartContext measures what gg pushed at claim time whether they asked
	// or not. Collapsing them would hide exactly the adoption gap that motivated
	// the push.
	TaskStartContextCalls      int `json:"task_start_context_calls"`
	TaskStartContextBytesTotal int `json:"task_start_context_bytes_total"`
	// Hydration re-fetch aggregates (TASK-279). HydrationCalls counts full-record
	// fetches that follow a compact display. HydrationBytesTotal is the sum of
	// full-render sizes fetched back. NetSavingsBytes and NetTokensSaved subtract
	// the re-fetched bytes from the gross compact savings — a negative net means
	// compaction induced more fetching than it saved.
	// NOTE: aggregate approach — no per-ID causation tracking (ring-buffer
	// deferred). Any full-record fetch counts as hydration regardless of
	// whether a compact call preceded it for that specific ID.
	HydrationCalls      int            `json:"hydration_calls"`
	HydrationBytesTotal int            `json:"hydration_bytes_total"`
	HydrationVerbCounts map[string]int `json:"hydration_verb_counts"`
	NetSavingsBytes     int            `json:"net_savings_bytes"`
	NetTokensSaved      int            `json:"net_tokens_saved"`
	// Honest re-fetch split (TASK-491). The total HydrationCalls above conflates
	// three classes: human full-reads, gate-MANDATED agent --full reads (forced
	// by the hydration/triage gates, often the FIRST read of a record), and
	// DISCRETIONARY agent re-fetches (the only class that signals a compact
	// drop-list dropped a field the agent needed). The "drop-list agresif"
	// heuristic must fire ONLY on the discretionary-agent rate.
	//   AgentHydrationCalls          = agent-origin hydration entries (excludes humans)
	//   AgentMandatedHydrationCalls  = agent-origin AND Mandated=true (gate-forced)
	//   AgentDiscretionaryHydration  = agent-origin AND Mandated=false (the real signal)
	//   MandatedHydrationBytesTotal  = bytes fetched by mandated agent reads (for net-savings note)
	// All recomputed from Origin/Mandated on every Entry, so old telemetry.jsonl
	// (no mandated field → false → counted as discretionary) stays safe/additive.
	AgentHydrationCalls         int `json:"agent_hydration_calls"`
	AgentMandatedHydrationCalls int `json:"agent_mandated_hydration_calls"`
	AgentDiscretionaryHydration int `json:"agent_discretionary_hydration_calls"`
	MandatedHydrationBytesTotal int `json:"mandated_hydration_bytes_total"`
	// Dupe-check aggregates (TASK-268). Helps answer: "how often do agents
	// file anyway after seeing a dup warning?" High force/cancel ratios
	// argue the threshold is off; zero MatchesHits with non-zero Calls
	// means the check is running but the threshold is too tight.
	DupeCheckCalls       int `json:"dupe_check_calls"`
	DupeCheckMatchesHits int `json:"dupe_check_matches_hits"` // calls where matches_count > 0
	DupeChoiceReuse      int `json:"dupe_choice_reuse"`
	DupeChoiceForce      int `json:"dupe_choice_force"`
	DupeChoiceCancel     int `json:"dupe_choice_cancel"`
	DupeChoiceAutoForce  int `json:"dupe_choice_auto_force"`
	// MissingHandlerCalls counts entries where compact was active but the
	// command had no compact render path (TASK-283). MissingHandlerVerbCounts
	// breaks these down by verb so maintainers can identify which commands
	// need compact render paths added.
	MissingHandlerCalls      int            `json:"missing_handler_calls"`
	MissingHandlerVerbCounts map[string]int `json:"missing_handler_verb_counts"`
	// GlyphByteOverhead is the total extra bytes that Unicode glyphs in compact
	// output cost vs equivalent 1-byte ASCII placeholders, summed across all
	// compact calls in the window. GlyphTokenOverhead converts that to tokens
	// (÷4). These fields let the `gg status` display answer "how much of the
	// compact token budget is spent on glyph decoration?"
	GlyphByteOverhead  int `json:"glyph_byte_overhead"`
	GlyphTokenOverhead int `json:"glyph_token_overhead"`
	// CalibrationFactor is the bytes-per-token ratio measured from the canonical
	// compact corpus (internal/telemetry/testdata/compact_corpus.golden) at
	// summarize time. It is informational only — CompactTokensSaved continues to
	// use the hardcoded BytesPerToken constant (currently 3) so the historical
	// series stays comparable. Callers can compare CalibrationFactor against
	// BytesPerToken to decide whether recalibration is warranted.
	CalibrationFactor int `json:"calibration_factor"`
	// Per-verb compact aggregates (TASK-337). Used to compute missed-savings
	// breakdowns: a verb with high VerbCounts but low CompactByVerbCalls is a
	// place where agents kept paying default-render cost when a compact path
	// existed.
	CompactByVerbCalls      map[string]int `json:"compact_by_verb_calls"`
	CompactByVerbBytesSaved map[string]int `json:"compact_by_verb_bytes_saved"`
	// AgentVerbCounts counts per-verb calls from agent-origin entries only.
	// MissedCompactByVerb uses this so that human full-reads are not counted as
	// missed compact opportunities (TASK-490). Additive field — recomputed from
	// the existing Origin field on every Entry, so old telemetry.jsonl is safe.
	AgentVerbCounts map[string]int `json:"agent_verb_counts"`
}

// MissedCompactRow is one row in the missed-savings breakdown — a verb that has
// a compact render path (proven by ≥1 compact call) but where many invocations
// still ran default. EstimatedBytesMissed is conservative: it multiplies missed
// calls by the verb's own observed avg-bytes-saved-per-compact-call, so it never
// extrapolates beyond what the verb itself has demonstrated.
type MissedCompactRow struct {
	Verb                 string `json:"verb"`
	TotalCalls           int    `json:"total_calls"`
	CompactCalls         int    `json:"compact_calls"`
	MissedCalls          int    `json:"missed_calls"`
	AvgBytesSavedPerCall int    `json:"avg_bytes_saved_per_call"`
	EstimatedBytesMissed int    `json:"estimated_bytes_missed"`
}

// MissedCompactByVerb returns missed-savings rows sorted by EstimatedBytesMissed
// descending. Only verbs with ≥1 compact call are included (otherwise we can't
// estimate per-call savings without extrapolating across verbs). Missed calls are
// counted against agent-origin calls only — human full-reads are not missed
// opportunities (TASK-490). limit caps the number of returned rows; a
// non-positive limit returns all rows. Returns an empty slice when no verb has
// compact data — callers should treat this as "nothing to show", not an error.
func (s *WeeklySummary) MissedCompactByVerb(limit int) []MissedCompactRow {
	if s == nil || len(s.CompactByVerbCalls) == 0 {
		return nil
	}
	rows := make([]MissedCompactRow, 0, len(s.CompactByVerbCalls))
	for verb, compactCalls := range s.CompactByVerbCalls {
		if compactCalls <= 0 {
			continue
		}
		// Use agent-origin totals only: human full-reads are not "missed"
		// compact calls. CompactByVerbCalls counts all compact renders
		// (agent + human), so clamp against negative.
		total := s.AgentVerbCounts[verb]
		missed := total - compactCalls
		if missed <= 0 {
			continue
		}
		avg := s.CompactByVerbBytesSaved[verb] / compactCalls
		if avg <= 0 {
			continue
		}
		rows = append(rows, MissedCompactRow{
			Verb:                 verb,
			TotalCalls:           total,
			CompactCalls:         compactCalls,
			MissedCalls:          missed,
			AvgBytesSavedPerCall: avg,
			EstimatedBytesMissed: missed * avg,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].EstimatedBytesMissed != rows[j].EstimatedBytesMissed {
			return rows[i].EstimatedBytesMissed > rows[j].EstimatedBytesMissed
		}
		return rows[i].Verb < rows[j].Verb
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

// Summarize reads the telemetry file and aggregates the last 7 days of data.
// Returns an empty summary (not an error) when the file doesn't exist.
func Summarize(runtimeDir string) (*WeeklySummary, error) {
	return SummarizeFrom(runtimeDir, time.Now().UTC().AddDate(0, 0, -7))
}

// SummarizeFrom reads the telemetry file and aggregates all entries at or
// after since. Returns an empty summary (not an error) when the file doesn't
// exist. Useful for per-session gap detection where the cutoff is the
// session's first write timestamp rather than a fixed 7-day window.
func SummarizeFrom(runtimeDir string, since time.Time) (*WeeklySummary, error) {
	data, err := os.ReadFile(filePath(runtimeDir))
	if os.IsNotExist(err) {
		return &WeeklySummary{
			VerbCounts:               map[string]int{},
			HydrationVerbCounts:      map[string]int{},
			MissingHandlerVerbCounts: map[string]int{},
			CompactByVerbCalls:       map[string]int{},
			CompactByVerbBytesSaved:  map[string]int{},
			AgentVerbCounts:          map[string]int{},
		}, nil
	}
	if err != nil {
		return nil, err
	}

	sum := &WeeklySummary{
		VerbCounts:               map[string]int{},
		HydrationVerbCounts:      map[string]int{},
		MissingHandlerVerbCounts: map[string]int{},
		CompactByVerbCalls:       map[string]int{},
		CompactByVerbBytesSaved:  map[string]int{},
		AgentVerbCounts:          map[string]int{},
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.Timestamp)
		if err != nil || ts.Before(since) {
			continue
		}
		sum.Total++
		sum.VerbCounts[e.Verb]++
		if e.Origin == originAgent {
			sum.AgentCalls++
			sum.AgentVerbCounts[e.Verb]++
		} else {
			sum.HumanCalls++
		}
		if e.Compact {
			sum.CompactCalls++
			sum.CompactBytesOut += e.BytesOut
			sum.CompactBytesDefault += e.BytesDefault
			sum.GlyphByteOverhead += e.GlyphOverheadBytes
			sum.CompactByVerbCalls[e.Verb]++
			if savedV := e.BytesDefault - e.BytesOut; savedV > 0 {
				sum.CompactByVerbBytesSaved[e.Verb] += savedV
			}
		}
		if e.Hydration {
			sum.HydrationCalls++
			sum.HydrationBytesTotal += e.BytesHydrated
			sum.HydrationVerbCounts[e.Verb]++
			// Agent-origin split for the honest drop-list-risk signal (TASK-491).
			// Human full-reads never feed the warning (mirror TASK-490); among
			// agent reads, gate-mandated --full reads are separated from the
			// discretionary re-fetches that are the only true compact-drop signal.
			if e.Origin == originAgent {
				sum.AgentHydrationCalls++
				if e.Mandated {
					sum.AgentMandatedHydrationCalls++
					sum.MandatedHydrationBytesTotal += e.BytesHydrated
				} else {
					sum.AgentDiscretionaryHydration++
				}
			}
		}
		if e.WithContext {
			if e.Verb == VerbTaskStartContext {
				sum.TaskStartContextCalls++
				sum.TaskStartContextBytesTotal += e.ContextBlockBytes
			} else {
				sum.WithContextCalls++
				sum.WithContextBytesTotal += e.ContextBlockBytes
			}
		}
		if e.DupeCheck {
			sum.DupeCheckCalls++
			if e.MatchesCount > 0 {
				sum.DupeCheckMatchesHits++
			}
			switch e.UserChoice {
			case "reuse":
				sum.DupeChoiceReuse++
			case "force":
				sum.DupeChoiceForce++
			case "cancel":
				sum.DupeChoiceCancel++
			case "auto-force":
				sum.DupeChoiceAutoForce++
			}
		}
		if e.MissingHandler {
			sum.MissingHandlerCalls++
			sum.MissingHandlerVerbCounts[e.Verb]++
		}
	}
	if saved := sum.CompactBytesDefault - sum.CompactBytesOut; saved > 0 {
		sum.CompactTokensSaved = saved / BytesPerToken
	}
	sum.CalibrationFactor = CorpusCalibration.Rounded
	// GlyphTokenOverhead: extra tokens spent on Unicode glyphs vs 1-byte ASCII.
	// Uses the same BytesPerToken estimate. Even at zero calls this stays zero,
	// so the gg status display can gate on CompactCalls > 0 before showing it.
	sum.GlyphTokenOverhead = sum.GlyphByteOverhead / BytesPerToken
	// Net savings = gross bytes saved by compact - bytes fetched back by hydration.
	// Can be negative when compact induces more re-fetching than it saves.
	//
	// TASK-491 rationale (AC-5): NetSavingsBytes intentionally still subtracts
	// the FULL HydrationBytesTotal (mandated + discretionary) so the historical
	// net series stays comparable across the change — we do NOT silently shrink
	// the charge-back. Instead the "drop-list agresif" WARNING no longer derives
	// from net at all (see hydrationRiskSuffix / cmd/telemetry.go): it fires only
	// on the discretionary-agent rate. MandatedHydrationBytesTotal is exposed so
	// readers can see how much of the charge-back is gate-mandated first-read
	// bytes (which overstate the "compact induced this fetch" claim) without us
	// rewriting the long-running net metric.
	sum.NetSavingsBytes = (sum.CompactBytesDefault - sum.CompactBytesOut) - sum.HydrationBytesTotal
	sum.NetTokensSaved = sum.NetSavingsBytes / BytesPerToken
	return sum, nil
}
