package graph

import (
	"context"
	"strconv"
	"strings"
)

// runRead dispatches the package's read templates. `c` is the normalized Cypher;
// `cypher` is the original (used where a label must be extracted).
func (s *sqliteStore) runRead(ctx context.Context, c, cypher string, params map[string]any) (*GraphResult, error) {
	switch {
	case strings.Contains(c, "RETURN count("):
		return s.countQuery(ctx, c, params)
	case strings.HasPrefix(c, "MATCH (n:") && strings.Contains(c, "LIMIT 1") && strings.Contains(c, "AS props"):
		return s.findNodeByProperty(ctx, cypher, params)
	case strings.HasPrefix(c, "MATCH (n:Symbol {source_file: $path"):
		return s.fileSymbols(ctx, params)
	case strings.HasPrefix(c, "MATCH (s:Symbol {name: $name"):
		return s.findSymbols(ctx, params)
	case strings.HasPrefix(c, "MATCH (n:Symbol {boundary: true"):
		return s.boundarySymbols(ctx, params)
	case strings.Contains(c, "-[:CALLS]->(to:Symbol"):
		return s.callees(ctx, params)
	case strings.Contains(c, "-[:IMPORTS]->(f:File {path: $path"):
		return s.dependentsOf(ctx, params)
	case strings.Contains(c, "(b:Bug {project_id: $pid})-[:AFFECTS]->(f:File {path: $path"):
		return s.bugsAffectingFile(ctx, params)
	case strings.Contains(c, "(b:Bug {bug_id: $bug_id, project_id: $pid})-[:AFFECTS]->(target)"):
		return s.bugAffects(ctx, params)
	case strings.Contains(c, "[:DEPENDS_ON]->(t:Task {qdrant_id: $tid") && strings.Contains(c, "[:BLOCKS]->"):
		return s.taskDependents(ctx, params)
	case strings.Contains(c, "[:IMPLEMENTS]->(d:Decision") && strings.Contains(c, "[:DECIDES]->"):
		return s.taskSiblings(ctx, params)
	case strings.HasPrefix(c, "MATCH (w:Wave {project_id: $pid, status: $status})"):
		return s.listWaves(ctx, params, true)
	case strings.HasPrefix(c, "MATCH (w:Wave {project_id: $pid}) RETURN w"):
		return s.listWaves(ctx, params, false)
	case strings.HasPrefix(c, "MATCH (w:Wave {id: $wave_id, project_id: $pid}) OPTIONAL MATCH"):
		return s.waveStatus(ctx, params)
	case strings.HasPrefix(c, "MATCH (n {project_id: $pid}) RETURN labels(n)"):
		return s.exportNodes(ctx, params)
	case strings.Contains(c, "MATCH (src {project_id: $pid})-[r]->(dst {project_id: $pid})"):
		return s.exportEdges(ctx, params)
	default:
		return nil, unrecognizedCypher(cypher)
	}
}

// scalarResult builds a single-row "n" int64 count result.
func scalarResult(n int64) *GraphResult {
	rec := GraphRecord{keys: []string{"n"}, values: []any{n}}
	return newGraphResult([]string{"n"}, []GraphRecord{rec})
}

// idPropsResult builds a result of (id string, props map) rows, mirroring
// "RETURN toString(id(n)) AS id, properties(n) AS props".
func idPropsResult(rows []nodeRow) *GraphResult {
	keys := []string{"id", "props"}
	recs := make([]GraphRecord, 0, len(rows))
	for _, r := range rows {
		recs = append(recs, GraphRecord{
			keys:   keys,
			values: []any{strconv.FormatInt(r.id, 10), normalizeProps(r.props)},
		})
	}
	return newGraphResult(keys, recs)
}

// nodeRow is an internal node read result.
type nodeRow struct {
	id    int64
	label string
	props map[string]any
}

// queryNodes runs a node SELECT and decodes each row's props.
func (s *sqliteStore) queryNodes(ctx context.Context, sqlText string, args ...any) ([]nodeRow, error) {
	rows, err := s.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []nodeRow
	for rows.Next() {
		var id int64
		var label, propsJSON string
		if err := rows.Scan(&id, &label, &propsJSON); err != nil {
			return nil, err
		}
		props, decErr := decodeProps(propsJSON)
		if decErr != nil {
			return nil, decErr
		}
		out = append(out, nodeRow{id: id, label: label, props: props})
	}
	return out, rows.Err()
}

