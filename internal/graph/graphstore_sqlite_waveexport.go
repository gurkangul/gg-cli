package graph

import (
	"context"
	"fmt"
)

// upsertWave handles
// "MERGE (w:Wave {id: $wave_id, project_id: $pid}) SET w.name=..., ...".
func (s *sqliteStore) upsertWave(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	waveID, _ := params["wave_id"].(string)
	props := map[string]any{
		"id":         waveID,
		"name":       params["name"],
		"goal":       params["goal"],
		"start_date": params["start_date"],
		"end_date":   params["end_date"],
		"status":     params["status"],
		"project_id": pid,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existingID, found, err := s.findNodeIDByIdentity(ctx, LabelWave, pid, map[string]any{"id": waveID})
	if err != nil {
		return nil, err
	}
	encoded, err := encodeProps(props)
	if err != nil {
		return nil, err
	}
	if found {
		// MERGE ... SET overwrites the named fields; merge into existing props so
		// any unrelated fields survive (parity with Memgraph's per-field SET).
		var cur string
		if scanErr := s.db.QueryRowContext(ctx,
			`SELECT props FROM nodes WHERE internal_id = ?`, existingID).Scan(&cur); scanErr != nil {
			return nil, scanErr
		}
		merged, decErr := decodeProps(cur)
		if decErr != nil {
			return nil, decErr
		}
		for k, v := range props {
			merged[k] = v
		}
		encMerged, encErr := encodeProps(merged)
		if encErr != nil {
			return nil, encErr
		}
		if _, err := s.execWrite(ctx,
			`UPDATE nodes SET props = ? WHERE internal_id = ?`, encMerged, existingID); err != nil {
			return nil, fmt.Errorf("upsert wave %s: %w", waveID, err)
		}
		return emptyResult(), nil
	}
	if _, err := s.execWrite(ctx,
		`INSERT INTO nodes(label, project_id, props) VALUES(?, ?, ?)`, LabelWave, pid, encoded); err != nil {
		return nil, fmt.Errorf("insert wave %s: %w", waveID, err)
	}
	return emptyResult(), nil
}

// listWaves handles the two ListWaves templates (with / without a status
// filter), returning "w" node-property rows ordered by start_date.
func (s *sqliteStore) listWaves(ctx context.Context, params map[string]any, withStatus bool) (*GraphResult, error) {
	pid := pidOf(params)
	q := `SELECT props FROM nodes WHERE label = 'Wave' AND project_id = ?`
	args := []any{pid}
	if withStatus {
		status, _ := params["status"].(string)
		q += ` AND json_extract(props, '$.status') = ?`
		args = append(args, status)
	}
	q += ` ORDER BY json_extract(props, '$.start_date')`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := []string{"w"}
	var recs []GraphRecord
	for rows.Next() {
		var propsJSON string
		if err := rows.Scan(&propsJSON); err != nil {
			return nil, err
		}
		props, decErr := decodeProps(propsJSON)
		if decErr != nil {
			return nil, decErr
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{props}})
	}
	return newGraphResult(keys, recs), rows.Err()
}

// waveStatus handles
// "MATCH (w:Wave {id: $wave_id}) OPTIONAL MATCH (t:Task)-[:IN_WAVE]->(w)
//
//	RETURN w, collect(t.qdrant_id) AS tasks".
//
// Returns at most one row: the wave's props and the collected task ids. When the
// wave does not exist, returns zero rows (callers treat that as "not found").
func (s *sqliteStore) waveStatus(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	waveID, _ := params["wave_id"].(string)

	waveNodeID, found, err := s.findNodeIDByIdentity(ctx, LabelWave, pid, map[string]any{"id": waveID})
	if err != nil {
		return nil, err
	}
	if !found {
		return newGraphResult([]string{"w", "tasks"}, nil), nil
	}
	var propsJSON string
	if scanErr := s.db.QueryRowContext(ctx,
		`SELECT props FROM nodes WHERE internal_id = ?`, waveNodeID).Scan(&propsJSON); scanErr != nil {
		return nil, scanErr
	}
	props, decErr := decodeProps(propsJSON)
	if decErr != nil {
		return nil, decErr
	}

	taskRows, err := s.db.QueryContext(ctx,
		`SELECT json_extract(t.props, '$.qdrant_id')
		 FROM nodes t
		 JOIN edges e ON e.src = t.internal_id
		 WHERE e.project_id = ? AND e.type = 'IN_WAVE' AND e.dst = ?
		   AND t.label = 'Task' AND t.project_id = ?`,
		pid, waveNodeID, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = taskRows.Close() }()
	var tasks []any
	for taskRows.Next() {
		var id any
		if err := taskRows.Scan(&id); err != nil {
			return nil, err
		}
		tasks = append(tasks, nullToStr(id))
	}
	if err := taskRows.Err(); err != nil {
		return nil, err
	}
	keys := []string{"w", "tasks"}
	rec := GraphRecord{keys: keys, values: []any{props, tasks}}
	return newGraphResult(keys, []GraphRecord{rec}), nil
}

// exportNodes handles
// "MATCH (n {project_id: $pid}) RETURN labels(n) AS lbls, properties(n) AS props".
func (s *sqliteStore) exportNodes(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	rows, err := s.db.QueryContext(ctx,
		`SELECT label, props FROM nodes WHERE project_id = ?`, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := []string{"lbls", "props"}
	var recs []GraphRecord
	for rows.Next() {
		var label, propsJSON string
		if err := rows.Scan(&label, &propsJSON); err != nil {
			return nil, err
		}
		props, decErr := decodeProps(propsJSON)
		if decErr != nil {
			return nil, decErr
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{[]any{label}, normalizeProps(props)}})
	}
	return newGraphResult(keys, recs), rows.Err()
}

// exportEdges handles the ExportEdges template, returning src/dst labels+props,
// the relationship type and its props for every project-scoped edge whose
// endpoints are both in the project.
func (s *sqliteStore) exportEdges(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	rows, err := s.db.QueryContext(ctx,
		`SELECT src.label, src.props, dst.label, dst.props, e.type, e.props
		 FROM edges e
		 JOIN nodes src ON src.internal_id = e.src
		 JOIN nodes dst ON dst.internal_id = e.dst
		 WHERE e.project_id = ? AND src.project_id = ? AND dst.project_id = ?`,
		pid, pid, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	keys := []string{"src_lbls", "src_props", "dst_lbls", "dst_props", "rel_type", "rel_props"}
	var recs []GraphRecord
	for rows.Next() {
		var srcLabel, srcProps, dstLabel, dstProps, relType, relProps string
		if err := rows.Scan(&srcLabel, &srcProps, &dstLabel, &dstProps, &relType, &relProps); err != nil {
			return nil, err
		}
		sp, e1 := decodeProps(srcProps)
		if e1 != nil {
			return nil, e1
		}
		dp, e2 := decodeProps(dstProps)
		if e2 != nil {
			return nil, e2
		}
		rp, e3 := decodeProps(relProps)
		if e3 != nil {
			return nil, e3
		}
		recs = append(recs, GraphRecord{keys: keys, values: []any{
			[]any{srcLabel}, normalizeProps(sp),
			[]any{dstLabel}, normalizeProps(dp),
			relType, normalizeProps(rp),
		}})
	}
	return newGraphResult(keys, recs), rows.Err()
}
