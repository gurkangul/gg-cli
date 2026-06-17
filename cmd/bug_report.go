package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/identity"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
)

var bugReportCmd = &cobra.Command{
	Use:   `report "title"`,
	Short: "Report a new bug",
	Args:  cobra.ExactArgs(1),
	RunE:  runBugReport,
}

var (
	bugDetail   string
	bugSeverity string
	bugTags     string
	bugTaskID   string
	bugFiles    string
	bugSymbols  string
	bugForce    bool
)

func init() {
	bugReportCmd.Flags().StringVar(&bugDetail, "detail", "", "detailed description")
	bugReportCmd.Flags().StringVar(&bugSeverity, "severity", "medium", "severity: critical, high, medium, low")
	bugReportCmd.Flags().StringVar(&bugTags, "tags", "", "comma-separated tags")
	bugReportCmd.Flags().StringVar(&bugTaskID, "task", "", "link to a task (e.g. TASK-042)")
	bugReportCmd.Flags().StringVar(&bugFiles, "files", "", "comma-separated source file paths this bug affects")
	bugReportCmd.Flags().StringVar(&bugSymbols, "symbols", "", "comma-separated symbol names this bug affects")
	bugReportCmd.Flags().BoolVar(&bugForce, "force", false, "skip duplicate-detection prompt and file anyway")
	addFromFlag(bugReportCmd)
	bugCmd.AddCommand(bugReportCmd)
}

// normalizeBugFiles converts raw --files paths to project-relative form.
// Absolute paths are rebased onto the project root; relative paths that escape
// the root are dropped. Paths that can't be normalized (root detection failed)
// are passed through unchanged so the caller still gets useful output.
func normalizeBugFiles(paths []string) []string {
	root, err := config.FindRoot()
	if err != nil {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, ok := normalizeProjectPath(root, "", p); ok {
			out = append(out, rel)
		}
		// paths outside the project root are silently dropped
	}
	return out
}

func runBugReport(cmd *cobra.Command, args []string) error {
	printProjectBanner()
	title, err := requireNonEmpty("title", args[0])
	if err != nil {
		return err
	}
	if !validSeverities[bugSeverity] {
		return fmt.Errorf("invalid severity %q — use critical, high, medium, or low", bugSeverity)
	}
	taskID, err := normalizeTaskRef(bugTaskID)
	if err != nil {
		return err
	}

	if err := requireAgentIdentity(); err != nil {
		return err
	}

	// AC-2: Use offline-safe loader so bug report survives Qdrant downtime.
	d, err := loadDepsOfflineSafe(true)
	if err != nil {
		return err
	}
	defer d.Close()

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	// AC-4: InboxGatePreflight fails-open when Qdrant is unreachable.
	if err := runInboxGatePreflight(ctx, d.store, "bug-report"); err != nil {
		return err
	}

	var vector []float32
	tags := parseTags(bugTags)
	if !d.qdrantDown {
		embedText := title
		if bugDetail != "" {
			embedText = title + " " + bugDetail
		}
		vector, err = d.embedder.Generate(ctx, embedText)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ embedding unavailable — JSONL write will continue and semantic indexing will be queued: %v\n", err)
		} else if !bugForce {
			fromFlag := ""
			if f := cmd.Flags().Lookup("from"); f != nil {
				fromFlag = f.Value.String()
			}
			threshold := float32(dupThreshold)
			if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil && cfg.Bugs.DupeThreshold > 0 {
				threshold = float32(cfg.Bugs.DupeThreshold)
			}
			res := promptIfDuplicateThreshold(ctx, d, "bugs", vector, threshold)

			if res.UserChoice != "" {
				if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil {
					if rtDir, rtErr := cfg.RuntimeDir(); rtErr == nil {
						telemetry.RecordDupeCheck(rtDir, "bug-report", fromFlag,
							res.MatchesCount, res.TopScore, res.UserChoice)
					}
				}
			}

			if res.Abort {
				if res.ReuseAsBugID != "" {
					if err := addDupeNote(ctx, d, res.ReuseAsBugID, title, vector); err != nil {
						fmt.Fprintf(os.Stderr, "warning: add note failed: %v\n", err)
					} else {
						fmt.Printf("Note added to %s — no duplicate bug created.\n", res.ReuseAsBugID)
					}
				} else {
					fmt.Println("Aborted — no bug reported.")
				}
				return nil
			}
			if res.SawDup {
				tags = appendUniq(tags, "dupe-acknowledged")
			}
		}
	} else {
		fmt.Fprintln(cmd.ErrOrStderr(), "⚠ vector store unavailable — read served from JSONL (may miss cross-project context)")
	}

	affectedFiles := normalizeBugFiles(parseTags(bugFiles))
	affectedSymbols := parseTags(bugSymbols)

	b := store.Bug{
		Title:           title,
		Detail:          strings.TrimSpace(bugDetail),
		Severity:        bugSeverity,
		Tags:            tags,
		TaskID:          taskID,
		AffectedFiles:   affectedFiles,
		AffectedSymbols: affectedSymbols,
		By:              resolveAuthor(cmd),
	}

	id, reportErr := d.store.ReportBug(ctx, b, vector)
	if reportErr != nil {
		var oq *store.OutboxQueued
		if errors.As(reportErr, &oq) {
			queueBrainOutbox(oq, config.GGDirOrEmpty())
			warnBrainOutboxQueued(cmd.ErrOrStderr(), oq.Cause)
			if isSandboxPermissionError(oq.Cause) {
				fmt.Fprintln(cmd.ErrOrStderr(), "⚠ sandbox EPERM detected — outbox entry queued, but the agent should rerun outside sandbox to avoid drift")
			}
		} else {
			return fmt.Errorf("report bug: %w", reportErr)
		}
	}

	// Write Bug node + AFFECTS edges to the code graph. Always upsert the Bug
	// node, even when no affected files/symbols were given — otherwise
	// `gg impact BUG-NNN` cannot find the bug and projects end up with 0 Bug
	// nodes in the code graph despite a populated vector store.
	if cfg, cfgErr := config.Load(); cfgErr == nil && cfg != nil {
		if gc, gcErr := graph.New(cfg.DataDir, cfg.ProjectID); gcErr == nil {
			gctx, gcancel := withTimeout(cmd.Context())
			defer gcancel()
			if mergeErr := gc.MergeBugAffects(gctx, id, title, affectedFiles, affectedSymbols); mergeErr != nil {
				fmt.Printf("~ graph edges skipped: %v\n", mergeErr)
			}
			_ = gc.Close(gctx)
		}
	}

	return printJSON(map[string]any{"id": id, "title": title, "severity": bugSeverity}, func() {
		fmt.Printf("bug reported: %s — %s [%s]\n", id, title, bugSeverity)
	})
}

// addDupeNote adds a note to the store linking the given title/vector to an
// existing bug (reuse-as-note flow). The note is tagged with the bug ID and
// "dupe-of" so it surfaces in semantic searches near the original bug.
func addDupeNote(ctx context.Context, d *deps, bugID, title string, vector []float32) error {
	n := store.Note{
		Text:   title,
		Tags:   []string{bugID, "dupe-of"},
		Author: identity.Agent(),
	}
	_, err := d.store.AddNote(ctx, n, vector)
	var oq *store.OutboxQueued
	if errors.As(err, &oq) {
		queueBrainOutbox(oq, config.GGDirOrEmpty())
		warnBrainOutboxQueued(os.Stderr, oq.Cause)
		return nil
	}
	return err
}
