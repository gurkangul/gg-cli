package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

const (
	dupThreshold = float32(0.85)
	dupLimit     = uint64(3)
)

// dupResult carries the outcome of a duplicate-check prompt.
type dupResult struct {
	Abort  bool // caller should abort creation
	SawDup bool // at least one candidate was found (used for auto-tagging)
	// MatchesCount is the number of candidates that crossed the threshold.
	// Zero means either the check ran and found nothing OR the check errored
	// out silently. Callers recording telemetry can distinguish the two via
	// the err path inside promptIfDuplicateThreshold (warning is printed).
	MatchesCount int
	// TopScore is the highest cosine returned from the dedup search, or 0
	// when MatchesCount == 0. Surfaced so telemetry can bucket "did the
	// threshold cast a wide or narrow net?" over time.
	TopScore float32
	// UserChoice is one of the telemetry.DupeChoice* constants describing
	// how the prompt resolved. Empty string when no matches surfaced (no
	// prompt fired). Callers pass this to telemetry.RecordDupeCheck.
	UserChoice string
	// ReuseAsBugID is set when the user chose "note" (reuse-as-note flow) and
	// the caller should add a note linked to this existing bug ID instead of
	// creating a new bug.
	ReuseAsBugID string
}

// promptIfDuplicate searches for near-duplicates in the given kind's collection.
// If candidates are found:
//   - In a TTY: prints them and prompts the user to continue or abort.
//   - In non-TTY (agent / pipe): prints a warning to stderr and proceeds.
//
// Returns true if the caller should abort creation.
// Errors from the dedup search are treated as non-fatal: a warning is printed
// and creation continues, so a transient Qdrant glitch never blocks writes.
func promptIfDuplicate(ctx context.Context, d *deps, kind string, vector []float32) (abort bool) {
	return promptIfDuplicateThreshold(ctx, d, kind, vector, dupThreshold).Abort
}

// promptIfDuplicateThreshold is like promptIfDuplicate but uses a caller-supplied
// similarity threshold and returns a dupResult so callers can react to the SawDup
// signal (e.g. auto-append a "dupe-acknowledged" audit tag) and record telemetry.
//
// The dupResult carries enough context for a telemetry.RecordDupeCheck call —
// callers are expected to make that recording (not this function) because
// recording needs verb + fromFlag + runtimeDir from the calling command.
func promptIfDuplicateThreshold(ctx context.Context, d *deps, kind string, vector []float32, threshold float32) dupResult {
	cands, err := d.store.FindNearDups(ctx, kind, vector, threshold, dupLimit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: dedup check failed: %v\n", err)
		return dupResult{}
	}
	if len(cands) == 0 {
		return dupResult{}
	}

	fmt.Fprintf(os.Stderr, "⚠  Similar %s already exist:\n", kind)
	for _, c := range cands {
		label := c.Label
		if len(label) > 72 {
			label = label[:69] + "..."
		}
		fmt.Fprintf(os.Stderr, "   %-12s  %q  (similarity: %.0f%%)\n", c.ID, label, c.Score*100)
	}

	res := dupResult{
		SawDup:       true,
		MatchesCount: len(cands),
		TopScore:     cands[0].Score,
	}

	if !isTerminal(os.Stdin) {
		fmt.Fprintln(os.Stderr, "   (non-interactive — creating anyway)")
		res.UserChoice = "auto-force"
		return res
	}

	// 3-way prompt: force-file / add-note-to-existing / cancel.
	topID := cands[0].ID
	fmt.Fprintf(os.Stderr, "[f]ile anyway / [n]ote on %s / [c]ancel: ", topID)
	line, _ := newStdinReader().ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "f", "force", "y", "yes":
		res.UserChoice = "force"
	case "n", "note":
		res.Abort = true
		res.UserChoice = "reuse"
		res.ReuseAsBugID = topID
	default: // "c", "cancel", "" (Enter)
		res.Abort = true
		res.UserChoice = "cancel"
	}
	return res
}

// isTerminal reports whether f is a character device (i.e. a real terminal).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// newStdinReader returns a buffered reader for os.Stdin.
// Centralised here so dedup and init prompts use the same reader type.
func newStdinReader() *bufio.Reader {
	return bufio.NewReader(os.Stdin)
}

// appendUniq appends tag to tags only if it is not already present.
func appendUniq(tags []string, tag string) []string {
	for _, t := range tags {
		if t == tag {
			return tags
		}
	}
	return append(tags, tag)
}
