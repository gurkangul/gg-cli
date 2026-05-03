package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
	"github.com/gurkangul/gg-cli/internal/orchestrator/terminal"
	"github.com/gurkangul/gg-cli/internal/store"
)

var spawnSupervisorCmd = &cobra.Command{
	Use:   "supervisor",
	Short: "Route gg messages to live worker panes",
	Long: `Run an explicit foreground dispatcher that watches inbox messages and
forwards matching role-targeted instructions into worker panes.

No daemon is created. The command only runs while this foreground process is
active and stops immediately on Ctrl-C.`,
}

var spawnSupervisorWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch inbox and trigger matching worker panes",
	RunE:  runSpawnSupervisorWatch,
}

var (
	spawnSupervisorRole        string
	spawnSupervisorPollSecs    int
	spawnSupervisorOpenMissing bool
)

func init() {
	spawnSupervisorWatchCmd.Flags().StringVar(&spawnSupervisorRole, "role", "", "worker role to consume (default: $GG_ROLE)")
	spawnSupervisorWatchCmd.Flags().IntVar(&spawnSupervisorPollSecs, "poll", 2, "seconds between inbox polls")
	spawnSupervisorWatchCmd.Flags().BoolVar(&spawnSupervisorOpenMissing, "open-missing", false, "when pane missing, open a new worker pane if task is specified")
	spawnSupervisorCmd.AddCommand(spawnSupervisorWatchCmd)
	spawnCmd.AddCommand(spawnSupervisorCmd)
}

type supervisorState struct {
	Processed map[string]bool `json:"processed"`
}

func safeSupervisorRoleFile(role string) string {
	raw := strings.TrimSpace(strings.ToLower(role))
	if raw == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune('_')
	}
	s := strings.Trim(b.String(), "_")
	if s == "" {
		return "default"
	}
	return s
}

func supervisorStatePath(runtimeDir, role string) string {
	safeRole := safeSupervisorRoleFile(role)
	return filepath.Join(spawn.Dir(runtimeDir), "supervisor", safeRole+".json")
}

func loadSupervisorState(runtimeDir, role string) (*supervisorState, error) {
	path := supervisorStatePath(runtimeDir, role)
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &supervisorState{Processed: map[string]bool{}}, nil
		}
		return nil, err
	}
	var s supervisorState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("parse supervisor state: %w", err)
	}
	if s.Processed == nil {
		s.Processed = map[string]bool{}
	}
	return &s, nil
}

func saveSupervisorState(runtimeDir, role string, s *supervisorState) error {
	path := supervisorStatePath(runtimeDir, role)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// #nosec G703 -- path is constrained under spawn.Dir(runtimeDir)/supervisor and role is sanitized by safeSupervisorRoleFile; covered by TestSupervisorStatePath_SanitizesRole.
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func runSpawnSupervisorWatch(cmd *cobra.Command, _ []string) error {
	role := strings.TrimSpace(spawnSupervisorRole)
	if role == "" {
		role = strings.TrimSpace(os.Getenv("GG_ROLE"))
	}
	if role == "" {
		return fmt.Errorf("role required: pass --role or set GG_ROLE")
	}
	if spawnSupervisorPollSecs <= 0 {
		return fmt.Errorf("--poll must be > 0")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	runtimeDir, err := cfg.RuntimeDir()
	if err != nil {
		return err
	}

	state, err := loadSupervisorState(runtimeDir, role)
	if err != nil {
		return err
	}

	ggDir, _ := config.GGDir()
	inboxClient, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("inbox store: %w", err)
	}
	defer func() { _ = inboxClient.Close() }()

	term, err := terminal.NewFromEnv()
	if err != nil {
		return fmt.Errorf("terminal backend: %w", err)
	}

	pollCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	startupMsgs, startupErr := supervisorInboxMessages(pollCtx, inboxClient, role)
	cancel()
	if startupErr != nil {
		return fmt.Errorf("startup inbox poll: %w", startupErr)
	}
	seedSupervisorProcessed(state, startupMsgs)
	if err := saveSupervisorState(runtimeDir, role, state); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ supervisor state save: %v\n", err)
	}

	ticker := time.NewTicker(time.Duration(spawnSupervisorPollSecs) * time.Second)
	defer ticker.Stop()
	fmt.Fprintf(cmd.ErrOrStderr(), "→ spawn supervisor watch active for role=%s (poll=%ds)\n", role, spawnSupervisorPollSecs)

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			pollCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			msgs, pollErr := supervisorInboxMessages(pollCtx, inboxClient, role)
			cancel()
			if pollErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ supervisor inbox poll: %v\n", pollErr)
				continue
			}
			processSupervisorMessages(cmd.Context(), cmd, runtimeDir, term, role, state, msgs, spawnSupervisorOpenMissing)
			if err := saveSupervisorState(runtimeDir, role, state); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ supervisor state save: %v\n", err)
			}
		}
	}
}

