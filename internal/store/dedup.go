package store

import (
	"context"
	"fmt"

	"github.com/qdrant/go-client/qdrant"
)

// DupCandidate is a near-duplicate result returned by FindNearDups.
type DupCandidate struct {
	ID    string  // human-readable ID (TASK-NNN, DISC-NNN, etc.) or short UUID prefix
	Label string  // title / text / topic / approach
	Score float32 // cosine similarity (0–1)
}

// FindNearDups searches the given kind's collection for vectors with cosine
// similarity at or above threshold, returning at most limit candidates.
// kind must be one of: tasks, decisions, rejections, notes, discussions, bugs.
// Dedup is best-effort: if the collection is empty or the search fails, a nil
// slice and nil error are returned so the caller can continue creation.
func (c *Client) FindNearDups(ctx context.Context, kind string, vector []float32, threshold float32, limit uint64) ([]DupCandidate, error) {
	coll, idField, labelField, err := kindMeta(c, kind)
	if err != nil {
		return nil, err
	}

	results, err := c.qc.Query(ctx, &qdrant.QueryPoints{
		CollectionName: coll,
		Query:          qdrant.NewQuery(vector...),
		Limit:          qdrant.PtrOf(limit),
		WithPayload:    qdrant.NewWithPayloadEnable(true),
		ScoreThreshold: qdrant.PtrOf(threshold),
	})
	if err != nil {
		// Collection may not exist on first run — treat as no duplicates.
		return nil, nil //nolint:nilerr
	}

	cands := make([]DupCandidate, 0, len(results))
	for _, r := range results {
		pay := r.GetPayload()
		id := ""
		if idField != "" {
			id = pay[idField].GetStringValue()
		}
		// UUID-keyed types (decisions, rejections, notes) have no short ID in payload.
		if id == "" {
			raw := r.GetId().GetUuid()
			if len(raw) >= 8 {
				id = raw[:8]
			} else {
				id = raw
			}
		}
		label := pay[labelField].GetStringValue()
		if label == "" {
			continue // malformed point — skip
		}
		cands = append(cands, DupCandidate{
			ID:    id,
			Label: label,
			Score: r.GetScore(),
		})
	}
	return cands, nil
}

// kindMeta maps a collection kind name to its Qdrant collection, the payload
// field used as a human-readable ID (empty string for UUID-keyed types), and
// the payload field used as the display label.
func kindMeta(c *Client, kind string) (coll, idField, labelField string, err error) {
	switch kind {
	case "tasks":
		return c.collTasks(), "task_id", "title", nil
	case "decisions":
		return c.collDecisions(), "", "text", nil
	case "rejections":
		return c.collRejections(), "", "approach", nil
	case "notes":
		return c.collNotes(), "", "text", nil
	case "discussions":
		return c.collDiscussions(), "disc_id", "topic", nil
	case "bugs":
		return c.collBugs(), "bug_id", "title", nil
	default:
		return "", "", "", fmt.Errorf("unknown kind %q — use tasks, decisions, rejections, notes, discussions, or bugs", kind)
	}
}
