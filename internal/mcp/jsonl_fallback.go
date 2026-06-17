package mcp

import (
	"encoding/json"

	brainpkg "github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

// JSONL fallback for the read tools (FIX 1).
//
// With the embedded SQLite backend the store is always reachable, so a fresh /
// un-reembedded project has no collection to query and a semantic read returns
// store.IsCollectionNotFoundError rather than a transport failure. The CLI
// handles this by falling back to the offline JSONL lexical scan
// (cmd/search.go:serveSearchFromJSONL); without the same fallback the MCP tools
// silently report EMPTY durable memory — a false-negative. These helpers mirror
// that behaviour for each store kind:
//
//   - store search ok          → return the store results, err == nil
//   - IsCollectionNotFoundError → JSONL scan of <ggDir>/brain/<kind>.jsonl
//   - any other store error     → surface it (caller emits isError=true)
//
// The JSONL-derived records are mapped into the same store.* types the store
// path returns, so the JSON payload contract the agent reads is unchanged.

// fallbackSearch runs a store search; on collection-not-found it scans the JSONL
// brain for kind and maps each matched entry via conv. Any other store error is
// returned verbatim so the caller can surface it instead of an empty result.
func fallbackSearch[T any](
	ggDir, kind string,
	query string,
	search func() ([]T, error),
	conv func(brainpkg.Entry) T,
) ([]T, error) {
	out, err := search()
	if err == nil {
		return out, nil
	}
	if !store.IsCollectionNotFoundError(err) {
		return nil, err
	}
	// Un-reembedded collection — fall back to the JSONL lexical scan.
	matches, scanErr := brainpkg.SearchByTextScored(ggDir, kind, query)
	if scanErr != nil {
		// No JSONL file (pre-JSONL brain) → treat as genuinely empty, not an
		// error. The CLI falls through to its LKG cache here; the MCP server has
		// no cache layer, so an empty bundle is the correct, non-erroring answer.
		return nil, nil
	}
	res := make([]T, 0, len(matches))
	for _, m := range matches {
		res = append(res, conv(m.Entry))
	}
	return res, nil
}

// firstSearchError returns the message of the first non-nil error, or "" when
// all are nil. fallbackSearch already absorbs collection-not-found, so any error
// reaching here is a real store failure the caller must surface (isError=true)
// rather than silently reporting empty durable memory.
func firstSearchError(errs ...error) string {
	for _, e := range errs {
		if e != nil {
			return e.Error()
		}
	}
	return ""
}

// ── brainpkg.Entry → store.* converters (replicated from cmd/search_jsonl.go;
//    internal/mcp cannot import cmd without an import cycle) ──────────────────

func decisionFromEntry(e brainpkg.Entry) store.Decision {
	return store.Decision{
		ID:        e.UUID,
		Text:      payStr(e.Payload, "text", ""),
		Reason:    payStr(e.Payload, "reason", ""),
		Status:    payStr(e.Payload, "status", ""),
		Tags:      payStrSlice(e.Payload, "tags"),
		TaskID:    payStr(e.Payload, "task_id", ""),
		Author:    payStr(e.Payload, "author", e.Author),
		CreatedAt: payStr(e.Payload, "created_at", e.CreatedAt),
	}
}

func rejectionFromEntry(e brainpkg.Entry) store.Rejection {
	return store.Rejection{
		ID:        e.UUID,
		Approach:  payStr(e.Payload, "approach", ""),
		Reason:    payStr(e.Payload, "reason", ""),
		Tags:      payStrSlice(e.Payload, "tags"),
		TaskID:    payStr(e.Payload, "task_id", ""),
		Author:    payStr(e.Payload, "author", e.Author),
		CreatedAt: payStr(e.Payload, "created_at", e.CreatedAt),
	}
}

func taskFromEntry(e brainpkg.Entry) store.Task {
	return store.Task{
		ID:        payStr(e.Payload, "task_id", e.UUID),
		Title:     payStr(e.Payload, "title", ""),
		Detail:    payStr(e.Payload, "detail", ""),
		Status:    payStr(e.Payload, "status", ""),
		Priority:  payStr(e.Payload, "priority", ""),
		DependsOn: payStrSlice(e.Payload, "depends_on"),
		Blocks:    payStrSlice(e.Payload, "blocks"),
		Deadline:  payStr(e.Payload, "deadline", ""),
		Tags:      payStrSlice(e.Payload, "tags"),
		Author:    payStr(e.Payload, "author", e.Author),
		Requester: payStr(e.Payload, "requester", ""),
		CreatedAt: payStr(e.Payload, "created_at", e.CreatedAt),
	}
}

func bugFromEntry(e brainpkg.Entry) store.Bug {
	return store.Bug{
		ID:              payStr(e.Payload, "bug_id", e.UUID),
		Title:           payStr(e.Payload, "title", ""),
		Detail:          payStr(e.Payload, "detail", ""),
		Severity:        payStr(e.Payload, "severity", ""),
		Status:          payStr(e.Payload, "status", ""),
		RootCause:       payStr(e.Payload, "root_cause", ""),
		FixSummary:      payStr(e.Payload, "fix_summary", ""),
		ReproScript:     payStr(e.Payload, "repro_script", ""),
		TaskID:          payStr(e.Payload, "task_id", ""),
		Tags:            payStrSlice(e.Payload, "tags"),
		AffectedFiles:   payStrSlice(e.Payload, "affected_files"),
		AffectedSymbols: payStrSlice(e.Payload, "affected_symbols"),
		ReopenCount:     payInt(e.Payload, "reopen_count"),
		ReopenReasons:   payStrSlice(e.Payload, "reopen_reasons"),
		By:              payStr(e.Payload, "by", e.Author),
		CreatedAt:       payStr(e.Payload, "created_at", e.CreatedAt),
		UpdatedAt:       payStr(e.Payload, "updated_at", ""),
	}
}

func noteFromEntry(e brainpkg.Entry) store.Note {
	return store.Note{
		ID:        e.UUID,
		Text:      payStr(e.Payload, "text", ""),
		Tags:      payStrSlice(e.Payload, "tags"),
		TaskID:    payStr(e.Payload, "task_id", ""),
		CreatedAt: payStr(e.Payload, "created_at", e.CreatedAt),
	}
}

func discussionFromEntry(e brainpkg.Entry) store.Discussion {
	return store.Discussion{
		ID:           payStr(e.Payload, "disc_id", e.UUID),
		Topic:        payStr(e.Payload, "topic", ""),
		Detail:       payStr(e.Payload, "detail", ""),
		Status:       payStr(e.Payload, "status", ""),
		ResolvedVia:  payStr(e.Payload, "resolved_via", ""),
		ResolvedNote: payStr(e.Payload, "resolved_note", ""),
		DismissNote:  payStr(e.Payload, "dismiss_note", ""),
		Tags:         payStrSlice(e.Payload, "tags"),
		CreatedAt:    payStr(e.Payload, "created_at", e.CreatedAt),
		UpdatedAt:    payStr(e.Payload, "updated_at", ""),
	}
}

// ── payload accessors (mirror cmd/search_jsonl.go) ─────────────────────────

func payStr(payload map[string]any, key, fallback string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return fallback
}

func payStrSlice(payload map[string]any, key string) []string {
	switch values := payload[key].(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func payInt(payload map[string]any, key string) int {
	switch v := payload[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}
