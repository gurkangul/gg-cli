package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/spf13/cobra"
)

var recordCmd = &cobra.Command{
	Use:   `record "text"`,
	Short: "Record that a decision was made (the canonical knowledge-capture verb)",
	Long: `Record an architectural decision, rejected approach, or design choice.

WHEN TO USE: you've concluded something — chosen a library, rejected an approach,
established a constraint. Anything that would appear in an ADR belongs here.

WHEN NOT TO USE: for in-progress deliberation use 'gg message send'; for task
tracking use 'gg task create'; for progress notes use 'gg task done'.

Examples:
  gg record "use JWT for auth" --reason "stateless, scales horizontally" --tags "auth,security"
  gg record "do NOT use Redis sessions" --decision-status=rejected --reason "ops burden"
  gg record "switch to PostgreSQL" --rejected-alternatives "MySQL,SQLite" --implements TASK-003

See also: gg task (track work), gg search (find context), gg status (overview)`,
	Args: cobra.ExactArgs(1),
	RunE: runRecord,
}

var (
	recordReason     string
	recordTags                string
	recordTask                string
	recordStance              string
	recordDecisionStatus      string
	recordRejectedAlternatives string
	recordImplements          string // TASK-X that implements this decision → (Decision)-[:DECIDES]->(Task)
	recordRejects             string // DEC-UUID that this decision supersedes → (Decision)-[:REJECTS]->(Decision)
)

func init() {
	recordCmd.Flags().StringVar(&recordReason, "reason", "", "why this decision was made (or rejected)")
	recordCmd.Flags().StringVar(&recordTags, "tags", "", "comma-separated tags")
	recordCmd.Flags().StringVar(&recordTask, "task", "", "related task ID")
	recordCmd.Flags().StringVar(&recordStance, "stance", "accept", `stance: "accept" (decision) or "reject" (rejection)`)
	recordCmd.Flags().StringVar(&recordDecisionStatus, "decision-status", "active", "decision lifecycle status: active, superseded, rejected")
	recordCmd.Flags().StringVar(&recordRejectedAlternatives, "rejected-alternatives", "", "comma-separated approaches that were considered and rejected")
	recordCmd.Flags().StringVar(&recordImplements, "implements", "", "TASK-X that implements this decision (writes Memgraph edge)")
	recordCmd.Flags().StringVar(&recordRejects, "rejects", "", "decision UUID superseded by this one (writes Memgraph edge)")
	addFromFlag(recordCmd)
	rootCmd.AddCommand(recordCmd)
}

