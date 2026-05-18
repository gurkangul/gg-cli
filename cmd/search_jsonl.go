package cmd

import (
	"encoding/json"

	"github.com/gurkangul/gg-cli/internal/brain"
	"github.com/gurkangul/gg-cli/internal/store"
)

func taskFromJSONLEntry(e brain.Entry) store.Task {
	return store.Task{
		ID:        stringPayload(e.Payload, "task_id", e.UUID),
		Title:     stringPayload(e.Payload, "title", ""),
		Detail:    stringPayload(e.Payload, "detail", ""),
		Status:    stringPayload(e.Payload, "status", ""),
		Priority:  stringPayload(e.Payload, "priority", ""),
		DependsOn: stringSlicePayload(e.Payload, "depends_on"),
		Blocks:    stringSlicePayload(e.Payload, "blocks"),
		Deadline:  stringPayload(e.Payload, "deadline", ""),
		Tags:      stringSlicePayload(e.Payload, "tags"),
		Author:    stringPayload(e.Payload, "author", e.Author),
		Requester: stringPayload(e.Payload, "requester", ""),
		CreatedAt: stringPayload(e.Payload, "created_at", e.CreatedAt),
	}
}

func bugFromJSONLEntry(e brain.Entry) store.Bug {
	return store.Bug{
		ID:              stringPayload(e.Payload, "bug_id", e.UUID),
		Title:           stringPayload(e.Payload, "title", ""),
		Detail:          stringPayload(e.Payload, "detail", ""),
		Severity:        stringPayload(e.Payload, "severity", ""),
		Status:          stringPayload(e.Payload, "status", ""),
		RootCause:       stringPayload(e.Payload, "root_cause", ""),
		FixSummary:      stringPayload(e.Payload, "fix_summary", ""),
		ReproScript:     stringPayload(e.Payload, "repro_script", ""),
		TaskID:          stringPayload(e.Payload, "task_id", ""),
		Tags:            stringSlicePayload(e.Payload, "tags"),
		AffectedFiles:   stringSlicePayload(e.Payload, "affected_files"),
		AffectedSymbols: stringSlicePayload(e.Payload, "affected_symbols"),
		ReopenCount:     intPayload(e.Payload, "reopen_count"),
		ReopenReasons:   stringSlicePayload(e.Payload, "reopen_reasons"),
		By:              stringPayload(e.Payload, "by", e.Author),
		CreatedAt:       stringPayload(e.Payload, "created_at", e.CreatedAt),
		UpdatedAt:       stringPayload(e.Payload, "updated_at", ""),
	}
}

func stringPayload(payload map[string]any, key, fallback string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return fallback
}

func stringSlicePayload(payload map[string]any, key string) []string {
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

func intPayload(payload map[string]any, key string) int {
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
