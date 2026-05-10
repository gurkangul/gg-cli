package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/store"
	"github.com/gurkangul/gg-cli/internal/telemetry"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Tail inbox messages and event stream",
	Long: `Stream new inbox messages and telemetry events to stdout.

Designed as a hook source for external pipes: terminal status bars, desktop
notification scripts, or other agents polling for activity.

Does not require a long-running server — reads the existing NDJSON telemetry
file and polls the inbox DB at ~1 second cadence. Safe to Ctrl-C at any time;
messages are not marked as read.

Examples:
  gg watch                             # stream everything, pretty format
  gg watch --role qa                   # only messages addressed to 'qa'
  gg watch --event tell                # only 'tell' telemetry events
  gg watch --since 5m                  # replay last 5 min, then tail
  gg watch --format ndjson             # machine-readable NDJSON output
  gg watch --no-inbox                  # telemetry events only
  gg watch --no-telemetry              # inbox messages only
  gg watch --index                     # foreground index watcher (alias for gg index --watch)`,
	RunE: runWatch,
}

var (
	watchRole        string
	watchEvent       string
	watchTag         string
	watchSince       string
	watchFormat      string
	watchNoInbox     bool
	watchNoTelemetry bool
	watchIndex       bool
)

func init() {
	watchCmd.Flags().StringVar(&watchRole, "role", "", "filter inbox messages by recipient role")
	watchCmd.Flags().StringVar(&watchEvent, "event", "", "filter telemetry events by verb (e.g. tell, task)")
	watchCmd.Flags().StringVar(&watchTag, "tag", "", "filter messages whose content contains this tag/string")
	watchCmd.Flags().StringVar(&watchSince, "since", "", "replay events from this duration back on startup (e.g. 5m, 2h)")
	watchCmd.Flags().StringVar(&watchFormat, "format", "pretty", "output format: pretty or ndjson")
	watchCmd.Flags().BoolVar(&watchNoInbox, "no-inbox", false, "skip inbox messages, show telemetry events only")
	watchCmd.Flags().BoolVar(&watchNoTelemetry, "no-telemetry", false, "skip telemetry events, show inbox messages only")
	watchCmd.Flags().BoolVar(&watchIndex, "index", false, "foreground index watcher alias for gg index --watch")
	rootCmd.AddCommand(watchCmd)
}

// watchEvent is the unified event emitted on stdout.
type watchItem struct {
	Type    string `json:"type"`              // "event" or "message"
	Verb    string `json:"verb,omitempty"`    // telemetry verb (event only)
	Origin  string `json:"origin,omitempty"`  // "agent" or "human" (event only)
	From    string `json:"from,omitempty"`    // sender role (message only)
	To      string `json:"to,omitempty"`      // recipient role (message only)
	Content string `json:"content,omitempty"` // message content (message only)
	TaskID  string `json:"task,omitempty"`    // related task (message only)
	TS      string `json:"ts"`                // RFC3339
}