type supervisorInboxReader interface {
	GetInbox(ctx context.Context, role string, humanOnly bool) ([]store.Message, error)
}

func supervisorInboxMessages(ctx context.Context, inbox supervisorInboxReader, role string) ([]store.Message, error) {
	roleMsgs, err := inbox.GetInbox(ctx, role, false)
	if err != nil {
		return nil, err
	}
	allMsgs, err := inbox.GetInbox(ctx, "all", false)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(roleMsgs)+len(allMsgs))
	msgs := make([]store.Message, 0, len(roleMsgs)+len(allMsgs))
	for _, m := range append(roleMsgs, allMsgs...) {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func processSupervisorMessages(ctx context.Context, cmd *cobra.Command, runtimeDir string, term terminal.Terminal, role string, state *supervisorState, msgs []store.Message, openMissing bool) {
	for _, m := range msgs {
		if state.Processed[m.ID] {
			continue
		}
		if !eligibleForSupervisor(role, m) {
			state.Processed[m.ID] = true
			continue
		}
		if deliverSupervisorMessage(ctx, cmd, runtimeDir, term, m, openMissing) {
			state.Processed[m.ID] = true
		}
	}
}

func seedSupervisorProcessed(state *supervisorState, msgs []store.Message) {
	if state.Processed == nil {
		state.Processed = map[string]bool{}
	}
	for _, m := range msgs {
		state.Processed[m.ID] = true
	}
}

func eligibleForSupervisor(role string, m store.Message) bool {
	to := strings.TrimSpace(strings.ToLower(m.ToRole))
	if to == strings.TrimSpace(strings.ToLower(role)) {
		return true
	}
	if to != "all" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Audience), "agents") || strings.TrimSpace(m.Audience) == ""
}

func deliverSupervisorMessage(ctx context.Context, cmd *cobra.Command, runtimeDir string, term terminal.Terminal, m store.Message, openMissing bool) bool {
	pane, err := paneForMessage(runtimeDir, m)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ supervisor lookup message %s: %v\n", m.ID, err)
		return false
	}
	if pane == nil {
		if openMissing && strings.TrimSpace(m.TaskID) != "" {
			sid, spawnErr := spawnWorkerForTask(ctx, term, runtimeDir, spawnAgentDefault(), m.TaskID)
			if spawnErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ missing pane for %s (%s): auto-open failed: %v\n", m.TaskID, m.ID, spawnErr)
				return false
			}
			pane = &spawn.WorkerPane{TaskID: m.TaskID, SurfaceID: string(sid)}
			fmt.Fprintf(cmd.ErrOrStderr(), "✓ opened pane %s for %s (message %s)\n", pane.SurfaceID, pane.TaskID, m.ID)
		} else {
			if strings.TrimSpace(m.TaskID) == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "⚠ no worker pane registered for message %s — open a worker with: gg spawn worker --task TASK-NNN\n", m.ID)
				return true
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ missing pane for task %s (message %s) — open with: gg spawn worker --task %s\n", m.TaskID, m.ID, m.TaskID)
			return true
		}
	}

	surfaceID := terminal.SurfaceID(pane.SurfaceID)
	if err := terminal.WakeAndSendWithFlock(ctx, term, surfaceID, m.Content, spawn.Dir(runtimeDir)); err != nil {
		if isSurfaceDefinitelyDead(ctx, surfaceID, err) {
			pruneStalePane(runtimeDir, *pane)
			fmt.Fprintf(cmd.ErrOrStderr(), "⚠ stale pane %s for %s (message %s) — pruned; reopen with gg spawn worker --task %s\n", pane.SurfaceID, pane.TaskID, m.ID, pane.TaskID)
			return true
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "⚠ supervisor delivery failed for %s on %s: %v\n", m.ID, pane.SurfaceID, err)
		return false
	}

	_ = spawn.UpdateWorkerNudge(runtimeDir, pane.SurfaceID, m.Content, "")
	fmt.Fprintf(cmd.OutOrStdout(), "✓ supervisor delivered %s to %s (%s)\n", m.ID, pane.TaskID, pane.SurfaceID)
	return true
}

func paneForMessage(runtimeDir string, m store.Message) (*spawn.WorkerPane, error) {
	if strings.TrimSpace(m.TaskID) != "" {
		return spawn.FindPaneForTask(runtimeDir, m.TaskID)
	}
	panes, err := spawn.ListPanes(runtimeDir)
	if err != nil {
		return nil, err
	}
	if len(panes) == 0 {
		return nil, nil
	}
	p := panes[0]
	return &p, nil
}
