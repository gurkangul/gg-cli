package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	_ "modernc.org/sqlite" // registers "sqlite" driver

	"github.com/gurkangul/gg-cli/internal/store"
)

// gsdImportStats tracks what was created vs skipped during a GSD import.
type gsdImportStats struct {
	MilestonesCreated int
	MilestonesSkipped int
	DecisionsCreated  int
	DecisionsSkipped  int
	SlicesCreated     int
	SlicesSkipped     int
}

// runImportFromGSD mirrors GSD milestones, decisions, and completed slices
// into gg. It is idempotent: items already tagged gsd-source:<id> are skipped.
func runImportFromGSD(cmd *cobra.Command, projectDir string) error {
	dbPath := filepath.Join(projectDir, ".gsd", "gsd.db")
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no GSD database found at %s — run this command from a GSD project root or use --project", dbPath)
	}

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("open gsd.db: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Verify the DB is readable.
	if err := db.PingContext(cmd.Context()); err != nil {
		return fmt.Errorf("read gsd.db: %w", err)
	}

	d, err := loadDeps(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// Build sets of already-imported source IDs to ensure idempotency.
	importedTasks, err := gsdImportedSourceIDs(ctx, d, "tasks")
	if err != nil {
		return fmt.Errorf("scan existing tasks: %w", err)
	}
	importedDecisions, err := gsdImportedSourceIDs(ctx, d, "decisions")
	if err != nil {
		return fmt.Errorf("scan existing decisions: %w", err)
	}
	importedNotes, err := gsdImportedSourceIDs(ctx, d, "notes")
	if err != nil {
		return fmt.Errorf("scan existing notes: %w", err)
	}

	stats := gsdImportStats{}

	// ── Milestones → tasks ─────────────────────────────────────────────────
	milestones, err := gsdReadMilestones(cmd.Context(), db)
	if err != nil {
		return fmt.Errorf("read milestones: %w", err)
	}
	for _, m := range milestones {
		sourceTag := "gsd-source:" + m.ID
		if importedTasks[sourceTag] {
			stats.MilestonesSkipped++
			continue
		}
		title := fmt.Sprintf("[GSD] %s", m.Title)
		detail := fmt.Sprintf("GSD milestone %s (status: %s)", m.ID, m.Status)
		if m.Vision != "" {
			detail += "\n\n" + m.Vision
		}
		tags := []string{"gsd-imported", "milestone", sourceTag}

		embedText := title + " " + detail
		vector, err := d.embedder.Generate(ctx, embedText)
		if err != nil {
			return fmt.Errorf("embed milestone %s: %w", m.ID, err)
		}
		t := store.Task{
			Title:    title,
			Detail:   detail,
			Priority: "high",
			Tags:     tags,
			Author:   "gsd-import",
		}
		id, err := d.store.CreateTask(ctx, t, vector)
		if err != nil {
			return fmt.Errorf("create task for milestone %s: %w", m.ID, err)
		}
		fmt.Printf("  ✓ task %s ← GSD milestone %s\n", id, m.ID)
		stats.MilestonesCreated++
	}

	// ── Decisions ──────────────────────────────────────────────────────────
	decisions, err := gsdReadDecisions(cmd.Context(), db)
	if err != nil {
		return fmt.Errorf("read decisions: %w", err)
	}
	for _, dec := range decisions {
		sourceTag := "gsd-source:" + dec.ID
		if importedDecisions[sourceTag] {
			stats.DecisionsSkipped++
			continue
		}
		text := dec.Decision
		if dec.Choice != "" {
			text = dec.Decision + " → " + dec.Choice
		}
		tags := []string{"gsd-imported", sourceTag}

		embedText := text
		if dec.Rationale != "" {
			embedText += " " + dec.Rationale
		}
		vector, err := d.embedder.Generate(ctx, embedText)
		if err != nil {
			return fmt.Errorf("embed decision %s: %w", dec.ID, err)
		}
		gd := store.Decision{
			ID:     dec.ID, // stable UUID from GSD — Qdrant upserts are idempotent
			Text:   text,
			Reason: dec.Rationale,
			Tags:   tags,
			Author: dec.MadeBy,
		}
		if err := d.store.AddDecision(ctx, gd, vector); err != nil {
			return fmt.Errorf("store decision %s: %w", dec.ID, err)
		}
		fmt.Printf("  ✓ decision ← GSD %s\n", dec.ID)
		stats.DecisionsCreated++
	}

	// ── Completed/active slices → notes ────────────────────────────────────
	slices, err := gsdReadSlices(cmd.Context(), db)
	if err != nil {
		return fmt.Errorf("read slices: %w", err)
	}
	for _, s := range slices {
		sourceID := s.MilestoneID + "/" + s.ID
		sourceTag := "gsd-source:" + sourceID
		if importedNotes[sourceTag] {
			stats.SlicesSkipped++
			continue
		}
		text := fmt.Sprintf("[GSD] Slice %s/%s (%s): %s", s.MilestoneID, s.ID, s.Status, s.Title)
		tags := []string{"gsd-imported", "slice-" + s.Status, "milestone-" + s.MilestoneID, sourceTag}

		vector, err := d.embedder.Generate(ctx, text)
		if err != nil {
			return fmt.Errorf("embed slice %s: %w", sourceID, err)
		}
		n := store.Note{
			Text: text,
			Tags: tags,
		}
		if _, err := d.store.AddNote(ctx, n, vector); err != nil {
			return fmt.Errorf("store note for slice %s: %w", sourceID, err)
		}
		fmt.Printf("  ✓ note ← GSD slice %s/%s (%s)\n", s.MilestoneID, s.ID, s.Status)
		stats.SlicesCreated++
	}

	fmt.Printf("\nImported from %s:\n", dbPath)
	fmt.Printf("  Milestones: %d created, %d skipped\n", stats.MilestonesCreated, stats.MilestonesSkipped)
	fmt.Printf("  Decisions:  %d created, %d skipped\n", stats.DecisionsCreated, stats.DecisionsSkipped)
	fmt.Printf("  Slices:     %d created, %d skipped\n", stats.SlicesCreated, stats.SlicesSkipped)
	return nil
}

// gsdImportedSourceIDs scans gg items in a collection and returns the set of
// gsd-source:<id> tags already present. Used for idempotency checks.
func gsdImportedSourceIDs(ctx context.Context, d *deps, kind string) (map[string]bool, error) {
	seen := map[string]bool{}
	var items []tagList
	switch kind {
	case "tasks":
		tasks, err := d.store.ListTasks(ctx, "")
		if err != nil {
			return nil, err
		}
		for _, t := range tasks {
			items = append(items, t.Tags)
		}
	case "decisions":
		decs, err := d.store.ListDecisions(ctx, 10000)
		if err != nil {
			return nil, err
		}
		for _, dec := range decs {
			items = append(items, dec.Tags)
		}
	case "notes":
		notes, err := d.store.ListNotes(ctx, 10000)
		if err != nil {
			return nil, err
		}
		for _, n := range notes {
			items = append(items, n.Tags)
		}
	}
	for _, tags := range items {
		for _, tag := range tags {
			if strings.HasPrefix(tag, "gsd-source:") {
				seen[tag] = true
			}
		}
	}
	return seen, nil
}

type tagList = []string

// ── GSD DB row types ────────────────────────────────────────────────────────

type gsdMilestone struct {
	ID     string
	Title  string
	Status string
	Vision string
}

type gsdDecision struct {
	ID        string
	Decision  string
	Choice    string
	Rationale string
	MadeBy    string
}

type gsdSlice struct {
	MilestoneID string
	ID          string
	Title       string
	Status      string
}

func gsdReadMilestones(ctx context.Context, db *sql.DB) ([]gsdMilestone, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title, status, vision FROM milestones ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []gsdMilestone
	for rows.Next() {
		var m gsdMilestone
		if err := rows.Scan(&m.ID, &m.Title, &m.Status, &m.Vision); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func gsdReadDecisions(ctx context.Context, db *sql.DB) ([]gsdDecision, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, decision, choice, rationale, made_by FROM decisions
		 WHERE superseded_by IS NULL ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []gsdDecision
	for rows.Next() {
		var d gsdDecision
		if err := rows.Scan(&d.ID, &d.Decision, &d.Choice, &d.Rationale, &d.MadeBy); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func gsdReadSlices(ctx context.Context, db *sql.DB) ([]gsdSlice, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT milestone_id, id, title, status FROM slices
		 WHERE status IN ('complete', 'in_progress', 'pending') ORDER BY milestone_id, sequence, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []gsdSlice
	for rows.Next() {
		var s gsdSlice
		if err := rows.Scan(&s.MilestoneID, &s.ID, &s.Title, &s.Status); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
