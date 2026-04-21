package store

import (
	"context"

	"github.com/qdrant/go-client/qdrant"
)

func (c *Client) SearchBugs(ctx context.Context, vector []float32, limit uint64) ([]Bug, error) {
	results, err := c.qdrantQuery(ctx, &qdrant.QueryPoints{
		CollectionName: c.collBugs(),
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
	})
	if err != nil {
		return nil, err
	}
	bugs := make([]Bug, 0, len(results))
	for _, r := range results {
		bugs = append(bugs, bugFromPayload(r.GetPayload()))
	}
	return bugs, nil
}

func (c *Client) CountBugs(ctx context.Context, statusFilter string) (uint64, error) {
	req := &qdrant.CountPoints{
		CollectionName: c.collBugs(),
	}
	if statusFilter != "" {
		req.Filter = &qdrant.Filter{
			Must: []*qdrant.Condition{qdrant.NewMatchKeyword("status", statusFilter)},
		}
	}
	return c.qc.Count(ctx, req)
}

// CountBugsByTag returns the number of bugs whose tags list contains tag.
func (c *Client) CountBugsByTag(ctx context.Context, tag string) (uint64, error) {
	return c.qc.Count(ctx, &qdrant.CountPoints{
		CollectionName: c.collBugs(),
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{qdrant.NewMatchKeyword("tags", tag)},
		},
	})
}

func bugFromPayload(pay map[string]*qdrant.Value) Bug {
	return Bug{
		ID:              pay["bug_id"].GetStringValue(),
		Title:           pay["title"].GetStringValue(),
		Detail:          pay["detail"].GetStringValue(),
		Severity:        pay["severity"].GetStringValue(),
		Status:          pay["status"].GetStringValue(),
		RootCause:       pay["root_cause"].GetStringValue(),
		FixSummary:      pay["fix_summary"].GetStringValue(),
		ReproScript:     pay["repro_script"].GetStringValue(),
		TaskID:          pay["task_id"].GetStringValue(),
		Tags:            extractStringList(pay["tags"]),
		AffectedFiles:   extractStringList(pay["affected_files"]),
		AffectedSymbols: extractStringList(pay["affected_symbols"]),
		ReopenCount:     int(pay["reopen_count"].GetDoubleValue()),
		ReopenReasons:   extractStringList(pay["reopen_reasons"]),
		By:              pay["by"].GetStringValue(),
		CreatedAt:       pay["created_at"].GetStringValue(),
		UpdatedAt:       pay["updated_at"].GetStringValue(),
	}
}
