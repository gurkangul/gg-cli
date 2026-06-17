package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
)

var (
	reconcileApply bool
	reconcileNow   = time.Now
)

var reconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: experimentalShort("Reconcile append-only task events with the live task projection"),
	Long: experimentalLong(`Compares .gg/brain/task-events.jsonl against the vector store task projection.

Default mode is read-only: reports missing projections, projection drift,
orphaned owners/leases, and stale leases. Use --apply to repair safe cases:
missing non-cancelled projections are replayed from .gg/brain/tasks.jsonl,
drifted task lifecycle fields are reset from the event log, stale leases are
released back to pending, and projected cancelled tasks are removed.`),
	Args: cobra.NoArgs,
	RunE: runReconcile,
}

func init() {
	reconcileCmd.Flags().BoolVar(&reconcileApply, "apply", false, "repair safe task projection drift")
	rootCmd.AddCommand(reconcileCmd)
}

type taskProjection struct {
	ID         string
	Status     string
	Owner      string
	ClaimedAt  string
	LeaseUntil string
	Cancelled  bool
}

type taskDrift struct {
	TaskID string
	Kind   string
	Detail string
	Fields map[string]any
}

func runReconcile(cmd *cobra.Command, _ []string) error {
	fmt.Println("GG Reconcile")
	fmt.Println(strings.Repeat("─", 50))

	ggDir, err := config.GGDir()
	if err != nil {
		return fmt.Errorf("find .gg dir: %w", err)
	}
	events, skipped, err := brain.ReadAllWithCount(ggDir, "task-events")
	if err != nil {
		return err
	}
	fmt.Printf("task event log: .gg/brain/task-events.jsonl (%d entries", len(events))
	if skipped > 0 {
		fmt.Printf(", %d malformed skipped", skipped)
	}
	fmt.Println(")")

	d, err := loadDepsReadOnly(false)
	if err != nil {
		return err
	}
	defer d.Close()
	if d.qdrantDown {
		return fmt.Errorf("qdrant unreachable — cannot compare live task projection")
	}

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	creates, err := taskCreateEntries(ggDir)
	if err != nil {
		return err
	}
	expected := foldTaskEvents(creates, events)
	actual, err := currentTaskMap(ctx, d.store)
	if err != nil {
		return err
	}

	drifts := reconcileTaskDrift(expected, actual, reconcileNow().UTC())
	if len(drifts) == 0 {
		fmt.Println("✓ task projection matches event log")
		return nil
	}
	sort.Slice(drifts, func(i, j int) bool {
		if drifts[i].TaskID == drifts[j].TaskID {
			return drifts[i].Kind < drifts[j].Kind
		}
		return drifts[i].TaskID < drifts[j].TaskID
	})
	fmt.Printf("Found %d task projection issue(s):\n", len(drifts))
	for _, drift := range drifts {
		fmt.Printf("  %s %-18s %s\n", drift.TaskID, drift.Kind, drift.Detail)
	}
	if !reconcileApply {
		fmt.Println("\nRead-only. Re-run with --apply to repair safe task projection drift.")
		return fmt.Errorf("task projection drift found")
	}

	repaired := 0
	for _, drift := range drifts {
		if err := applyTaskDrift(ctx, d.store, creates, drift); err != nil {
			return err
		}
		repaired++
	}
	fmt.Printf("\n✓ repaired %d task projection issue(s)\n", repaired)
	return nil
}

func taskCreateEntries(ggDir string) (map[string]brain.Entry, error) {
	entries, _, err := brain.ReadAllWithCount(ggDir, "tasks")
	if err != nil {
		return nil, err
	}
	out := make(map[string]brain.Entry, len(entries))
	for _, e := range entries {
		id := stringField(e.Payload, "task_id")
		if id != "" {
			out[id] = e
		}
	}
	return out, nil
}

func currentTaskMap(ctx context.Context, sc *store.Client) (map[string]store.Task, error) {
	tasks, err := sc.ListTasks(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string]store.Task, len(tasks))
	for _, t := range tasks {
		out[t.ID] = t
	}
	return out, nil
}

