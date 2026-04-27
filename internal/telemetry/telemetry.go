// Package telemetry records per-call usage data for gg verbs — strictly
// LOCAL (no network), used by gg's own dogfood metrics (DISC-008).
//
// Each call appends a single JSON line to
// ~/.gg/projects/<project_id>/telemetry.jsonl (the per-project runtime dir).
// The file is append-only and never truncated by gg itself; callers can
// rotate or delete it freely.
//
// Telemetry is ON by default (local-only, no network). Disable entirely with:
// export GG_TELEMETRY=0   (env override always wins)
// or by adding to .gg/config.yaml:  telemetry: {enabled: false}
//
// Origin heuristic: if GG_ROLE is set the call is assumed to come from an
// agent; otherwise it is treated as human-initiated. This is a best-effort
// classification — any agent that runs without GG_ROLE will count as human.
package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	fileName    = "telemetry.jsonl"
	originAgent = "agent"
	originHuman = "human"

	// BytesPerToken is the bytes-per-token divisor used for all token estimates
	// in compact telemetry. Compact output is code-heavy ASCII (IDs, dates,
	// short titles) and mixed English/Turkish prose. Empirical tokenization of
	// representative gg compact lines yields ~3 chars/token, so 3 is more
	// accurate than the generic English-prose value of 4.
	//
	// All callers that display a token count should label it "(est.)" so users
	// understand this is a heuristic, not a precise tiktoken count.
	BytesPerToken = 3
)

// configExplicitDisabled is true when .gg/config.yaml contains
// `telemetry.enabled: false`. It lets users turn telemetry off via config
// without relying on env vars. Default zero value is false → default ON.
var configExplicitDisabled atomic.Bool

// Entry is a single telemetry record.
type Entry struct {
	Verb      string `json:"verb"`
	Origin    string `json:"origin"` // "agent" or "human"
	Timestamp string `json:"ts"`     // RFC3339
	// Compact-mode measurement fields (omitted for non-compact calls).
	// BytesOut is what was actually printed; BytesDefault is what the
	// non-compact renderer would have produced for the same data.
	Compact      bool `json:"compact,omitempty"`
	BytesOut     int  `json:"bytes_out,omitempty"`
	BytesDefault int  `json:"bytes_default,omitempty"`
	// RendererV stamps the compact-renderer version so aggregates across
	// format changes can be bucketed. Zero = pre-v1 (unknown). Callers that
	// use RecordCompact set this via the rendererV arg.
	RendererV int `json:"renderer_v,omitempty"`
	// --with-context fields (omitted when flag is not used).
	WithContext       bool `json:"with_context,omitempty"`
	ContextBlockBytes int  `json:"context_block_bytes,omitempty"`
	// Hydration re-fetch fields (TASK-279). Set by RecordHydration when a
	// caller fetches the full record after the agent saw compact output.
	// BytesHydrated is the full-render byte size of the fetched record.
	// Omitted on non-hydration entries so the JSONL stays clean.
	Hydration      bool `json:"hydration,omitempty"`
	BytesHydrated  int  `json:"bytes_hydrated,omitempty"`
	// Dupe-check fields (TASK-268). Set by RecordDupeCheck when `gg bug
	// report` runs its advisory near-duplicate search. Omitted on all other
	// entries so the JSONL stays clean.
	DupeCheck    bool    `json:"dupe_check,omitempty"`
	MatchesCount int     `json:"matches_count,omitempty"`
	TopScore     float32 `json:"top_score,omitempty"`
	// UserChoice is one of: reuse | force | cancel | auto-force.
	// Lets `gg audit` retroactively measure how often agents override a dup
	// warning vs. heed it — the feedback signal for threshold tuning.
	UserChoice string `json:"user_choice,omitempty"`
	// GlyphOverheadBytes is the extra byte cost that Unicode glyphs in the
	// compact output contribute vs equivalent 1-byte ASCII placeholders — each
	// non-ASCII rune adds (utf8.RuneLen(r) - 1) overhead bytes. Recorded per
	// compact call so the weekly aggregate can answer "how many tokens do our
	// glyphs cost vs a plain-ASCII compact renderer?"
	// Omitted on non-compact entries and when the overhead is zero.
	GlyphOverheadBytes int `json:"glyph_overhead_bytes,omitempty"`
	// MissingHandler is set when compact mode was active but the command fell
	// through to the default (non-compact) renderer — no emitCompact call was
	// made. Verb identifies which command lacked a compact render path.
	// Omitted on all other entries so the JSONL stays clean.
	MissingHandler bool `json:"missing_handler,omitempty"`
	// ActiveWorkers is the number of simultaneously active worker panes at the
	// time of the event. Set by the parallel queue runner (TASK-276). Omitted
	// on non-orchestration entries so the JSONL stays clean.
	ActiveWorkers int `json:"active_workers,omitempty"`
	// SessionID tags the entry with the current agent session identifier.
	// Populated from CLAUDE_SESSION_ID (Claude Code harness) or GG_SESSION_ID
	// (generic agent override), whichever is set first. Empty on human-CLI
	// invocations. Enables session-level compact context aggregation (TASK-286).
	SessionID string `json:"session_id,omitempty"`
}