func runWatch(cmd *cobra.Command, _ []string) error {
	if watchIndex {
		indexWatch = true
		return runIndexWatch(cmd)
	}
	if watchFormat != "pretty" && watchFormat != "ndjson" {
		return fmt.Errorf("--format must be 'pretty' or 'ndjson'")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Determine lookback window for startup replay.
	var sinceTime time.Time
	if watchSince != "" {
		dur, parseErr := parseDuration(watchSince)
		if parseErr != nil {
			return fmt.Errorf("--since: %w", parseErr)
		}
		sinceTime = time.Now().UTC().Add(-dur)
	}

	// --- telemetry file setup ---
	var telemetryOffset int64
	var telemetryPath string
	if !watchNoTelemetry {
		runtimeDir, rdErr := cfg.RuntimeDir()
		if rdErr == nil {
			telemetryPath = runtimeDir + "/telemetry.jsonl"
			telemetryOffset, err = seekTelemetryOffset(telemetryPath, sinceTime)
			if err != nil {
				// Non-fatal — file may not exist yet.
				telemetryOffset = 0
			}
		}
	}

	// --- inbox setup ---
	var inboxClient *store.Client
	seenMessageIDs := make(map[string]bool)
	if !watchNoInbox {
		ggDir, _ := config.GGDir()
		ic, icErr := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
		if icErr == nil {
			// Pre-seed seen IDs from the lookback window so we only emit truly new messages.
			seedCtx, seedCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			existing, seedErr := ic.GetInbox(seedCtx, watchRole, false)
			seedCancel()
			if seedErr == nil {
				for _, m := range existing {
					if sinceTime.IsZero() {
						// No --since: seed all existing as seen so we only show future messages.
						seenMessageIDs[m.ID] = true
					} else {
						// With --since: seed only those older than the lookback window.
						ts, tsErr := time.Parse(time.RFC3339, m.CreatedAt)
						if tsErr == nil && ts.Before(sinceTime) {
							seenMessageIDs[m.ID] = true
						}
					}
				}
			}
			inboxClient = ic
		}
	}
	defer func() {
		if inboxClient != nil {
			_ = inboxClient.Close()
		}
	}()

	if watchFormat == "pretty" {
		fmt.Fprintln(os.Stderr, "gg watch — streaming (Ctrl-C to stop)")
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return nil
		case <-ticker.C:
			if !watchNoTelemetry && telemetryPath != "" {
				telemetryOffset = pollTelemetryFile(cmd.OutOrStdout(), telemetryPath, telemetryOffset, watchEvent)
			}
			if !watchNoInbox && inboxClient != nil {
				pollCtx, pollCancel := context.WithTimeout(cmd.Context(), 5*time.Second)
				msgs, pollErr := inboxClient.GetInbox(pollCtx, watchRole, false)
				pollCancel()
				if pollErr == nil {
					for _, m := range msgs {
						if seenMessageIDs[m.ID] {
							continue
						}
						seenMessageIDs[m.ID] = true
						if watchTag != "" && !strings.Contains(m.Content, watchTag) {
							continue
						}
						item := watchItem{
							Type:    "message",
							From:    m.FromRole,
							To:      m.ToRole,
							Content: m.Content,
							TaskID:  m.TaskID,
							TS:      m.CreatedAt,
						}
						emitWatchItem(cmd.OutOrStdout(), item, watchFormat)
					}
				}
			}
		}
	}
}

// seekTelemetryOffset opens the telemetry file and finds the byte offset for
// the first entry at or after sinceTime. If sinceTime is zero, it returns the
// current EOF so we only tail new entries.
func seekTelemetryOffset(path string, sinceTime time.Time) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	if sinceTime.IsZero() {
		// Start at EOF — tail mode.
		return f.Seek(0, io.SeekEnd)
	}

	// Scan for the first entry at or after sinceTime, return its byte offset.
	scanner := bufio.NewScanner(f)
	var pos int64
	for scanner.Scan() {
		line := scanner.Text()
		var e telemetry.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			pos += int64(len(line)) + 1
			continue
		}
		ts, tsErr := time.Parse(time.RFC3339, e.Timestamp)
		if tsErr == nil && !ts.Before(sinceTime) {
			return pos, nil // replay from here
		}
		pos += int64(len(line)) + 1
	}
	// All entries are older than sinceTime — start at EOF.
	return pos, nil
}

// pollTelemetryFile reads any new lines appended to the telemetry file since
// offset, emits matched events, and returns the new offset.
func pollTelemetryFile(w io.Writer, path string, offset int64, eventFilter string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return offset
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset
	}

	scanner := bufio.NewScanner(f)
	newOffset := offset
	for scanner.Scan() {
		line := scanner.Text()
		newOffset += int64(len(line)) + 1
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e telemetry.Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if eventFilter != "" && e.Verb != eventFilter {
			continue
		}
		item := watchItem{
			Type:   "event",
			Verb:   e.Verb,
			Origin: e.Origin,
			TS:     e.Timestamp,
		}
		emitWatchItem(w, item, watchFormat)
	}
	return newOffset
}

func emitWatchItem(w io.Writer, item watchItem, format string) {
	if format == "ndjson" {
		b, err := json.Marshal(item)
		if err != nil {
			return
		}
		fmt.Fprintln(w, string(b))
		return
	}

	// Pretty format.
	ts, err := time.Parse(time.RFC3339, item.TS)
	if err != nil {
		ts = time.Now()
	}
	clock := ts.Local().Format("15:04:05")

	switch item.Type {
	case "event":
		fmt.Fprintf(w, "%s [event] %s (%s)\n", clock, item.Verb, item.Origin)
	case "message":
		line := fmt.Sprintf("%s [msg] %s → %s: %s", clock, item.From, item.To, highlightMentions(item.Content))
		if item.TaskID != "" {
			line += " [" + item.TaskID + "]"
		}
		fmt.Fprintln(w, line)
	}
}
