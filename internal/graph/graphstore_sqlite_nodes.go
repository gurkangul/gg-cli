package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

// pidOf returns the project_id carried in params (injected by Client.runQuery).
func pidOf(params map[string]any) string {
	if v, ok := params["pid"].(string); ok {
		return v
	}
	return ""
}

// encodeProps JSON-encodes a property map for the props column. A nil map
// becomes "{}" so the column is never NULL.
func encodeProps(p map[string]any) (string, error) {
	if p == nil {
		return "{}", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode props: %w", err)
	}
	return string(b), nil
}

// decodeProps parses a props column back into a property map.
func decodeProps(s string) (map[string]any, error) {
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, fmt.Errorf("decode props: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// normalizeProps coerces JSON-decoded numeric values to int64 when they have no
// fractional part, matching the neo4j driver's behavior of returning graph
// integers as int64. This keeps recordValue[int64] and downstream consumers
// (e.g. line numbers on edge props) backend-identical.
func normalizeProps(m map[string]any) map[string]any {
	for k, v := range m {
		if f, ok := v.(float64); ok && f == float64(int64(f)) {
			m[k] = int64(f)
		}
	}
	return m
}

// idResult builds a single-row "id" result mirroring
// "RETURN toString(id(n)) AS id".
func idResult(internalID int64) *GraphResult {
	rec := GraphRecord{keys: []string{"id"}, values: []any{strconv.FormatInt(internalID, 10)}}
	return newGraphResult([]string{"id"}, []GraphRecord{rec})
}

// createNode handles "CREATE (n:LABEL $props) RETURN toString(id(n)) AS id".
func (s *sqliteStore) createNode(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	label := firstSubmatch(createNodeLabel, normalizeCypher(cypher))
	if label == "" {
		return nil, unrecognizedCypher(cypher)
	}
	props, _ := params["props"].(map[string]any)
	pid := propStr(props, "project_id")
	if pid == "" {
		pid = pidOf(params)
	}
	encoded, err := encodeProps(props)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.execWrite(ctx,
		`INSERT INTO nodes(label, project_id, props) VALUES(?, ?, ?)`,
		label, pid, encoded)
	if err != nil {
		return nil, fmt.Errorf("create node %s: %w", label, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return idResult(id), nil
}

// mergeNode handles
// "MERGE (n:LABEL {k1: $id_k1, ...}) SET n += $props RETURN toString(id(n)) AS id".
// The identity is (label, project_id, the merge-key props); on conflict it
// updates the full props map, mirroring Memgraph's "MERGE ... SET n += $props".
func (s *sqliteStore) mergeNode(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	nc := normalizeCypher(cypher)
	m := mergeNodeLabel.FindStringSubmatch(nc)
	if len(m) < 3 {
		return nil, unrecognizedCypher(cypher)
	}
	label := m[1]
	identity := parseIdentityClause(m[2], params)
	props, _ := params["props"].(map[string]any)
	pid := propStr(props, "project_id")
	if pid == "" {
		pid = pidOf(params)
	}
	encoded, err := encodeProps(props)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existingID, found, err := s.findNodeIDByIdentity(ctx, label, pid, identity)
	if err != nil {
		return nil, err
	}
	if found {
		if _, err := s.execWrite(ctx,
			`UPDATE nodes SET props = ? WHERE internal_id = ?`, encoded, existingID); err != nil {
			return nil, fmt.Errorf("merge node %s update: %w", label, err)
		}
		return idResult(existingID), nil
	}
	res, err := s.execWrite(ctx,
		`INSERT INTO nodes(label, project_id, props) VALUES(?, ?, ?)`, label, pid, encoded)
	if err != nil {
		return nil, fmt.Errorf("merge node %s insert: %w", label, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return idResult(id), nil
}

// findNodeIDByIdentity returns the internal_id of the node matching (label,
// project_id, identity props), if any. identity values are matched against the
// JSON props via json_extract so the same identity semantics as Memgraph's
// MERGE key apply.
func (s *sqliteStore) findNodeIDByIdentity(ctx context.Context, label, pid string, identity map[string]any) (int64, bool, error) {
	q := `SELECT internal_id FROM nodes WHERE label = ? AND project_id = ?`
	args := []any{label, pid}
	for k, v := range identity {
		if k == "project_id" {
			continue
		}
		q += ` AND json_extract(props, '$.' || ?) = ?`
		args = append(args, k, v)
	}
	q += ` LIMIT 1`
	var id int64
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("find node %s by identity: %w", label, err)
	}
	return id, true, nil
}

// deleteNodeByID handles
// "MATCH (n) WHERE toString(id(n)) = $id AND n.project_id = $pid DETACH DELETE n".
// The project_id guard makes cross-project deletion a structural no-op. The
// ON DELETE CASCADE on edges removes incident relationships (DETACH semantics).
func (s *sqliteStore) deleteNodeByID(ctx context.Context, params map[string]any) (*GraphResult, error) {
	idStr, _ := params["id"].(string)
	id, convErr := strconv.ParseInt(idStr, 10, 64)
	if convErr != nil {
		// A non-numeric id never matches a SQLite internal_id — no-op, as in
		// Memgraph when the element id belongs to nothing.
		return emptyResult(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.execWrite(ctx,
		`DELETE FROM nodes WHERE internal_id = ? AND project_id = ?`, id, pidOf(params)); err != nil {
		return nil, fmt.Errorf("delete node by id: %w", err)
	}
	return emptyResult(), nil
}

// deleteNodesByProps handles the property-keyed DETACH DELETE templates
// (InvalidateFile's Symbol/File deletes and DeleteTaskNode).
func (s *sqliteStore) deleteNodesByProps(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	nc := normalizeCypher(cypher)
	label := firstSubmatch(detachDeleteByProps, nc)
	if label == "" {
		return nil, unrecognizedCypher(cypher)
	}
	identity := parseNodePatternProps(nc, params)
	s.mu.Lock()
	defer s.mu.Unlock()
	q := `DELETE FROM nodes WHERE label = ? AND project_id = ?`
	args := []any{label, pidOf(params)}
	for k, v := range identity {
		q += ` AND json_extract(props, '$.' || ?) = ?`
		args = append(args, k, v)
	}
	if _, err := s.execWrite(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("delete %s by props: %w", label, err)
	}
	return emptyResult(), nil
}

// sweepProject handles "MATCH (n {project_id: $pid}) DETACH DELETE n".
func (s *sqliteStore) sweepProject(ctx context.Context, params map[string]any) (*GraphResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.execWrite(ctx, `DELETE FROM nodes WHERE project_id = ?`, pidOf(params)); err != nil {
		return nil, fmt.Errorf("sweep project: %w", err)
	}
	// Edges are removed by ON DELETE CASCADE; also clear any project-scoped
	// edges whose endpoints were already gone (defensive — keeps stats exact).
	if _, err := s.execWrite(ctx, `DELETE FROM edges WHERE project_id = ?`, pidOf(params)); err != nil {
		return nil, fmt.Errorf("sweep project edges: %w", err)
	}
	return emptyResult(), nil
}

// sweepLang handles "MATCH (n {project_id: $pid, lang: $lang}) DETACH DELETE n".
func (s *sqliteStore) sweepLang(ctx context.Context, params map[string]any) (*GraphResult, error) {
	lang, _ := params["lang"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.execWrite(ctx,
		`DELETE FROM nodes WHERE project_id = ? AND json_extract(props, '$.lang') = ?`,
		pidOf(params), lang); err != nil {
		return nil, fmt.Errorf("sweep project lang %s: %w", lang, err)
	}
	return emptyResult(), nil
}

// emptyResult is a zero-row result for write statements that return nothing.
func emptyResult() *GraphResult { return newGraphResult(nil, nil) }

// propStr returns the string value of key in m, or "".
func propStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