func filePath(runtimeDir string) string {
	return filepath.Join(runtimeDir, fileName)
}

// SetEnabled applies the explicit config-based telemetry setting. Call this
// at startup ONLY when .gg/config.yaml carries an explicit `telemetry.enabled`
// value — omit the call (or never reach it) when the field is absent, so the
// default ON behaviour kicks in. The GG_TELEMETRY env var always takes
// precedence over this setting.
func SetEnabled(enabled bool) {
	configExplicitDisabled.Store(!enabled)
}

// IsDisabled reports whether telemetry is off. Telemetry is ON by default
// (local-only, no network). Priority order:
//
//  1. GG_TELEMETRY=0/false/no/off → always disabled
//  2. GG_TELEMETRY=1/true/yes/on  → always enabled
//  3. config telemetry.enabled: false (via SetEnabled) → disabled
//  4. otherwise → enabled
func IsDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GG_TELEMETRY"))) {
	case "0", "false", "no", "off":
		return true
	case "1", "true", "yes", "on":
		return false
	}
	return configExplicitDisabled.Load()
}

// Record appends one entry to the telemetry log. Errors are silently ignored
// so a full disk or missing directory never breaks the command being recorded.
// No-op if the user has disabled telemetry via GG_TELEMETRY=0.
//
// Origin classification heuristic (best-effort):
//   - GG_ROLE env set                                → agent
//   - GG_AGENT env set (e.g. "claude-code", "gsd")   → agent
//   - --from flag passed (any non-empty value)       → agent
//   - Otherwise                                       → human
//
// Pass fromFlag = "" if the calling command has no --from flag.
func Record(runtimeDir, verb, fromFlag string) {
	recordEntry(runtimeDir, Entry{
		Verb:      verb,
		Origin:    classify(fromFlag),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// GlyphOverheadBytesOf returns the extra byte cost that Unicode glyphs in s
// contribute vs equivalent 1-byte ASCII placeholders. For every non-ASCII rune
// r the overhead is (utf8.RuneLen(r) - 1): a 3-byte BMP glyph like ✓ costs 2
// extra bytes vs a single ASCII character at the same position.
//
// This is the denominator for the glyph-vs-ASCII token comparison surfaced in
// `gg status`. It runs on the already-rendered compact output, so no
// per-glyph allowlist is needed — any non-ASCII rune is measured directly.
func GlyphOverheadBytesOf(s string) int {
	overhead := 0
	for _, r := range s {
		if r >= utf8.RuneSelf {
			overhead += utf8.RuneLen(r) - 1
		}
	}
	return overhead
}

// RecordCompact appends a telemetry entry with byte-count measurements for a
// --compact invocation. bytesDefault is what the non-compact renderer would
// have produced on the same data — callers must render both to compute the
// baseline. rendererV stamps the compact-output format version so aggregates
// across a format change don't silently mix apples and oranges.
// compactOutput is the rendered compact string used to measure glyph overhead;
// pass "" to skip glyph measurement (treated as zero overhead).
func RecordCompact(runtimeDir, verb, fromFlag string, bytesOut, bytesDefault, rendererV int, compactOutput string) {
	recordEntry(runtimeDir, Entry{
		Verb:               verb,
		Origin:             classify(fromFlag),
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		Compact:            true,
		BytesOut:           bytesOut,
		BytesDefault:       bytesDefault,
		RendererV:          rendererV,
		GlyphOverheadBytes: GlyphOverheadBytesOf(compactOutput),
	})
}

// DupeCheck choice constants — one of these is recorded per RecordDupeCheck
// call so `gg audit` can bucket "did the agent heed or override the warning?"
const (
	DupeChoiceReuse     = "reuse"      // user picked [n] append-note
	DupeChoiceForce     = "force"      // user picked [f] file-anyway
	DupeChoiceCancel    = "cancel"     // user picked [c] abort
	DupeChoiceAutoForce = "auto-force" // non-TTY → auto-proceed
)

// RecordDupeCheck appends a telemetry entry for an advisory duplicate-check
// run in `gg bug report` (TASK-267 / TASK-268). matchesCount is the number of
// candidates that crossed the similarity threshold (0 means the check ran
// but found nothing — still worth recording so threshold tuning has a
// denominator). topScore is the highest cosine returned (0 when no matches).
// userChoice is one of the DupeChoice* constants above.
func RecordDupeCheck(runtimeDir, verb, fromFlag string, matchesCount int, topScore float32, userChoice string) {
	recordEntry(runtimeDir, Entry{
		Verb:         verb,
		Origin:       classify(fromFlag),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		DupeCheck:    true,
		MatchesCount: matchesCount,
		TopScore:     topScore,
		UserChoice:   userChoice,
	})
}

// RecordWithContext appends a telemetry entry for a --with-context invocation.
// contextBlockBytes is the size in bytes of the appended === Related Context === block.
func RecordWithContext(runtimeDir, verb, fromFlag string, contextBlockBytes int) {
	recordEntry(runtimeDir, Entry{
		Verb:              verb,
		Origin:            classify(fromFlag),
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		WithContext:       true,
		ContextBlockBytes: contextBlockBytes,
	})
}

// RecordMissingHandler appends a telemetry entry for a command invocation where
// compact mode was active (via GG_AGENT/GG_ROLE/--compact flag) but the command
// fell through to its default renderer because no compact render path exists.
// This surfaces which verbs still need compact implementations so the gap can be
// prioritised — "compact requested but silently ignored" shows up in
// `gg telemetry summary` as missing_handler_calls by verb.
func RecordMissingHandler(runtimeDir, verb, fromFlag string) {
	recordEntry(runtimeDir, Entry{
		Verb:           verb,
		Origin:         classify(fromFlag),
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		MissingHandler: true,
	})
}

// RecordHydration appends a telemetry entry for a full-record re-fetch that
// follows compact display. bytesHydrated is the full-render size of the fetched
// record — this is charged against gross compact savings to compute net savings.
func RecordHydration(runtimeDir, verb, fromFlag string, bytesHydrated int) {
	recordEntry(runtimeDir, Entry{
		Verb:          verb,
		Origin:        classify(fromFlag),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Hydration:     true,
		BytesHydrated: bytesHydrated,
	})
}

// RecordActiveWorkers appends a telemetry entry capturing the current parallel
// worker count. Called by the queue runner on each dispatch cycle (TASK-276).
func RecordActiveWorkers(runtimeDir, fromFlag string, count int) {
	recordEntry(runtimeDir, Entry{
		Verb:          "queue-dispatch",
		Origin:        classify(fromFlag),
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		ActiveWorkers: count,
	})
}

func classify(fromFlag string) string {
	switch {
	case strings.TrimSpace(os.Getenv("GG_ROLE")) != "":
		return originAgent
	case strings.TrimSpace(os.Getenv("GG_AGENT")) != "":
		return originAgent
	case strings.TrimSpace(fromFlag) != "":
		return originAgent
	}
	return originHuman
}

// sessionID returns the current agent session identifier, checking
// CLAUDE_SESSION_ID first (Claude Code harness), then GG_SESSION_ID
// (generic override). Returns "" when neither is set (human CLI invocations).
func sessionID() string {
	if id := strings.TrimSpace(os.Getenv("CLAUDE_SESSION_ID")); id != "" {
		return id
	}
	return strings.TrimSpace(os.Getenv("GG_SESSION_ID"))
}

func recordEntry(runtimeDir string, e Entry) {
	if runtimeDir == "" || e.Verb == "" || IsDisabled() {
		return
	}
	if e.SessionID == "" {
		e.SessionID = sessionID()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filePath(runtimeDir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(data, '\n'))
}

