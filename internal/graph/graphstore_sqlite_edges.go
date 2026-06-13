package graph

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
)

// resolveNodeID looks up a node's internal_id from a string element id ($from /
// $to), scoped to pid. Returns (0, false) when absent or not numeric.
func (s *sqliteStore) resolveNodeID(ctx context.Context, idStr, pid string) (int64, bool, error) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, false, nil
	}
	var got int64
	qErr := s.db.QueryRowContext(ctx,
		`SELECT internal_id FROM nodes WHERE internal_id = ? AND project_id = ?`, id, pid).Scan(&got)
	if qErr == sql.ErrNoRows {
		return 0, false, nil
	}
	if qErr != nil {
		return 0, false, qErr
	}
	return got, true, nil
}

// resolveNodeIDByProps looks up a node's internal_id by (label, project_id,
// identity props). Returns (0, false) when no node matches — mirroring Memgraph
// MATCH producing no rows, so the subsequent MERGE/CREATE is a no-op.
func (s *sqliteStore) resolveNodeIDByProps(ctx context.Context, label, pid string, identity map[string]any) (int64, bool, error) {
	return s.findNodeIDByIdentity(ctx, label, pid, identity)
}

// createEdgeByID handles the CreateEdge template: match both endpoints by
// element id within the project, then CREATE the relationship.
func (s *sqliteStore) createEdgeByID(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	edgeType := firstSubmatch(edgeTypeCreate, normalizeCypher(cypher))
	if edgeType == "" {
		return nil, unrecognizedCypher(cypher)
	}
	return s.insertEdgeByID(ctx, edgeType, params, false)
}

// upsertEdgeByID handles the UpsertEdge template: like createEdgeByID but MERGE
// (insert-or-update) on (src, type, dst).
func (s *sqliteStore) upsertEdgeByID(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	edgeType := firstSubmatch(edgeTypeMerge, normalizeCypher(cypher))
	if edgeType == "" {
		return nil, unrecognizedCypher(cypher)
	}
	return s.insertEdgeByID(ctx, edgeType, params, true)
}

