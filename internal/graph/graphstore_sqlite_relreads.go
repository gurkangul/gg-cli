package graph

import (
	"context"
	"sort"
	"strconv"
)

// callees handles the single-hop CALLS neighbor lookup used by the Go-side BFS:
// "MATCH (s:Symbol)-[:CALLS]->(to:Symbol) WHERE id(s) = $id RETURN id, props".
func (s *sqliteStore) callees(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	idStr, _ := params["id"].(string)
	srcID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return idPropsResult(nil), nil
	}
	rows, err := s.queryNodes(ctx,
		`SELECT n.internal_id, n.label, n.props FROM nodes n
		 JOIN edges e ON e.dst = n.internal_id
		 WHERE e.project_id = ? AND e.type = 'CALLS' AND e.src = ?
		   AND n.label = 'Symbol' AND n.project_id = ?
		 ORDER BY json_extract(n.props, '$.source_file'), json_extract(n.props, '$.name')`,
		pid, srcID, pid)
	if err != nil {
		return nil, err
	}
	return idPropsResult(rows), nil
}

// dependentsOf handles
// "MATCH (d:File)-[:IMPORTS]->(f:File {path: $path}) RETURN d.path AS dep ORDER BY dep".
func (s *sqliteStore) dependentsOf(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	path, _ := params["path"].(string)
	rows, err := s.db.QueryContext(ctx,
		`SELECT json_extract(d.props, '$.path') AS dep
		 FROM nodes d
		 JOIN edges e ON e.src = d.internal_id
		 JOIN nodes f ON f.internal_id = e.dst
		 WHERE e.project_id = ? AND e.type = 'IMPORTS'
		   AND d.label = 'File' AND d.project_id = ?
		   AND f.label = 'File' AND f.project_id = ?
		   AND json_extract(f.props, '$.path') = ?
		 ORDER BY dep`,
		pid, pid, pid, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanStringColumn(rows, "dep")
}

// referencersOf handles
// "MATCH (d:File)-[:REFERENCES]->(s:Symbol) WHERE id(s) = $id RETURN DISTINCT d.path AS dep".
// It is the symbol-exact reverse of dependentsOf: given a Symbol node id, return
// the distinct files that reference it, ordered by path.
func (s *sqliteStore) referencersOf(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	idStr, _ := params["id"].(string)
	symID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return newGraphResult([]string{"dep"}, nil), nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT json_extract(d.props, '$.path') AS dep
		 FROM nodes d
		 JOIN edges e ON e.src = d.internal_id
		 JOIN nodes sym ON sym.internal_id = e.dst
		 WHERE e.project_id = ? AND e.type = 'REFERENCES'
		   AND d.label = 'File' AND d.project_id = ?
		   AND sym.label = 'Symbol' AND sym.project_id = ?
		   AND sym.internal_id = ?
		 ORDER BY dep`,
		pid, pid, pid, symID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanStringColumn(rows, "dep")
}

// bugsAffectingFile handles
// "MATCH (b:Bug)-[:AFFECTS]->(f:File {path: $path}) RETURN b.bug_id, b.title".
func (s *sqliteStore) bugsAffectingFile(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	path, _ := params["path"].(string)
	rows, err := s.db.QueryContext(ctx,
		`SELECT json_extract(b.props, '$.bug_id') AS bug_id,
		        json_extract(b.props, '$.title')  AS title
		 FROM nodes b
		 JOIN edges e ON e.src = b.internal_id
		 JOIN nodes f ON f.internal_id = e.dst
		 WHERE e.project_id = ? AND e.type = 'AFFECTS'
		   AND b.label = 'Bug' AND b.project_id = ?
		   AND f.label = 'File' AND f.project_id = ?
		   AND json_extract(f.props, '$.path') = ?`,
		pid, pid, pid, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := []string{"bug_id", "title"}
	var recs []GraphRecord
	for rows.Next() {
		var bugID, title any
		if err := rows.Scan(&bugID, &title); err != nil {
			return nil, err
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{nullToStr(bugID), nullToStr(title)}})
	}
	return newGraphResult(keys, recs), rows.Err()
}

// bugAffects handles the BugAffects template: for each AFFECTS target return the
// target's labels and its path (File) or name (otherwise).
func (s *sqliteStore) bugAffects(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	bugID, _ := params["bug_id"].(string)
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.label,
		        CASE WHEN t.label = 'File'
		             THEN json_extract(t.props, '$.path')
		             ELSE json_extract(t.props, '$.name') END AS val
		 FROM nodes b
		 JOIN edges e ON e.src = b.internal_id
		 JOIN nodes t ON t.internal_id = e.dst
		 WHERE e.project_id = ? AND e.type = 'AFFECTS'
		   AND b.label = 'Bug' AND b.project_id = ?
		   AND json_extract(b.props, '$.bug_id') = ?
		   AND t.project_id = ?`,
		pid, pid, bugID, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := []string{"lbls", "val"}
	var recs []GraphRecord
	for rows.Next() {
		var label string
		var val any
		if err := rows.Scan(&label, &val); err != nil {
			return nil, err
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{[]any{label}, nullToStr(val)}})
	}
	return newGraphResult(keys, recs), rows.Err()
}