// countQuery handles the three count templates (File / Symbol nodes; all edges)
// plus the brain-edge typed count.
func (s *sqliteStore) countQuery(ctx context.Context, c string, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	switch {
	case strings.Contains(c, "(f:File {project_id: $pid}) RETURN count(f)"):
		return s.countNodes(ctx, LabelFile, pid)
	case strings.Contains(c, "(s:Symbol {project_id: $pid}) RETURN count(s)"):
		return s.countNodes(ctx, LabelSymbol, pid)
	case strings.Contains(c, "(n:Decision {project_id: $pid}) RETURN count(n)"):
		return s.countNodes(ctx, LabelDecision, pid)
	case strings.Contains(c, "WHERE type(r) IN ["):
		return s.countBrainEdges(ctx, c, pid)
	case strings.Contains(c, "()-[r {project_id: $pid}]->() RETURN count(r)"):
		return s.countAllEdges(ctx, pid)
	default:
		return nil, unrecognizedCypher(c)
	}
}

func (s *sqliteStore) countNodes(ctx context.Context, label, pid string) (*GraphResult, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM nodes WHERE label = ? AND project_id = ?`, label, pid).Scan(&n); err != nil {
		return nil, err
	}
	return scalarResult(n), nil
}

func (s *sqliteStore) countAllEdges(ctx context.Context, pid string) (*GraphResult, error) {
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM edges WHERE project_id = ?`, pid).Scan(&n); err != nil {
		return nil, err
	}
	return scalarResult(n), nil
}

// countBrainEdges counts edges whose type is in the IN-list embedded in the
// Cypher (DECIDES, DEPENDS_ON, BLOCKS, IMPLEMENTS, REJECTS).
func (s *sqliteStore) countBrainEdges(ctx context.Context, c, pid string) (*GraphResult, error) {
	types := parseEdgeTypeInList(c)
	if len(types) == 0 {
		return scalarResult(0), nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(types)), ",")
	args := []any{pid}
	for _, t := range types {
		args = append(args, t)
	}
	var n int64
	q := `SELECT count(*) FROM edges WHERE project_id = ? AND type IN (` + placeholders + `)`
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return nil, err
	}
	return scalarResult(n), nil
}

// findNodeByProperty handles
// "MATCH (n:LABEL {KEY: $val, project_id: $pid}) RETURN id, props LIMIT 1".
func (s *sqliteStore) findNodeByProperty(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	nc := normalizeCypher(cypher)
	label := firstSubmatch(matchNodeLabel, nc)
	if label == "" {
		return nil, unrecognizedCypher(cypher)
	}
	key := firstSubmatch(matchNodeKey, nc)
	val := params["val"]
	rows, err := s.queryNodes(ctx,
		`SELECT internal_id, label, props FROM nodes
		 WHERE label = ? AND project_id = ? AND json_extract(props, '$.' || ?) = ? LIMIT 1`,
		label, pidOf(params), key, val)
	if err != nil {
		return nil, err
	}
	return idPropsResult(rows), nil
}

// fileSymbols handles the Symbol-by-source_file read.
func (s *sqliteStore) fileSymbols(ctx context.Context, params map[string]any) (*GraphResult, error) {
	path, _ := params["path"].(string)
	rows, err := s.queryNodes(ctx,
		`SELECT internal_id, label, props FROM nodes
		 WHERE label = 'Symbol' AND project_id = ? AND json_extract(props, '$.source_file') = ?`,
		pidOf(params), path)
	if err != nil {
		return nil, err
	}
	return idPropsResult(rows), nil
}

// findSymbols handles the Symbol-by-name read (ordered by source_file, name).
func (s *sqliteStore) findSymbols(ctx context.Context, params map[string]any) (*GraphResult, error) {
	name, _ := params["name"].(string)
	rows, err := s.queryNodes(ctx,
		`SELECT internal_id, label, props FROM nodes
		 WHERE label = 'Symbol' AND project_id = ? AND json_extract(props, '$.name') = ?
		 ORDER BY json_extract(props, '$.source_file'), json_extract(props, '$.name')`,
		pidOf(params), name)
	if err != nil {
		return nil, err
	}
	return idPropsResult(rows), nil
}

// boundarySymbols handles the boundary=true, resolution=$tier read.
func (s *sqliteStore) boundarySymbols(ctx context.Context, params map[string]any) (*GraphResult, error) {
	tier, _ := params["tier"].(string)
	rows, err := s.queryNodes(ctx,
		`SELECT internal_id, label, props FROM nodes
		 WHERE label = 'Symbol' AND project_id = ?
		   AND json_extract(props, '$.boundary') = 1
		   AND json_extract(props, '$.resolution') = ?`,
		pidOf(params), tier)
	if err != nil {
		return nil, err
	}
	return idPropsResult(rows), nil
}