func (s *sqliteStore) insertEdgeByID(ctx context.Context, edgeType string, params map[string]any, merge bool) (*GraphResult, error) {
	from, _ := params["from"].(string)
	to, _ := params["to"].(string)
	props, _ := params["props"].(map[string]any)
	pid := propStr(props, "project_id")
	if pid == "" {
		pid = pidOf(params)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	srcID, srcOK, err := s.resolveNodeID(ctx, from, pid)
	if err != nil {
		return nil, err
	}
	dstID, dstOK, err := s.resolveNodeID(ctx, to, pid)
	if err != nil {
		return nil, err
	}
	if !srcOK || !dstOK {
		// One endpoint is missing or in another project — MATCH yields no rows,
		// so CREATE/MERGE does nothing (no cross-project edge possible).
		return emptyResult(), nil
	}
	return s.writeEdge(ctx, srcID, dstID, edgeType, pid, props, merge)
}

// upsertEdgeByKey handles the UpsertEdgeByKey template: both endpoints are
// matched by label + WHERE equality clauses, then the relationship is MERGEd.
func (s *sqliteStore) upsertEdgeByKey(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	nc := normalizeCypher(cypher)
	srcLabel := firstSubmatch(labelClauseSrc, nc)
	dstLabel := firstSubmatch(labelClauseDst, nc)
	edgeType := firstSubmatch(edgeTypeMerge, nc)
	if srcLabel == "" || dstLabel == "" || edgeType == "" {
		return nil, unrecognizedCypher(cypher)
	}
	srcIdentity := parseWhereEqualityClauses(nc, "a", params)
	dstIdentity := parseWhereEqualityClauses(nc, "b", params)
	rprops, _ := params["rprops"].(map[string]any)
	pid := propStr(rprops, "project_id")
	if pid == "" {
		pid = pidOf(params)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	srcID, srcOK, err := s.resolveNodeIDByProps(ctx, srcLabel, pid, srcIdentity)
	if err != nil {
		return nil, err
	}
	dstID, dstOK, err := s.resolveNodeIDByProps(ctx, dstLabel, pid, dstIdentity)
	if err != nil {
		return nil, err
	}
	if !srcOK || !dstOK {
		return emptyResult(), nil
	}
	return s.writeEdge(ctx, srcID, dstID, edgeType, pid, rprops, true)
}

// upsertKnowledgeEdge handles the upsertKnowledgeEdge / upsertBackfillEdge
// templates: both endpoints matched by qdrant_id within the project, then the
// relationship MERGEd with project_id + created_at (+ created_by for backfill).
func (s *sqliteStore) upsertKnowledgeEdge(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	m := knowledgeEdgeRE.FindStringSubmatch(normalizeCypher(cypher))
	if len(m) < 4 {
		return nil, unrecognizedCypher(cypher)
	}
	srcLabel, dstLabel, edgeType := m[1], m[2], m[3]
	pid := pidOf(params)
	srcID, _ := params["src_id"].(string)
	dstID, _ := params["dst_id"].(string)

	edgeProps := map[string]any{"project_id": pid}
	if ts, ok := params["ts"]; ok {
		edgeProps["created_at"] = ts
	}
	if cb, ok := params["created_by"]; ok {
		edgeProps["created_by"] = cb
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	srcNodeID, srcOK, err := s.resolveNodeIDByProps(ctx, srcLabel, pid, map[string]any{"qdrant_id": srcID})
	if err != nil {
		return nil, err
	}
	dstNodeID, dstOK, err := s.resolveNodeIDByProps(ctx, dstLabel, pid, map[string]any{"qdrant_id": dstID})
	if err != nil {
		return nil, err
	}
	if !srcOK || !dstOK {
		return emptyResult(), nil
	}
	return s.writeEdge(ctx, srcNodeID, dstNodeID, edgeType, pid, edgeProps, true)
}

// writeEdge inserts (or, when merge, upserts) an edge between two resolved
// nodes. For MERGE semantics it locates an existing (src, type, dst) edge and
// merges props (SET r += props); CREATE always inserts a new edge.
func (s *sqliteStore) writeEdge(ctx context.Context, srcID, dstID int64, edgeType, pid string, props map[string]any, merge bool) (*GraphResult, error) {
	if merge {
		existingID, existingProps, found, err := s.findEdge(ctx, srcID, dstID, edgeType, pid)
		if err != nil {
			return nil, err
		}
		if found {
			for k, v := range props {
				existingProps[k] = v
			}
			encoded, encErr := encodeProps(existingProps)
			if encErr != nil {
				return nil, encErr
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE edges SET props = ? WHERE internal_id = ?`, encoded, existingID); err != nil {
				return nil, fmt.Errorf("merge edge %s: %w", edgeType, err)
			}
			return emptyResult(), nil
		}
	}
	encoded, err := encodeProps(props)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO edges(src, dst, type, project_id, props) VALUES(?, ?, ?, ?, ?)`,
		srcID, dstID, edgeType, pid, encoded); err != nil {
		return nil, fmt.Errorf("create edge %s: %w", edgeType, err)
	}
	return emptyResult(), nil
}

// findEdge locates an existing (src, type, dst) edge in the project and returns
// its id and decoded props.
func (s *sqliteStore) findEdge(ctx context.Context, srcID, dstID int64, edgeType, pid string) (int64, map[string]any, bool, error) {
	var id int64
	var propsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT internal_id, props FROM edges WHERE project_id = ? AND src = ? AND dst = ? AND type = ? LIMIT 1`,
		pid, srcID, dstID, edgeType).Scan(&id, &propsJSON)
	if err == sql.ErrNoRows {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("find edge %s: %w", edgeType, err)
	}
	props, decErr := decodeProps(propsJSON)
	if decErr != nil {
		return 0, nil, false, decErr
	}
	return id, props, true, nil
}

// assignTaskToWave handles
// "MATCH (t:Task {qdrant_id: $task_ref, project_id: $pid})
//
//	MATCH (w:Wave {id: $wave_id, project_id: $pid}) MERGE (t)-[:IN_WAVE]->(w)".
func (s *sqliteStore) assignTaskToWave(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	taskRef, _ := params["task_ref"].(string)
	waveID, _ := params["wave_id"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	taskID, tOK, err := s.resolveNodeIDByProps(ctx, LabelTask, pid, map[string]any{"qdrant_id": taskRef})
	if err != nil {
		return nil, err
	}
	waveNodeID, wOK, err := s.resolveNodeIDByProps(ctx, LabelWave, pid, map[string]any{"id": waveID})
	if err != nil {
		return nil, err
	}
	if !tOK || !wOK {
		return emptyResult(), nil
	}
	return s.writeEdge(ctx, taskID, waveNodeID, RelInWave, pid, map[string]any{"project_id": pid}, true)
}

// deleteAffectsEdges handles
// "MATCH (b:Bug {bug_id: $bug_id, project_id: $pid})-[r:AFFECTS]->() DELETE r".
func (s *sqliteStore) deleteAffectsEdges(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	bugID, _ := params["bug_id"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	srcID, ok, err := s.resolveNodeIDByProps(ctx, LabelBug, pid, map[string]any{"bug_id": bugID})
	if err != nil {
		return nil, err
	}
	if !ok {
		return emptyResult(), nil
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM edges WHERE project_id = ? AND src = ? AND type = ?`,
		pid, srcID, RelAffects); err != nil {
		return nil, fmt.Errorf("delete AFFECTS edges: %w", err)
	}
	return emptyResult(), nil
}