func runRecord(cmd *cobra.Command, args []string) error {
	printProjectBanner()
	text, err := requireNonEmpty("text", args[0])
	if err != nil {
		return err
	}

	stance := strings.ToLower(strings.TrimSpace(recordStance))
	if stance != "accept" && stance != "reject" {
		return fmt.Errorf("--stance must be \"accept\" or \"reject\", got %q", recordStance)
	}

	reason := strings.TrimSpace(recordReason)
	taskRef, err := normalizeTaskRef(recordTask)
	if err != nil {
		return fmt.Errorf("--task: %w", err)
	}

	if err := requireAgentIdentity(); err != nil {
		return err
	}

	// AC-2: Use offline-safe loader — write commands now tolerate Qdrant down.
	d, err := loadDepsOfflineSafe(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// AC-4: InboxGatePreflight already fails-open when Qdrant is unreachable.
	if err := runInboxGatePreflight(ctx, d.store, "record"); err != nil {
		return err
	}

	var vector []float32
	if !d.qdrantDown {
		embedText := text
		if reason != "" {
			embedText = text + " " + reason
		}
		vector, err = d.embedder.Generate(ctx, embedText)
		if err != nil {
			return fmt.Errorf("generate embedding: %w", err)
		}

		dupKind := "decisions"
		if stance == "reject" {
			dupKind = "rejections"
		}
		// AC-4: skip promptIfDuplicate when Qdrant is down.
		if promptIfDuplicate(ctx, d, dupKind, vector) {
			fmt.Println("Aborted — nothing recorded.")
			return nil
		}
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ Qdrant unreachable — read served from JSONL (may miss cross-project context)")
	}

	if stance == "reject" {
		r := store.Rejection{
			Approach: text,
			Reason:   reason,
			Tags:     parseTags(recordTags),
			TaskID:   taskRef,
			Author:   resolveAuthor(cmd),
		}
		if addErr := d.store.AddRejection(ctx, r, vector); addErr != nil {
			var oq *store.OutboxQueued
			if errors.As(addErr, &oq) {
				queueBrainOutbox(oq, config.GGDirOrEmpty())
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ queued for vector index (Qdrant unreachable; will replay on recovery)")
			} else {
				return fmt.Errorf("store rejection: %w", addErr)
			}
		}
		return printJSON(r, func() {
			fmt.Printf("✗ Rejection recorded: %s\n", text)
			if r.Reason != "" {
				fmt.Printf("  Reason: %s\n", r.Reason)
			}
			if len(r.Tags) > 0 {
				fmt.Printf("  Tags: %s\n", strings.Join(r.Tags, ", "))
			}
		})
	}

	decStatus := strings.TrimSpace(recordDecisionStatus)
	if decStatus == "" {
		decStatus = "active"
	}
	if !store.ValidDecisionStatuses[decStatus] {
		return fmt.Errorf("--decision-status must be active, superseded, or rejected — got %q", decStatus)
	}

	dec := store.Decision{
		ID:                   uuid.New().String(), // pre-assign so we can reference it in Memgraph edges
		Text:                 text,
		Reason:               reason,
		Status:               decStatus,
		RejectedAlternatives: parseTags(recordRejectedAlternatives),
		Tags:                 parseTags(recordTags),
		TaskID:               taskRef,
		Author:               resolveAuthor(cmd),
	}
	if addErr := d.store.AddDecision(ctx, dec, vector); addErr != nil {
		var oq *store.OutboxQueued
		if errors.As(addErr, &oq) {
			queueBrainOutbox(oq, config.GGDirOrEmpty())
			fmt.Fprintln(cmd.ErrOrStderr(), "⚠ queued for vector index (Qdrant unreachable; will replay on recovery)")
		} else {
			return fmt.Errorf("store decision: %w", addErr)
		}
	}

	// Always upsert the Decision node to Memgraph so `gg impact` and graph
	// queries see every decision — edges (DECIDES / REJECTS) remain conditional
	// on --implements / --rejects. Best-effort: warn but don't fail on graph
	// errors (Qdrant remains source of truth).
	implementsRef := strings.TrimSpace(recordImplements)
	rejectsRef := strings.TrimSpace(recordRejects)
	writeGraphEdges(cmd, ctx, dec, implementsRef, rejectsRef)

	return printJSON(dec, func() {
		fmt.Printf("✓ Decision recorded: %s\n", text)
		if dec.Reason != "" {
			fmt.Printf("  Reason: %s\n", dec.Reason)
		}
		if len(dec.Tags) > 0 {
			fmt.Printf("  Tags: %s\n", strings.Join(dec.Tags, ", "))
		}
	})
}

// writeGraphEdges writes optional Memgraph cross-link edges after a decision is stored in Qdrant.
// Failures are non-fatal: we warn to stderr but let the command succeed so Qdrant is always
// the source of truth. Memgraph is a derived view that can be rebuilt from the export.
func writeGraphEdges(cmd *cobra.Command, ctx interface{ Done() <-chan struct{} }, dec store.Decision, implementsRef, rejectsRef string) {
	cfg, err := config.Load()
	if err != nil || cfg.Memgraph.URI == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ Memgraph unavailable — skipping graph edge write (Qdrant record is intact)")
		return
	}

	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph init failed (%v) — skipping graph edges\n", err)
		return
	}
	defer func() { _ = gc.Close(cmd.Context()) }()

	// Upsert the Decision node itself.
	if uErr := gc.UpsertDecisionNode(cmd.Context(), dec.ID, dec.Text); uErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph Decision node upsert failed: %v\n", uErr)
		return
	}

	if implementsRef != "" {
		taskRef, normErr := normalizeTaskRef(implementsRef)
		if normErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ --implements: %v\n", normErr)
		} else {
			if uErr := gc.UpsertTaskNode(cmd.Context(), taskRef, taskRef); uErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph Task node upsert failed: %v\n", uErr)
			} else if eErr := gc.UpsertDecidesEdge(cmd.Context(), dec.ID, taskRef); eErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph DECIDES edge failed: %v\n", eErr)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "  Graph: (Decision)-[:DECIDES]->(%s)\n", taskRef)
			}
		}
	}

	if rejectsRef != "" {
		if uErr := gc.UpsertDecisionNode(cmd.Context(), rejectsRef, rejectsRef); uErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph rejected Decision node upsert failed: %v\n", uErr)
		} else if eErr := gc.UpsertRejectsEdge(cmd.Context(), dec.ID, rejectsRef); eErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ Memgraph REJECTS edge failed: %v\n", eErr)
		} else {
			fmt.Fprintf(cmd.ErrOrStderr(), "  Graph: (Decision)-[:REJECTS]->(Decision %s)\n", rejectsRef)
		}
	}
}
