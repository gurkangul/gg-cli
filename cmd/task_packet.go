package cmd

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var taskPacketCmd = &cobra.Command{
	Use:   "packet TASK-ID",
	Short: "Print a reviewer handoff packet for a task",
	Long: `Print a read-only reviewer handoff packet for a task.

The packet gathers the current task projection, ready_for_live plan, linked
decisions, task-scoped inbox messages, local lifecycle events, and suggested
reviewer commands. It does not approve, reject, mark done, or advance inbox
cursors.`,
	Args: cobra.ExactArgs(1),
	RunE: runTaskPacket,
}

func init() {
	taskCmd.AddCommand(taskPacketCmd)
}

type taskPacket struct {
	Task       *store.Task
	Decisions  []store.Decision
	Rejections []store.Rejection
	Messages   []store.Message
	Events     []taskPacketEvent
	Warnings   []string
	SkippedLog int
}

type taskPacketEvent struct {
	Action     string
	FromStatus string
	ToStatus   string
	Owner      string
	Actor      string
	Detail     string
	CreatedAt  string
}

func runTaskPacket(cmd *cobra.Command, args []string) error {
	taskID, err := requireTaskID(args[0])
	if err != nil {
		return err
	}
	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()
	if d.qdrantDown || d.qdrantSlow {
		return storeDownErr()
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	t, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return notFound(err.Error())
	}
	packet := taskPacket{Task: t}

	if decisions, decErr := d.store.ListDecisionsByTaskID(ctx, taskID); decErr == nil {
		packet.Decisions = trimDecisions(decisions, 5)
	} else {
		packet.Warnings = append(packet.Warnings, fmt.Sprintf("linked records unavailable: %v", decErr))
	}
	if rejections, rejErr := d.store.ListRejections(ctx, 0); rejErr == nil {
		packet.Rejections = trimRejections(filterTaskRejections(taskID, rejections), 5)
	} else {
		packet.Warnings = append(packet.Warnings, fmt.Sprintf("linked rejections unavailable: %v", rejErr))
	}

	if messages, msgErr := d.store.ListMessagesSince(ctx, time.Now().UTC().AddDate(0, 0, -30)); msgErr == nil {
		packet.Messages = trimMessages(filterTaskMessages(taskID, messages), 8)
	} else {
		packet.Warnings = append(packet.Warnings, fmt.Sprintf("handoff messages unavailable: %v", msgErr))
	}

	if events, skipped, eventErr := readTaskPacketEvents(taskID); eventErr == nil {
		packet.Events = trimEvents(events, 8)
		packet.SkippedLog = skipped
	} else {
		packet.Warnings = append(packet.Warnings, fmt.Sprintf("task event log unavailable: %v", eventErr))
	}

	renderTaskPacket(cmd.OutOrStdout(), packet)
	return nil
}

func trimDecisions(in []store.Decision, limit int) []store.Decision {
	if len(in) > limit {
		return in[:limit]
	}
	return in
}

func trimRejections(in []store.Rejection, limit int) []store.Rejection {
	if len(in) > limit {
		return in[:limit]
	}
	return in
}

func trimMessages(in []store.Message, limit int) []store.Message {
	sort.SliceStable(in, func(i, j int) bool { return in[i].CreatedAt > in[j].CreatedAt })
	if len(in) > limit {
		return in[:limit]
	}
	return in
}

func trimEvents(in []taskPacketEvent, limit int) []taskPacketEvent {
	sort.SliceStable(in, func(i, j int) bool { return in[i].CreatedAt > in[j].CreatedAt })
	if len(in) > limit {
		return in[:limit]
	}
	return in
}

func filterTaskMessages(taskID string, messages []store.Message) []store.Message {
	var out []store.Message
	needle := strings.ToUpper(taskID)
	for _, m := range messages {
		if strings.EqualFold(m.TaskID, taskID) || strings.Contains(strings.ToUpper(m.Content), needle) {
			out = append(out, m)
		}
	}
	return out
}

func filterTaskRejections(taskID string, rejections []store.Rejection) []store.Rejection {
	var out []store.Rejection
	for _, r := range rejections {
		if strings.EqualFold(r.TaskID, taskID) {
			out = append(out, r)
		}
	}
	return out
}

