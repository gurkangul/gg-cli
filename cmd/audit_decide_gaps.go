package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// decideKeywords is a conservative set of phrases that indicate an agent
// produced decision-shaped text. The bar is intentionally high to minimise
// false positives — only unambiguous decision statements are flagged.
var decideKeywords = []string{
	"decided to", "we decided", "i decided",
	"going with ", "we'll use ", "we will use ",
	"we chose ", "we rejected", "rejected because",
	"instead of ", "the decision is", "decision:",
	"we agreed to", "agreed on ", "agreed to use",
	"karar verdik", "reddettik", "seçtik",
}

// decideGapCoverageWindow is the tolerance window for correlating a decision
// message with a gg record call. A record created within this duration of the
// message is considered coverage.
const decideGapCoverageWindow = 2 * time.Hour

// flaggedMsg is a message whose content matched the decide-language heuristic
// but has no corresponding gg record within the coverage window.
type flaggedMsg struct {
	fromRole string
	toRole   string
	content  string
	msgTime  time.Time
}

var auditDecideGapsCmd = &cobra.Command{
	Use:   "decide-gaps",
	Short: "Flag gg messages containing decision-language but no corresponding gg record",
	Long: `Scan gg tell messages in the look-back window for decision-shaped text
(e.g. "decided to", "going with", "we chose", "rejected because") and
cross-reference against gg record/decide calls created in the same window.
Messages that look like decisions but have no matching gg record are flagged.

Non-blocking — exits 0 even when gaps are found.`,
	Args: cobra.NoArgs,
	RunE: runAuditDecideGaps,
}

// messageHasDecideLanguage returns true if the message content contains any of
// the conservative decide-language phrases. Case-insensitive.
func messageHasDecideLanguage(content string) bool {
	lower := strings.ToLower(content)
	for _, kw := range decideKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// decisionsInWindow returns the subset of decisions whose CreatedAt falls
// within ±decideGapCoverageWindow of the given timestamp.
func decisionsInWindow(decisions []string, msgTime time.Time) []string {
	// decisions is a slice of RFC3339 timestamps from the gg store.
	var near []string
	for _, ts := range decisions {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		diff := t.Sub(msgTime)
		if diff < 0 {
			diff = -diff
		}
		if diff <= decideGapCoverageWindow {
			near = append(near, ts)
		}
	}
	return near
}

func runAuditDecideGaps(cmd *cobra.Command, _ []string) error {
	since, err := parseSinceDuration(auditDecideGapsSince)
	if err != nil {
		return err
	}

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()

	w := cmd.OutOrStdout()

	if d.qdrantDown {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning: vector store unavailable — decide-gaps check skipped")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	messages, err := d.store.ListMessagesSince(ctx, since)
	if err != nil {
		return fmt.Errorf("list messages: %w", err)
	}

	decisions, err := d.store.ListDecisions(ctx, 0, true)
	if err != nil {
		return fmt.Errorf("list decisions: %w", err)
	}

	// Build a slice of decision timestamps for window correlation.
	decisionTimes := make([]string, 0, len(decisions))
	for _, dec := range decisions {
		decisionTimes = append(decisionTimes, dec.CreatedAt)
	}

	// System roles that produce non-human messages — exclude from heuristic scan.
	systemRoles := map[string]bool{
		"verify-gate": true,
		"system":      true,
	}

	var flagged []flaggedMsg
	for _, m := range messages {
		if systemRoles[m.FromRole] {
			continue
		}
		if !messageHasDecideLanguage(m.Content) {
			continue
		}
		msgTime, err := time.Parse(time.RFC3339, m.CreatedAt)
		if err != nil {
			continue
		}
		// Skip if there's a decision record within the coverage window.
		if len(decisionsInWindow(decisionTimes, msgTime)) > 0 {
			continue
		}
		flagged = append(flagged, flaggedMsg{
			fromRole: m.FromRole,
			toRole:   m.ToRole,
			content:  m.Content,
			msgTime:  msgTime,
		})
	}

	if len(flagged) == 0 {
		fmt.Fprintf(w, "No decide-gaps — all decision-shaped messages in the last %s have gg record coverage.\n", auditDecideGapsSince)
		return nil
	}

	if isCompactActive(cmd) {
		emitCompact(cmd, "decide-gaps",
			func(out io.Writer) { renderDecideGapsDefault(out, flagged, auditDecideGapsSince) },
			func(out io.Writer) { renderDecideGapsCompact(out, flagged) },
			compactRendererV_decideGaps,
		)
		return nil
	}

	renderDecideGapsDefault(w, flagged, auditDecideGapsSince)
	return nil
}

func renderDecideGapsDefault(w io.Writer, flagged []flaggedMsg, since string) {
	fmt.Fprintf(w, "decide-gaps: %d message(s) with decision-language but no gg record (last %s)\n\n",
		len(flagged), since)
	for _, f := range flagged {
		fmt.Fprintf(w, "  [%s] %s → %s\n", f.msgTime.Format("2006-01-02 15:04"), f.fromRole, f.toRole)
		fmt.Fprintf(w, "    %s\n\n", truncate(f.content, 120))
	}
	fmt.Fprintln(w, "Run `gg record \"<decision>\" --reason \"<why>\"` to capture the rationale.")
}

func renderDecideGapsCompact(w io.Writer, flagged []flaggedMsg) {
	for _, f := range flagged {
		fmt.Fprintf(w, "[%s] %s → %s: %s\n",
			f.msgTime.Format("2006-01-02T15:04"),
			f.fromRole, f.toRole,
			truncate(f.content, 80))
	}
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
