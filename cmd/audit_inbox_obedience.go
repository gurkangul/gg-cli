package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/store"
)

var auditInboxObedienceCmd = &cobra.Command{
	Use:   "inbox-obedience",
	Short: "Measure inbox-obey compliance per agent role",
	Long: `Count role-targeted messages received vs acknowledged (marked read) per agent
role over a time window. Acknowledged = marked read via 'gg inbox' (peek bypassed).

obedience_ratio = acknowledged / received

Roles with ratio < 0.5 and received > 3 are flagged as low-compliance.`,
	RunE: runAuditInboxObedience,
}

var (
	auditObedienceRole  string
	auditObedienceSince string
	auditObedienceJSON  bool
)

func init() {
	auditInboxObedienceCmd.Flags().StringVar(&auditObedienceRole, "role", "",
		"filter to a specific recipient role")
	auditInboxObedienceCmd.Flags().StringVar(&auditObedienceSince, "since", "7d",
		"time window (7d, 24h, 30d, or RFC3339 timestamp)")
	auditInboxObedienceCmd.Flags().BoolVar(&auditObedienceJSON, "json", false,
		"emit machine-readable JSON")
	auditCmd.AddCommand(auditInboxObedienceCmd)
}

// ObedienceRow is the per-role audit result.
type ObedienceRow struct {
	Role           string  `json:"role"`
	Received       int     `json:"received"`
	Acknowledged   int     `json:"acknowledged"`
	ObedienceRatio float64 `json:"obedience_ratio"`
	LowCompliance  bool    `json:"low_compliance"`
}

func runAuditInboxObedience(cmd *cobra.Command, _ []string) error {
	since, err := parseObedienceSince(auditObedienceSince)
	if err != nil {
		return fmt.Errorf("--since: %w", err)
	}

	d, err := loadDeps(false)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	rows, err := computeObedienceRows(ctx, d.store, since, auditObedienceRole)
	if err != nil {
		return err
	}

	if auditObedienceJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
	}

	printObedienceTable(rows, auditObedienceSince)
	return nil
}

// computeObedienceRows fetches all messages since `since` and computes
// per-role acknowledgment rates. roleFilter narrows to one role when set.
func computeObedienceRows(ctx context.Context, client *store.Client, since time.Time, roleFilter string) ([]ObedienceRow, error) {
	msgs, err := client.ListMessagesSince(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("fetch messages: %w", err)
	}

	type counts struct {
		received     int
		acknowledged int
	}
	byRole := make(map[string]*counts)

	for _, m := range msgs {
		targets := targetRoles(m)
		for _, role := range targets {
			if roleFilter != "" && !strings.EqualFold(role, roleFilter) {
				continue
			}
			if byRole[role] == nil {
				byRole[role] = &counts{}
			}
			byRole[role].received++
			if m.Read {
				byRole[role].acknowledged++
			}
		}
	}

	if len(byRole) == 0 {
		return []ObedienceRow{}, nil
	}

	rows := make([]ObedienceRow, 0, len(byRole))
	for role, c := range byRole {
		ratio := 0.0
		if c.received > 0 {
			ratio = float64(c.acknowledged) / float64(c.received)
		}
		rows = append(rows, ObedienceRow{
			Role:           role,
			Received:       c.received,
			Acknowledged:   c.acknowledged,
			ObedienceRatio: ratio,
			LowCompliance:  ratio < 0.5 && c.received > 3,
		})
	}

	// Sort deterministically by role name.
	sortObedienceRows(rows)
	return rows, nil
}

// targetRoles returns role strings a message directly targets for obedience
// accounting. Broadcasts to literal "all" are intentionally excluded: a single
// read bit on a shared broadcast cannot prove per-agent acknowledgement and used
// to produce permanent false "agent all 0%" warnings. Explicit @mentions inside
// a broadcast are still counted as role-targeted messages.
func targetRoles(m store.Message) []string {
	roles := []string{}
	if to := strings.ToLower(strings.TrimSpace(m.ToRole)); to != "" && to != "all" {
		roles = append(roles, to)
	}
	lower := strings.ToLower(m.Content)
	// Extract @mention roles (simple word-boundary parse).
	for _, word := range strings.Fields(lower) {
		if strings.HasPrefix(word, "@") {
			role := strings.TrimPrefix(word, "@")
			role = strings.TrimRight(role, ".,;:!?)")
			if role != "" && role != "all" && !roleInList(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	return roles
}

func roleInList(roles []string, role string) bool {
	for _, r := range roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

func printObedienceTable(rows []ObedienceRow, window string) {
	if len(rows) == 0 {
		fmt.Printf("No role-targeted messages in the last %s.\n", window)
		return
	}

	fmt.Printf("Inbox Obedience  (window: %s)\n", window)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("  %-16s  %8s  %12s  %7s  %s\n",
		"Role", "Received", "Acknowledged", "Ratio", "Status")
	fmt.Println(strings.Repeat("─", 60))
	for _, r := range rows {
		status := "OK"
		if r.LowCompliance {
			status = "⚠ LOW"
		}
		fmt.Printf("  %-16s  %8d  %12d  %6.0f%%  %s\n",
			r.Role, r.Received, r.Acknowledged, r.ObedienceRatio*100, status)
	}
}

// sortObedienceRows sorts rows by role name ascending (stable, in-place).
func sortObedienceRows(rows []ObedienceRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].Role < rows[j-1].Role; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// parseObedienceSince parses a duration string like "7d", "24h", or an RFC3339
// timestamp and returns the absolute cutoff time.
func parseObedienceSince(s string) (time.Time, error) {
	d, err := parseDuration(s)
	if err == nil {
		return time.Now().UTC().Add(-d), nil
	}
	t, err2 := time.Parse(time.RFC3339, s)
	if err2 != nil {
		return time.Time{}, fmt.Errorf("cannot parse %q as duration or RFC3339 timestamp", s)
	}
	return t, nil
}