func readTaskPacketEvents(taskID string) ([]taskPacketEvent, int, error) {
	ggDir, err := config.GGDir()
	if err != nil {
		return nil, 0, err
	}
	entries, skipped, err := brain.ReadAllWithCount(ggDir, "task-events")
	if err != nil {
		return nil, skipped, err
	}
	events := make([]taskPacketEvent, 0)
	for _, e := range entries {
		if !strings.EqualFold(payloadString(e.Payload, "task_id"), taskID) {
			continue
		}
		events = append(events, taskPacketEvent{
			Action:     payloadString(e.Payload, "action"),
			FromStatus: payloadString(e.Payload, "from_status"),
			ToStatus:   payloadString(e.Payload, "to_status"),
			Owner:      payloadString(e.Payload, "owner"),
			Actor:      payloadString(e.Payload, "actor"),
			Detail:     payloadString(e.Payload, "detail"),
			CreatedAt:  payloadString(e.Payload, "created_at"),
		})
	}
	return events, skipped, nil
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func renderTaskPacket(w io.Writer, p taskPacket) {
	t := p.Task
	fmt.Fprintf(w, "Task packet: %s\n", t.ID)
	fmt.Fprintf(w, "%s [%s] %s\n", statusIcon(t.Status), t.Priority, t.Title)
	fmt.Fprintf(w, "Status: %s\n", t.Status)
	if t.Owner != "" {
		fmt.Fprintf(w, "Owner: %s\n", t.Owner)
		if t.LeaseUntil != "" {
			fmt.Fprintf(w, "Lease until: %s\n", t.LeaseUntil)
		}
	}
	if t.ReadyForLiveBy != "" || t.ReadyForLivePlan != "" {
		fmt.Fprintln(w, "\nReady for live:")
		fmt.Fprintf(w, "  By: %s\n", emptyDash(t.ReadyForLiveBy))
		fmt.Fprintf(w, "  At: %s\n", emptyDash(t.ReadyForLiveAt))
		fmt.Fprintf(w, "  Plan: %s\n", emptyDash(t.ReadyForLivePlan))
	}
	if t.ReviewStatus != "" && t.ReviewStatus != "none" {
		fmt.Fprintln(w, "\nReview:")
		fmt.Fprintf(w, "  Status: %s\n", t.ReviewStatus)
		fmt.Fprintf(w, "  By: %s\n", emptyDash(t.ReviewedBy))
		fmt.Fprintf(w, "  Notes: %s\n", emptyDash(t.ReviewNotes))
	}

	if len(p.Warnings) > 0 || p.SkippedLog > 0 {
		fmt.Fprintln(w, "\nWarnings:")
		for _, warning := range p.Warnings {
			fmt.Fprintf(w, "  - %s\n", warning)
		}
		if p.SkippedLog > 0 {
			fmt.Fprintf(w, "  - task-events skipped malformed lines: %d\n", p.SkippedLog)
		}
	}

	fmt.Fprintln(w, "\nLast records:")
	if len(p.Decisions) == 0 && len(p.Rejections) == 0 {
		fmt.Fprintln(w, "  (no decisions/rejections linked via --task)")
	}
	for _, d := range p.Decisions {
		fmt.Fprintf(w, "  - D %s %s — %s\n", shortID(d.ID), d.CreatedAt, compactTrim(d.Text, 100))
	}
	for _, r := range p.Rejections {
		fmt.Fprintf(w, "  - R %s %s — %s\n", shortID(r.ID), r.CreatedAt, compactTrim(r.Approach, 100))
	}

	fmt.Fprintln(w, "\nHandoff inbox messages:")
	if len(p.Messages) == 0 {
		fmt.Fprintln(w, "  (no task-scoped messages from last 30 days)")
	} else {
		for _, m := range p.Messages {
			fmt.Fprintf(w, "  - %s [%s → %s] %s\n", m.CreatedAt, m.FromRole, m.ToRole, compactTrim(m.Content, 120))
		}
	}

	fmt.Fprintln(w, "\nTask lifecycle events:")
	if len(p.Events) == 0 {
		fmt.Fprintln(w, "  (no local task-events entries)")
	} else {
		for _, e := range p.Events {
			transition := e.Action
			if e.FromStatus != "" || e.ToStatus != "" {
				transition = fmt.Sprintf("%s %s→%s", e.Action, emptyDash(e.FromStatus), emptyDash(e.ToStatus))
			}
			fmt.Fprintf(w, "  - %s %s by %s", e.CreatedAt, transition, emptyDash(e.Actor))
			if e.Owner != "" {
				fmt.Fprintf(w, " owner=%s", e.Owner)
			}
			if e.Detail != "" {
				fmt.Fprintf(w, " — %s", compactTrim(e.Detail, 100))
			}
			fmt.Fprintln(w)
		}
	}

	fmt.Fprintln(w, "\nChanged files / impact summary:")
	fmt.Fprintln(w, "  (not stored in task metadata; reviewer should inspect git diff and any task-scoped evidence messages)")
	fmt.Fprintln(w, "  Evidence packet should include: commands run, live smoke, impacted files, known gaps, artifact paths")
	fmt.Fprintln(w, "  Suggested per changed file: gg impact <file> --compact")

	fmt.Fprintln(w, "\nTest evidence:")
	if t.ReadyForLivePlan != "" {
		fmt.Fprintf(w, "  Ready-for-live plan: %s\n", t.ReadyForLivePlan)
	} else {
		fmt.Fprintln(w, "  (not recorded yet; expect smoke/gate transcript before closure)")
	}

	fmt.Fprintln(w, "\nSuggested reviewer commands:")
	fmt.Fprintf(w, "  gg task get %s\n", t.ID)
	fmt.Fprintf(w, "  gg task packet %s\n", t.ID)
	if t.Status == "ready_for_live" {
		fmt.Fprintf(w, "  gg task review %s --approve --by reviewer-1\n", t.ID)
		fmt.Fprintf(w, "  gg task done %s \"verified: tests + live smoke\" --verifier reviewer-1\n", t.ID)
	} else {
		fmt.Fprintf(w, "  gg context --for-task %s\n", t.ID)
	}
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