// taskDependents handles the UNION of DEPENDS_ON (reverse) and BLOCKS (forward)
// task-id lookups. Returns distinct "id" rows.
func (s *sqliteStore) taskDependents(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	tid, _ := params["tid"].(string)
	ids := map[string]bool{}

	// waiter -[:DEPENDS_ON]-> t(tid)
	if err := s.collectTaskIDs(ctx, &ids,
		`SELECT json_extract(w.props, '$.qdrant_id')
		 FROM nodes t
		 JOIN edges e ON e.dst = t.internal_id
		 JOIN nodes w ON w.internal_id = e.src
		 WHERE e.project_id = ? AND e.type = 'DEPENDS_ON'
		   AND t.label = 'Task' AND t.project_id = ? AND json_extract(t.props, '$.qdrant_id') = ?
		   AND w.label = 'Task' AND w.project_id = ?`,
		pid, pid, tid, pid); err != nil {
		return nil, err
	}
	// t(tid) -[:BLOCKS]-> gated
	if err := s.collectTaskIDs(ctx, &ids,
		`SELECT json_extract(g.props, '$.qdrant_id')
		 FROM nodes t
		 JOIN edges e ON e.src = t.internal_id
		 JOIN nodes g ON g.internal_id = e.dst
		 WHERE e.project_id = ? AND e.type = 'BLOCKS'
		   AND t.label = 'Task' AND t.project_id = ? AND json_extract(t.props, '$.qdrant_id') = ?
		   AND g.label = 'Task' AND g.project_id = ?`,
		pid, pid, tid, pid); err != nil {
		return nil, err
	}
	return idStringResult(ids), nil
}

// taskSiblings handles the UNION of "share an IMPLEMENTS decision" and "share a
// DECIDES decision" sibling lookups. Returns distinct "id" rows, excluding tid.
func (s *sqliteStore) taskSiblings(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	tid, _ := params["tid"].(string)
	ids := map[string]bool{}

	// t -[:IMPLEMENTS]-> d <-[:IMPLEMENTS]- sibling
	if err := s.collectTaskIDs(ctx, &ids,
		`SELECT json_extract(sib.props, '$.qdrant_id')
		 FROM nodes t
		 JOIN edges e1 ON e1.src = t.internal_id AND e1.type = 'IMPLEMENTS' AND e1.project_id = ?
		 JOIN nodes d  ON d.internal_id = e1.dst AND d.label = 'Decision' AND d.project_id = ?
		 JOIN edges e2 ON e2.dst = d.internal_id AND e2.type = 'IMPLEMENTS' AND e2.project_id = ?
		 JOIN nodes sib ON sib.internal_id = e2.src AND sib.label = 'Task' AND sib.project_id = ?
		 WHERE t.label = 'Task' AND t.project_id = ? AND json_extract(t.props, '$.qdrant_id') = ?
		   AND json_extract(sib.props, '$.qdrant_id') <> ?`,
		pid, pid, pid, pid, pid, tid, tid); err != nil {
		return nil, err
	}
	// d -[:DECIDES]-> t  and  d -[:DECIDES]-> sibling
	if err := s.collectTaskIDs(ctx, &ids,
		`SELECT json_extract(sib.props, '$.qdrant_id')
		 FROM nodes d
		 JOIN edges e1 ON e1.src = d.internal_id AND e1.type = 'DECIDES' AND e1.project_id = ?
		 JOIN nodes t  ON t.internal_id = e1.dst AND t.label = 'Task' AND t.project_id = ?
		 JOIN edges e2 ON e2.src = d.internal_id AND e2.type = 'DECIDES' AND e2.project_id = ?
		 JOIN nodes sib ON sib.internal_id = e2.dst AND sib.label = 'Task' AND sib.project_id = ?
		 WHERE d.label = 'Decision' AND d.project_id = ?
		   AND json_extract(t.props, '$.qdrant_id') = ?
		   AND json_extract(sib.props, '$.qdrant_id') <> ?`,
		pid, pid, pid, pid, pid, tid, tid); err != nil {
		return nil, err
	}
	return idStringResult(ids), nil
}

// collectTaskIDs runs a single-column id query and adds non-empty results to ids.
func (s *sqliteStore) collectTaskIDs(ctx context.Context, ids *map[string]bool, sqlText string, args ...any) error {
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id any
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if str := nullToStr(id); str != "" {
			(*ids)[str] = true
		}
	}
	return rows.Err()
}

// idStringResult builds a result of distinct "id" string rows, sorted for
// determinism (Memgraph's RETURN DISTINCT has no inherent order; callers do not
// rely on a specific one, but a stable order keeps tests reproducible).
func idStringResult(ids map[string]bool) *GraphResult {
	keys := []string{"id"}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	recs := make([]GraphRecord, 0, len(out))
	for _, id := range out {
		recs = append(recs, GraphRecord{keys: keys, values: []any{id}})
	}
	return newGraphResult(keys, recs)
}

// scanStringColumn materializes a single-column string result under colName.
func scanStringColumn(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}, colName string) (*GraphResult, error) {
	keys := []string{colName}
	var recs []GraphRecord
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{nullToStr(v)}})
	}
	return newGraphResult(keys, recs), rows.Err()
}

// nullToStr coerces a scanned value (which may be nil for a SQL NULL or a JSON
// null, or []byte / string from json_extract) to a Go string.
func nullToStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}