func foldTaskEvents(creates map[string]brain.Entry, events []brain.Entry) map[string]taskProjection {
	out := make(map[string]taskProjection, len(creates))
	for id, e := range creates {
		out[id] = projectionFromCreate(e)
	}
	for _, e := range events {
		id := stringField(e.Payload, "task_id")
		if id == "" {
			continue
		}
		p := out[id]
		if p.ID == "" {
			p.ID = id
			p.Status = "pending"
		}
		action := stringField(e.Payload, "action")
		toStatus := stringField(e.Payload, "to_status")
		switch action {
		case "created":
			if p.Status == "" {
				p.Status = defaultStatus(toStatus, "pending")
			}
		case "started", "renewed":
			p.Status = defaultStatus(toStatus, "in_progress")
			p.Owner = stringField(e.Payload, "owner")
			p.LeaseUntil = stringField(e.Payload, "lease_until")
			if action == "started" {
				p.ClaimedAt = stringField(e.Payload, "created_at")
			}
			p.Cancelled = false
		case "released", "lease_expired":
			p.Status = defaultStatus(toStatus, "pending")
			p.Owner = ""
			p.ClaimedAt = ""
			p.LeaseUntil = ""
			p.Cancelled = false
		case "ready_for_live", "ready_for_live_updated":
			p.Status = defaultStatus(toStatus, "ready_for_live")
		case "done", "blocked":
			p.Status = defaultStatus(toStatus, action)
		case "cancelled":
			p.Status = "cancelled"
			p.Owner = ""
			p.ClaimedAt = ""
			p.LeaseUntil = ""
			p.Cancelled = true
		}
		out[id] = p
	}
	return out
}

func projectionFromCreate(e brain.Entry) taskProjection {
	id := stringField(e.Payload, "task_id")
	return taskProjection{
		ID:         id,
		Status:     defaultStatus(stringField(e.Payload, "status"), "pending"),
		Owner:      stringField(e.Payload, "owner"),
		ClaimedAt:  stringField(e.Payload, "claimed_at"),
		LeaseUntil: stringField(e.Payload, "lease_until"),
	}
}

func reconcileTaskDrift(expected map[string]taskProjection, actual map[string]store.Task, now time.Time) []taskDrift {
	var drifts []taskDrift
	for id, exp := range expected {
		got, ok := actual[id]
		switch {
		case exp.Cancelled && ok:
			drifts = append(drifts, taskDrift{TaskID: id, Kind: "cancelled_projection", Detail: "cancelled task still exists in the vector store"})
			continue
		case exp.Cancelled:
			continue
		case !ok:
			drifts = append(drifts, taskDrift{TaskID: id, Kind: "missing_projection", Detail: "task-events has non-cancelled task but the vector store is missing it", Fields: projectionFields(exp, nil, now)})
			continue
		}
		fields := projectionDiffFields(exp, got, now)
		if len(fields) > 0 {
			drifts = append(drifts, taskDrift{TaskID: id, Kind: "projection_drift", Detail: diffDetail(exp, got), Fields: fields})
		}
		if staleLease(got, now) {
			drifts = append(drifts, taskDrift{TaskID: id, Kind: "stale_lease", Detail: fmt.Sprintf("owner %s lease expired at %s", got.Owner, got.LeaseUntil), Fields: staleLeaseFields(got, now)})
		}
		if orphanedLease(got) {
			drifts = append(drifts, taskDrift{TaskID: id, Kind: "orphaned_lease", Detail: "owner/lease fields are internally inconsistent", Fields: staleLeaseFields(got, now)})
		}
	}
	return drifts
}

func projectionDiffFields(exp taskProjection, got store.Task, now time.Time) map[string]any {
	fields := map[string]any{}
	if got.Status != exp.Status {
		fields["status"] = exp.Status
	}
	if got.Owner != exp.Owner {
		fields["owner"] = exp.Owner
	}
	if got.ClaimedAt != exp.ClaimedAt {
		fields["claimed_at"] = exp.ClaimedAt
	}
	if got.LeaseUntil != exp.LeaseUntil {
		fields["lease_until"] = exp.LeaseUntil
	}
	if len(fields) == 0 {
		return nil
	}
	return projectionFields(exp, &got, now)
}

