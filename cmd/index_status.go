package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/graph"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/state"
	"github.com/gurkangul/gg-cli/internal/outbox"
)

var indexStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show code graph freshness and quality",
	RunE:  runIndexStatus,
}

func init() {
	indexCmd.AddCommand(indexStatusCmd)
}

type codeGraphStatus struct {
	Status            string      `json:"status"`
	Detail            string      `json:"detail,omitempty"`
	LastIndexedSHA    string      `json:"last_indexed_sha,omitempty"`
	HeadSHA           string      `json:"head_sha,omitempty"`
	IndexedAt         string      `json:"indexed_at,omitempty"`
	MemgraphAvailable bool        `json:"memgraph_available"`
	MemgraphDetail    string      `json:"memgraph_detail,omitempty"`
	GraphEmpty        bool        `json:"graph_empty"`
	Stats             graph.Stats `json:"stats"`
	PendingOutbox     int         `json:"pending_outbox"`
	NoWatcherStarted  bool        `json:"no_watcher_started"`
}

func runIndexStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return configErr(fmt.Sprintf("load config: %v", err))
	}
	root, err := config.FindRoot()
	if err != nil {
		return configErr(err.Error())
	}
	ggDir := root + "/.gg"

	ctx, cancel := withTimeout(cmd.Context())
	defer cancel()

	status := collectCodeGraphStatus(ctx, root, ggDir, cfg)
	return printJSON(status, func() {
		renderCodeGraphStatus(cmd.OutOrStdout(), status)
	})
}

func collectCodeGraphStatus(ctx context.Context, root, ggDir string, cfg *config.Config) codeGraphStatus {
	status := codeGraphStatus{
		Status:           "unknown",
		NoWatcherStarted: true,
	}

	if entries, err := outbox.List(ggDir); err == nil {
		status.PendingOutbox = len(entries)
	}

	if head, err := changed.HeadSHA(ctx, root); err == nil {
		status.HeadSHA = head
	} else {
		status.Detail = "HEAD unavailable: " + err.Error()
	}

	if s, err := state.Read(ggDir); err == nil {
		status.LastIndexedSHA = s.LastIndexedSHA
		status.IndexedAt = s.IndexedAt
	} else if errors.Is(err, state.ErrNoState) {
		status.Status = "missing"
		status.Detail = "index-state.json missing - run gg index --lang <lang>"
	} else {
		status.Status = "unknown"
		status.Detail = "index-state unreadable: " + err.Error()
	}

	status.fillGitFreshness(ctx, root)
	status.fillGraphStats(ctx, cfg)
	status.finalize()
	return status
}

func (s *codeGraphStatus) fillGitFreshness(ctx context.Context, root string) {
	if s.LastIndexedSHA == "" || s.HeadSHA == "" {
		return
	}
	if s.LastIndexedSHA == s.HeadSHA {
		s.Status = "ready"
		s.Detail = "index-state matches HEAD"
		return
	}
	ancestor, err := changed.IsAncestor(ctx, root, s.LastIndexedSHA)
	if err != nil {
		s.Status = "unknown"
		s.Detail = "ancestor check failed: " + err.Error()
		return
	}
	if !ancestor {
		s.Status = "non_ancestor"
		s.Detail = "last indexed SHA is not an ancestor of HEAD - run full gg index"
		return
	}
	s.Status = "stale"
	s.Detail = "HEAD has commits after the last indexed SHA - run gg index --changed"
}

func (s *codeGraphStatus) fillGraphStats(ctx context.Context, cfg *config.Config) {
	if cfg == nil || cfg.Memgraph.URI == "" {
		s.MemgraphDetail = "not configured"
		return
	}
	gc, err := graph.New(&cfg.Memgraph, cfg.ProjectID)
	if err != nil {
		s.MemgraphDetail = "client init: " + err.Error()
		return
	}
	defer func() { _ = gc.Close(ctx) }()

	if err := gc.HealthCheck(ctx); err != nil {
		s.MemgraphDetail = "unavailable: " + err.Error()
		return
	}
	s.MemgraphAvailable = true
	s.MemgraphDetail = "reachable"

	stats, err := gc.Stats(ctx)
	if err != nil {
		s.MemgraphDetail = "stats unavailable: " + err.Error()
		return
	}
	s.Stats = stats
	s.GraphEmpty = stats.Files == 0 && stats.Symbols == 0 && stats.Edges == 0
}

func (s *codeGraphStatus) finalize() {
	if s.PendingOutbox > 0 {
		s.Status = "partial"
		if s.Detail == "" {
			s.Detail = "pending index outbox entries - run gg doctor --reconcile"
		}
	}
	if s.MemgraphAvailable && s.GraphEmpty {
		s.Status = "missing"
		s.Detail = "Memgraph is reachable but graph is empty - run gg index --lang <lang>"
	}
	if !s.MemgraphAvailable && s.Detail == "" {
		s.Detail = "Memgraph unavailable or not configured"
	}
}

func renderCodeGraphStatus(w io.Writer, s codeGraphStatus) {
	fmt.Fprintln(w, "CODE GRAPH:")
	fmt.Fprintf(w, "  Status: %s", s.Status)
	if s.Detail != "" {
		fmt.Fprintf(w, " - %s", s.Detail)
	}
	fmt.Fprintln(w)
	if s.LastIndexedSHA != "" || s.HeadSHA != "" {
		fmt.Fprintf(w, "  SHA: indexed=%s head=%s\n", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA))
	}
	if s.IndexedAt != "" {
		fmt.Fprintf(w, "  Indexed: %s\n", s.IndexedAt)
	}
	fmt.Fprintf(w, "  Memgraph: %s", boolWord(s.MemgraphAvailable, "available", "unavailable"))
	if s.MemgraphDetail != "" {
		fmt.Fprintf(w, " (%s)", s.MemgraphDetail)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Counts: files=%d symbols=%d edges=%d\n", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges)
	if s.PendingOutbox > 0 {
		fmt.Fprintf(w, "  Outbox: %d pending index write(s)\n", s.PendingOutbox)
	}
	fmt.Fprintln(w, "  Watcher: not started implicitly")
}

func indexStatusShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

func boolWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func renderCodeGraphStatusCompact(s codeGraphStatus) string {
	var parts []string
	parts = append(parts, "CodeGraph "+s.Status)
	if s.LastIndexedSHA != "" || s.HeadSHA != "" {
		parts = append(parts, fmt.Sprintf("idx=%s head=%s", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA)))
	}
	parts = append(parts, fmt.Sprintf("memgraph=%s", boolWord(s.MemgraphAvailable, "ok", "down")))
	parts = append(parts, fmt.Sprintf("files=%d sym=%d edges=%d", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges))
	if s.PendingOutbox > 0 {
		parts = append(parts, fmt.Sprintf("outbox=%d", s.PendingOutbox))
	}
	if s.Detail != "" {
		parts = append(parts, compactTrim(s.Detail, 90))
	}
	return strings.Join(parts, "  ")
}

func codeGraphStatusWithTimeout(root, ggDir string, cfg *config.Config) codeGraphStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return collectCodeGraphStatus(ctx, root, ggDir, cfg)
}
