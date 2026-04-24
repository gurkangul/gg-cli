package cmd

// dashboard.go — gg dashboard --watch TUI (TASK-276 AC3/AC4).
//
// Renders a live-updating table of active worker panes. Refreshes every 2s.
// Color:
//   green   = working
//   yellow  = idle >5m
//   red     = waiting-on-master (need-input)
//   magenta = collision-risk (another task claims overlapping paths)
//
// GG_BELL=1 rings the terminal bell when a worker transitions to
// waiting-on-master. Unread count is surfaced in the header.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/orchestrator/locks"
	"github.com/gurkangul/gg-cli/internal/orchestrator/spawn"
)

// ANSI color codes.
const (
	ansiReset   = "\033[0m"
	ansiGreen   = "\033[32m"
	ansiYellow  = "\033[33m"
	ansiRed     = "\033[31m"
	ansiMagenta = "\033[35m"
	ansiCyan    = "\033[36m"
	ansiBold    = "\033[1m"
	ansiClear   = "\033[2J\033[H" // clear screen + move cursor to top
)

// idleThreshold is how long without a heartbeat update before state → idle.
const idleThreshold = 5 * time.Minute

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Live worker-pane dashboard (TASK-276 AC3)",
	Long: `Watch active worker panes in real time.

Columns: pane-id | agent | task | state | elapsed | last-heartbeat | touched-files

Colors:
  green   — working
  yellow  — idle >5m (no heartbeat)
  red     — waiting-on-master (need-input signal)
  magenta — collision-risk (path overlap with another active task)

GG_BELL=1  — ring the terminal bell when a worker enters waiting-on-master.

Press Ctrl-C to exit.`,
	RunE: runDashboard,
}

var dashboardWatch bool
var dashboardInterval int

func init() {
	dashboardCmd.Flags().BoolVarP(&dashboardWatch, "watch", "w", false, "refresh every N seconds (default 2)")
	dashboardCmd.Flags().IntVar(&dashboardInterval, "interval", 2, "refresh interval in seconds (requires --watch)")
	rootCmd.AddCommand(dashboardCmd)
}

// workerDisplayState tracks per-pane state for bell/transition detection.
type workerDisplayState struct {
	prevState spawn.WorkerState
	bellFired bool
}

func runDashboard(cmd *cobra.Command, _ []string) error {
	rt, err := spawnRuntimeDir()
	if err != nil {
		return err
	}

	bellEnabled := os.Getenv("GG_BELL") == "1"
	interval := time.Duration(dashboardInterval) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}

	// If --watch is not set, render once and exit.
	if !dashboardWatch {
		return renderDashboard(cmd, rt, nil, bellEnabled)
	}

	// Watch loop.
	paneStates := make(map[string]*workerDisplayState)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// First render immediately.
	if err := renderDashboard(cmd, rt, paneStates, bellEnabled); err != nil {
		return err
	}

	for {
		select {
		case <-cmd.Context().Done():
			fmt.Println("\nDashboard stopped.")
			return nil
		case <-ticker.C:
			if err := renderDashboard(cmd, rt, paneStates, bellEnabled); err != nil {
				return err
			}
		}
	}
}

