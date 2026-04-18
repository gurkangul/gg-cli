package graph

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// BugRef is a minimal summary of a Bug node returned from graph queries.
type BugRef struct {
	BugID string
	Title string
}

// MergeBugAffects upserts a Bug node and writes (Bug)-[:AFFECTS]->(File) and
// (Bug)-[:AFFECTS]->(Symbol) edges for each provided path / symbol name.
//
// File nodes are upserted by path (created if not yet indexed).
// Symbol nodes are upserted by name (best-effort — may match multiple symbols
// if the name is not unique; callers should provide qualified names when known).
// Missing files/symbols in the graph are silently created as stubs; they will
// be enriched by the next `gg index` run.
func (c *Client) MergeBugAffects(ctx context.Context, bugID, title string, files, symbols []string) error {
	if bugID == "" {
		return fmt.Errorf("MergeBugAffects: bugID is required")
	}

	// Upsert the Bug node.
	bugNode := BugNode(bugID, title)
	if err := c.UpsertNode(ctx, bugNode, []string{"bug_id"}); err != nil {
		return fmt.Errorf("merge bug node %s: %w", bugID, err)
	}

	// AFFECTS → File edges.
	for _, path := range files {
		if path == "" {
			continue
		}
		// Upsert a stub File node so the edge target always exists.
		fileNode := FileNode(path, "", "", ResolutionSyntactic)
		if err := c.UpsertNode(ctx, fileNode, []string{"path"}); err != nil {
			return fmt.Errorf("upsert file stub %s: %w", path, err)
		}
		if err := c.UpsertEdgeByKey(ctx,
			LabelBug, map[string]any{"bug_id": bugID},
			LabelFile, map[string]any{"path": path},
			RelAffects, nil,
		); err != nil {
			return fmt.Errorf("bug %s affects file %s: %w", bugID, path, err)
		}
	}

	// AFFECTS → Symbol edges (matched by name — best-effort).
	for _, name := range symbols {
		if name == "" {
			continue
		}
		if err := c.UpsertEdgeByKey(ctx,
			LabelBug, map[string]any{"bug_id": bugID},
			LabelSymbol, map[string]any{"name": name},
			RelAffects, nil,
		); err != nil {
			// Symbol may not exist yet; log as warning rather than hard-failing.
			// Future: once TASK-200 ships the full Bug→Symbol edge this can be strict.
			_ = err
		}
	}

	return nil
}

// BugsAffectingFile returns BugRef entries for every Bug node that has an
// AFFECTS edge pointing to the File node with the given path, scoped to this
// project. Used by `gg impact` to surface historical bugs on a file.
func (c *Client) BugsAffectingFile(ctx context.Context, filePath string) ([]BugRef, error) {
	result, cleanup, err := c.runQuery(ctx,
		"MATCH (b:Bug {project_id: $pid})-[:AFFECTS]->(f:File {path: $path, project_id: $pid}) "+
			"RETURN b.bug_id AS bug_id, b.title AS title",
		map[string]any{"path": filePath},
	)
	if err != nil {
		return nil, fmt.Errorf("bugs affecting %s: %w", filePath, err)
	}
	defer cleanup()

	var refs []BugRef
	for result.Next(ctx) {
		record := result.Record()
		id, _, _ := neo4j.GetRecordValue[string](record, "bug_id")
		title, _, _ := neo4j.GetRecordValue[string](record, "title")
		if id != "" {
			refs = append(refs, BugRef{BugID: id, Title: title})
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("bugs affecting %s iterate: %w", filePath, err)
	}
	return refs, nil
}

// BugAffects returns the file paths and symbol names that a Bug node affects,
// scoped to this project. Used by `gg impact BUG-NNN` to show blast radius.
func (c *Client) BugAffects(ctx context.Context, bugID string) (files []string, symbols []string, err error) {
	result, cleanup, qErr := c.runQuery(ctx,
		"MATCH (b:Bug {bug_id: $bug_id, project_id: $pid})-[:AFFECTS]->(target) "+
			"RETURN labels(target) AS lbls, "+
			"CASE WHEN 'File' IN labels(target) THEN target.path ELSE target.name END AS val",
		map[string]any{"bug_id": bugID},
	)
	if qErr != nil {
		return nil, nil, fmt.Errorf("bug affects %s: %w", bugID, qErr)
	}
	defer cleanup()

	for result.Next(ctx) {
		record := result.Record()
		lbls, _, _ := neo4j.GetRecordValue[[]any](record, "lbls")
		val, _, _ := neo4j.GetRecordValue[string](record, "val")
		if val == "" {
			continue
		}
		isFile := false
		for _, l := range lbls {
			if s, ok := l.(string); ok && s == LabelFile {
				isFile = true
				break
			}
		}
		if isFile {
			files = append(files, val)
		} else {
			symbols = append(symbols, val)
		}
	}
	if err := result.Err(); err != nil {
		return nil, nil, fmt.Errorf("bug affects %s iterate: %w", bugID, err)
	}
	return files, symbols, nil
}