func projectionFields(exp taskProjection, got *store.Task, now time.Time) map[string]any {
	version := int64(1)
	if got != nil && got.Version > 0 {
		version = got.Version + 1
	}
	return map[string]any{
		"status":       exp.Status,
		"owner":        exp.Owner,
		"claimed_at":   exp.ClaimedAt,
		"lease_until":  exp.LeaseUntil,
		"updated_at":   now.Format(time.RFC3339),
		"task_version": version,
	}
}

func staleLeaseFields(got store.Task, now time.Time) map[string]any {
	version := got.Version + 1
	if version <= 0 {
		version = 1
	}
	return map[string]any{
		"status":       "pending",
		"owner":        "",
		"claimed_at":   "",
		"lease_until":  "",
		"updated_at":   now.Format(time.RFC3339),
		"task_version": version,
	}
}

func applyTaskDrift(ctx context.Context, sc *store.Client, creates map[string]brain.Entry, drift taskDrift) error {
	switch drift.Kind {
	case "missing_projection":
		create, ok := creates[drift.TaskID]
		if !ok {
			return fmt.Errorf("%s missing from tasks.jsonl; cannot replay safely", drift.TaskID)
		}
		if err := sc.ReplayBrainEntry(ctx, "tasks", create.UUID, create.Payload); err != nil {
			return err
		}
		return sc.SetTaskProjectionPayload(ctx, drift.TaskID, drift.Fields)
	case "cancelled_projection":
		return sc.CancelTask(ctx, drift.TaskID, "reconcile", "task-events says task is cancelled")
	case "projection_drift", "orphaned_lease":
		return sc.SetTaskProjectionPayload(ctx, drift.TaskID, drift.Fields)
	case "stale_lease":
		if err := sc.SetTaskProjectionPayload(ctx, drift.TaskID, drift.Fields); err != nil {
			return err
		}
		return sc.AppendTaskEvent(store.TaskEvent{
			TaskID:    drift.TaskID,
			Action:    "lease_expired",
			ToStatus:  "pending",
			Actor:     "reconcile",
			Detail:    drift.Detail,
			CreatedAt: reconcileNow().UTC().Format(time.RFC3339),
		})
	default:
		return fmt.Errorf("unknown drift kind %q for %s", drift.Kind, drift.TaskID)
	}
}

func staleLease(t store.Task, now time.Time) bool {
	if t.Status != "in_progress" || t.Owner == "" || t.LeaseUntil == "" {
		return false
	}
	leaseUntil, err := time.Parse(time.RFC3339, t.LeaseUntil)
	return err == nil && !leaseUntil.After(now)
}

func orphanedLease(t store.Task) bool {
	if t.Owner == "" {
		return t.ClaimedAt != "" || t.LeaseUntil != ""
	}
	if t.LeaseUntil == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339, t.LeaseUntil)
	return err != nil
}

func diffDetail(exp taskProjection, got store.Task) string {
	parts := make([]string, 0, 4)
	appendDiff := func(field, expected, actual string) {
		if expected != actual {
			parts = append(parts, fmt.Sprintf("field %s expected=%q actual=%q", field, expected, actual))
		}
	}
	appendDiff("status", exp.Status, got.Status)
	appendDiff("owner", exp.Owner, got.Owner)
	appendDiff("claimed_at", exp.ClaimedAt, got.ClaimedAt)
	appendDiff("lease_until", exp.LeaseUntil, got.LeaseUntil)
	if len(parts) == 0 {
		return "projection drift detected but lifecycle fields compare equal; run with --apply to refresh metadata fields"
	}
	return strings.Join(parts, "; ")
}

func defaultStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func stringField(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	switch v := payload[key].(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	case nil:
		return ""
	}
}
