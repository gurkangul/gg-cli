package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/outbox"
	"github.com/gurkangul/gg-cli/internal/store"
)

type outboxStatusSummary struct {
	Count     int
	Kinds     string
	OldestAge string
	Retries   int
}

func summarizeOutboxEntries(entries []outbox.Entry, now time.Time) outboxStatusSummary {
	s := outboxStatusSummary{Count: len(entries)}
	if len(entries) == 0 {
		return s
	}
	kinds := map[string]int{}
	var oldest time.Time
	for _, e := range entries {
		kinds[e.Kind]++
		s.Retries += e.Retries
		created, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err != nil {
			continue
		}
		if oldest.IsZero() || created.Before(oldest) {
			oldest = created
		}
	}
	parts := make([]string, 0, len(kinds))
	for kind, n := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", kind, n))
	}
	sort.Strings(parts)
	s.Kinds = strings.Join(parts, ", ")
	if !oldest.IsZero() {
		s.OldestAge = now.Sub(oldest).Round(time.Second).String()
	}
	return s
}

func formatPlaceholderVectorDetail(counts map[string]int) string {
	var breakdown []string
	total := 0
	for _, kind := range []string{"decisions", "tasks", "rejections", "discussions", "notes", "bugs"} {
		if n := counts[kind]; n > 0 {
			total += n
			breakdown = append(breakdown, fmt.Sprintf("%s=%d", kind, n))
		}
	}
	if total == 0 {
		return ""
	}
	detail := fmt.Sprintf("placeholder_vectors=%d", total)
	if len(breakdown) > 0 {
		detail += " (" + strings.Join(breakdown, ", ") + ")"
	}
	return detail
}

func semanticMemoryActionWarning(ctx context.Context, cfg *config.Config) string {
	ggDir := config.GGDirOrEmpty()
	if ggDir == "" {
		return ""
	}
	if cfg == nil {
		cfg, _ = config.Load()
	}
	var parts []string
	if entries, err := outbox.List(ggDir); err == nil && len(entries) > 0 {
		summary := summarizeOutboxEntries(entries, time.Now())
		part := fmt.Sprintf("outbox=%d", summary.Count)
		if summary.OldestAge != "" {
			part += " oldest=" + summary.OldestAge
		}
		if summary.Kinds != "" {
			part += " kinds=" + summary.Kinds
		}
		parts = append(parts, part)
	}
	if cfg != nil {
		c, err := store.New(&cfg.Qdrant, ggDir, cfg.ProjectID)
		if err == nil {
			defer func() { _ = c.Close() }()
			healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			healthErr := c.HealthCheck(healthCtx)
			cancel()
			if healthErr != nil {
				parts = append(parts, "qdrant unreachable")
			} else {
				countCtx, countCancel := context.WithTimeout(ctx, 2*time.Second)
				counts, countErr := c.DegradedVectorCounts(countCtx)
				countCancel()
				if countErr == nil {
					if detail := formatPlaceholderVectorDetail(counts); detail != "" {
						parts = append(parts, detail)
					}
				}
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	hint := "Run: gg doctor"
	for _, part := range parts {
		if strings.HasPrefix(part, "placeholder_vectors=") {
			hint = "Run: gg reembed"
			break
		}
	}
	for _, part := range parts {
		if part == "qdrant unreachable" {
			hint = "Run: gg doctor"
			break
		}
	}
	return "semantic memory degraded: " + strings.Join(parts, "; ") + ". " + hint
}

func emitSemanticMemoryNotice(ctx context.Context, w io.Writer, cfg *config.Config) {
	if warning := semanticMemoryActionWarning(ctx, cfg); warning != "" {
		fmt.Fprintln(w, "─── SEMANTIC MEMORY NOTICE ───")
		fmt.Fprintln(w, warning)
		fmt.Fprintln(w)
	}
}
