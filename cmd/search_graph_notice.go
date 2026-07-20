package cmd

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/state"
)

// search_graph_notice.go — the stale code-graph notice `gg search` emits on
// stderr (TASK-504), plus the cheap freshness gate that usually lets it skip
// the work entirely (TASK-509 / recovered as TASK-522).
//
// Collecting graph status is not free: it walks the filesystem, shells out to
// git and pings graph.db, bounded at ~3s. That ran on EVERY interactive search,
// including the common case where the graph is perfectly fresh and no notice
// would be printed at all — pure overhead on the hot path.
//
// The gate below is the cheap version of the same question. If the SHA recorded
// in index-state equals HEAD, the graph cannot be stale, so the whole collection
// is skipped. Anything less certain — no state, unreadable state, an empty-tree
// SHA, a git failure, or a genuine mismatch — falls through to the full walk, so
// the gate can only ever save time, never suppress a notice that was due.

// emitSearchGraphNotice prints the one-line stale/empty code-graph notice to
// stderr. Fully best-effort: any config/root failure, or a graph already known
// fresh, is silent, and the bounded status collection cannot stall search.
func emitSearchGraphNotice(cmd *cobra.Command) {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	root, err := config.FindRoot()
	if err != nil {
		return
	}
	ggDir := root + "/" + config.DirName
	if searchGraphLikelyFresh(root, ggDir) {
		// HEAD == last-indexed SHA: the graph is fresh, so no notice could fire.
		// Skipping here is what keeps a warm search off the status walk.
		return
	}
	status := codeGraphStatusWithTimeout(root, ggDir, cfg)
	emitGraphStatusNotice(cmd.OutOrStderr(), status)
}

// searchGraphLikelyFresh reports whether the recorded index SHA matches HEAD.
//
// Deliberately conservative: every uncertain case returns false and pays for the
// full status collection. A false positive would hide a staleness notice the
// operator needed, which is far worse than the few milliseconds it saves.
func searchGraphLikelyFresh(root, ggDir string) bool {
	s, err := state.Read(ggDir)
	if err != nil || s == nil || s.LastIndexedSHA == "" || s.LastIndexedSHA == changed.EmptyTreeSHA {
		return false
	}
	// Bounded: this gate exists to save time, so it must never become the stall
	// it was meant to avoid.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	head, err := changed.HeadSHA(ctx, root)
	if err != nil || head == "" {
		return false
	}
	return head == s.LastIndexedSHA
}
