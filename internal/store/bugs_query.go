package store

import (
	"context"
)

// ActiveBugsFilter returns the payload filter that restricts results to bugs
// in an active state (open, fixing, or reopened). Fixed and wontfix bugs are
// excluded from default retrieval.
func ActiveBugsFilter() *Filter {
	return &Filter{
		Must: []*Condition{
			NewMatchKeywords("status", "open", "fixing", "reopened"),
		},
	}
}

// SearchBugs performs a semantic search across the bugs collection.
// When includeAll is false (the default for agent consumption), only active
// bugs are returned — fixed and wontfix bugs are suppressed so agents don't
// surface resolved issues as current context.
func (c *Client) SearchBugs(ctx context.Context, vector []float32, limit uint64, includeAll bool) ([]Bug, error) {
	req := &QueryPoints{
		CollectionName: c.collBugs(),
		Query:          NewQuery(vector...),
		Limit:          PtrOf(limit),
		WithPayload:    NewWithPayloadEnable(true),
	}
	if !includeAll {
		f := ActiveBugsFilter()
		f.Must = append(f.Must, nonDegradedVectorCondition())
		req.Filter = f
	} else {
		req.Filter = &Filter{Must: []*Condition{nonDegradedVectorCondition()}}
	}
	results, err := c.vsQuery(ctx, req)
	if err != nil {
		return nil, err
	}
	bugs := make([]Bug, 0, len(results))
	for _, r := range results {
		b := bugFromPayload(r.GetPayload())
		b.SemanticScore = r.GetScore()
		bugs = append(bugs, b)
	}
	return bugs, nil
}

func (c *Client) CountBugs(ctx context.Context, statusFilter string) (uint64, error) {
	req := &CountPoints{
		CollectionName: c.collBugs(),
	}
	if statusFilter != "" {
		req.Filter = &Filter{
			Must: []*Condition{NewMatchKeyword("status", statusFilter)},
		}
	}
	return c.vs.Count(ctx, req)
}

// CountBugsByTag returns the number of bugs whose tags list contains tag.
func (c *Client) CountBugsByTag(ctx context.Context, tag string) (uint64, error) {
	return c.vs.Count(ctx, &CountPoints{
		CollectionName: c.collBugs(),
		Filter: &Filter{
			Must: []*Condition{NewMatchKeyword("tags", tag)},
		},
	})
}

func bugFromPayload(pay map[string]*Value) Bug {
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
		VectorDegraded:  pay["gg_vector_degraded"].GetStringValue() != "",
	}
}