// renderDashboard prints one frame of the dashboard.
// paneStates tracks state transitions across frames (nil = one-shot render).
func renderDashboard(_ *cobra.Command, rt string, paneStates map[string]*workerDisplayState, bellEnabled bool) error {
	panes, err := spawn.ListPanes(rt)
	if err != nil {
		return fmt.Errorf("read panes: %w", err)
	}

	hb, _ := spawn.ReadHeartbeat(rt)
	sess, _ := spawn.ReadQueue(rt)

	// Load advisory claims to detect collision-risk.
	lk := locks.New(spawn.Dir(rt))
	claims, _ := lk.ReadAll()
	claimedPaths := buildClaimedPathIndex(claims)

	// Track waiting count for unread summary.
	waitingCount := 0

	// For each pane, check if state transitions to waiting-on-master → bell.
	for i := range panes {
		p := &panes[i]
		effectiveState := effectivePaneState(p)
		if effectiveState == spawn.WorkerStateWaiting {
			waitingCount++
		}

		if paneStates != nil {
			prev, seen := paneStates[p.SurfaceID]
			if !seen {
				prev = &workerDisplayState{}
				paneStates[p.SurfaceID] = prev
			}
			if bellEnabled && !prev.bellFired && effectiveState == spawn.WorkerStateWaiting && prev.prevState != spawn.WorkerStateWaiting {
				fmt.Print("\a") // terminal bell
				prev.bellFired = true
			}
			if effectiveState != spawn.WorkerStateWaiting {
				prev.bellFired = false // reset so next transition rings again
			}
			prev.prevState = effectiveState
		}
	}

	if paneStates != nil {
		fmt.Print(ansiClear)
	}

	now := time.Now()
	fmt.Printf("%s%s gg dashboard%s  %s\n", ansiBold, ansiCyan, ansiReset, now.Format("15:04:05"))

	// Header: master liveness.
	if hb != nil {
		age := now.Sub(hb.UpdatedAt).Round(time.Second)
		icon := ansiGreen + "●" + ansiReset
		if age > spawn.StaleDuration {
			icon = ansiRed + "●" + ansiReset
		}
		fmt.Printf("Master %s  agent: %s  heartbeat: %s ago\n", icon, hb.Agent, age)
	} else {
		fmt.Printf("Master %s●%s  (no heartbeat)\n", ansiRed, ansiReset)
	}

	// Queue summary.
	if sess != nil {
		pausedNote := ""
		if sess.Paused {
			pausedNote = " [PAUSED]"
		}
		fmt.Printf("Queue%s  cap: %d  completed: %d  skipped: %d",
			pausedNote, maxConcurrent(), len(sess.Completed), len(sess.Skipped))
		if waitingCount > 0 {
			fmt.Printf("  %s⚑ %d waiting-on-master%s", ansiRed, waitingCount, ansiReset)
		}
		fmt.Println()
	}

	fmt.Println(strings.Repeat("─", 100))

	if len(panes) == 0 {
		fmt.Println("(no active workers)")
		return nil
	}

	// Column header.
	fmt.Printf("%s%-18s %-12s %-12s %-18s %-9s %-12s %s%s\n",
		ansiBold,
		"PANE-ID", "AGENT", "TASK", "STATE", "ELAPSED", "HEARTBEAT", "TOUCHED-FILES",
		ansiReset)
	fmt.Println(strings.Repeat("─", 100))

	for i := range panes {
		p := &panes[i]
		effectiveState := effectivePaneState(p)
		color := stateColor(effectiveState)

		// Detect collision-risk: does this pane's task have path overlap with another?
		if hasCollisionRisk(p.TaskID, claims) {
			color = ansiMagenta
		}

		elapsed := now.Sub(p.SpawnedAt).Round(time.Second)
		hbAge := "-"
		if !p.LastHeartbeat.IsZero() {
			hbAge = now.Sub(p.LastHeartbeat).Round(time.Second).String()
		}

		touchedSummary := touchedFilesSummary(p.TaskID, claimedPaths)

		paneShort := dashTruncate(string(p.SurfaceID), 17)
		agent := dashTruncate(p.Agent, 11)
		task := dashTruncate(p.TaskID, 11)
		stateStr := dashTruncate(string(effectiveState), 17)

		fmt.Printf("%s%-18s %-12s %-12s %-18s %-9s %-12s %s%s\n",
			color,
			paneShort, agent, task, stateStr, elapsed, hbAge, touchedSummary,
			ansiReset)
	}

	return nil
}

// effectivePaneState returns the effective state, auto-promoting working→idle
// when the last heartbeat is older than idleThreshold.
func effectivePaneState(p *spawn.WorkerPane) spawn.WorkerState {
	if p.State == spawn.WorkerStateWaiting {
		return spawn.WorkerStateWaiting
	}
	// Promote to idle if no heartbeat or heartbeat is stale.
	if p.LastHeartbeat.IsZero() || time.Since(p.LastHeartbeat) > idleThreshold {
		// Only auto-idle if no explicit state is set or it was working.
		if p.State == "" || p.State == spawn.WorkerStateWorking {
			return spawn.WorkerStateIdle
		}
	}
	if p.State == "" {
		return spawn.WorkerStateWorking
	}
	return p.State
}

// stateColor maps worker state to an ANSI color prefix.
func stateColor(s spawn.WorkerState) string {
	switch s {
	case spawn.WorkerStateWorking:
		return ansiGreen
	case spawn.WorkerStateIdle:
		return ansiYellow
	case spawn.WorkerStateWaiting:
		return ansiRed
	default:
		return ""
	}
}

// buildClaimedPathIndex returns a map from path → list of task IDs that claim it.
func buildClaimedPathIndex(claims []locks.Claim) map[string][]string {
	idx := make(map[string][]string, len(claims)*4)
	for _, c := range claims {
		for _, p := range c.Paths {
			idx[p] = append(idx[p], c.TaskID)
		}
	}
	return idx
}

// hasCollisionRisk returns true when taskID's claimed paths overlap with
// any other active task's claimed paths.
func hasCollisionRisk(taskID string, claims []locks.Claim) bool {
	var myPaths []string
	for _, c := range claims {
		if c.TaskID == taskID {
			myPaths = c.Paths
			break
		}
	}
	if len(myPaths) == 0 {
		return false
	}
	mySet := make(map[string]bool, len(myPaths))
	for _, p := range myPaths {
		mySet[p] = true
	}
	for _, c := range claims {
		if c.TaskID == taskID {
			continue
		}
		for _, p := range c.Paths {
			if mySet[p] {
				return true
			}
		}
	}
	return false
}

// touchedFilesSummary returns a compact summary of paths claimed by taskID.
func touchedFilesSummary(taskID string, idx map[string][]string) string {
	var mine []string
	for path, holders := range idx {
		for _, h := range holders {
			if h == taskID {
				mine = append(mine, path)
				break
			}
		}
	}
	if len(mine) == 0 {
		return "-"
	}
	if len(mine) == 1 {
		return dashTruncate(mine[0], 30)
	}
	return fmt.Sprintf("%s +%d", dashTruncate(mine[0], 20), len(mine)-1)
}

// dashTruncate shortens s to max runes, appending "…" if trimmed.
func dashTruncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
