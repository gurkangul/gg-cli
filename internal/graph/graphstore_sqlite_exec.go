package graph

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Run dispatches one of the package's fixed Cypher templates to its SQL handler.
//
// Matching strategy: the template set is closed (see queries.go consumers), so
// each Cypher is routed by stable structural markers rather than interpreted as
// a general query language. Parametric templates (built with fmt.Sprintf and an
// interpolated label / relationship type) have their label/type extracted with
// the small regexes below. An unrecognized Cypher is a programming error — a new
// query was added without a SQLite handler — and surfaces loudly so the parity
// gap cannot pass silently.
func (s *sqliteStore) Run(ctx context.Context, cypher string, params map[string]any) (*GraphResult, error) {
	c := normalizeCypher(cypher)
	switch {
	// --- node writes / deletes ---
	case strings.HasPrefix(c, "CREATE (n:") && strings.Contains(c, "$props) RETURN"):
		return s.createNode(ctx, cypher, params)
	case strings.HasPrefix(c, "MERGE (n:") && strings.Contains(c, "SET n += $props RETURN"):
		return s.mergeNode(ctx, cypher, params)
	case strings.HasPrefix(c, "MATCH (n) WHERE toString(id(n)) = $id AND n.project_id = $pid DETACH DELETE"):
		return s.deleteNodeByID(ctx, params)
	case strings.HasPrefix(c, "MATCH (n {project_id: $pid, lang: $lang}) DETACH DELETE"):
		return s.sweepLang(ctx, params)
	case strings.HasPrefix(c, "MATCH (n {project_id: $pid}) DETACH DELETE"):
		return s.sweepProject(ctx, params)
	case detachDeleteByProps.MatchString(c):
		return s.deleteNodesByProps(ctx, cypher, params)

	// --- edge writes / deletes ---
	case strings.Contains(c, "-[r:AFFECTS]->() DELETE r"):
		return s.deleteAffectsEdges(ctx, params)
	case strings.Contains(c, "WHERE toString(id(a)) = $from") && strings.Contains(c, "CREATE (a)-[r:"):
		return s.createEdgeByID(ctx, cypher, params)
	case strings.Contains(c, "WHERE toString(id(a)) = $from") && strings.Contains(c, "MERGE (a)-[r:"):
		return s.upsertEdgeByID(ctx, cypher, params)
	case strings.Contains(c, "MATCH (a:") && strings.Contains(c, "MERGE (a)-[r:") && strings.Contains(c, "$rprops"):
		return s.upsertEdgeByKey(ctx, cypher, params)
	case strings.Contains(c, "qdrant_id: $src_id") && strings.Contains(c, "MERGE (a)-[r:"):
		return s.upsertKnowledgeEdge(ctx, cypher, params)
	case strings.Contains(c, "(t:Task {qdrant_id: $task_ref") && strings.Contains(c, "MERGE (t)-[:IN_WAVE]->(w)"):
		return s.assignTaskToWave(ctx, params)
	case strings.HasPrefix(c, "MERGE (w:Wave {id: $wave_id, project_id: $pid}) SET"):
		return s.upsertWave(ctx, params)

	// --- reads ---
	default:
		return s.runRead(ctx, c, cypher, params)
	}
}

// normalizeCypher collapses internal whitespace (including the newlines that
// fmt.Sprintf templates embed) to single spaces and trims the ends, so the
// structural markers above match regardless of formatting.
func normalizeCypher(cypher string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(cypher, " "))
}

var (
	whitespaceRun = regexp.MustCompile(`\s+`)

	// detachDeleteByProps matches the property-keyed node deletes
	// (InvalidateFile's Symbol/File deletes, DeleteTaskNode) of the shape
	//   MATCH (x:Label {k: $p, project_id: $pid}) DETACH DELETE x
	detachDeleteByProps = regexp.MustCompile(`^MATCH \(\w+:(\w+) \{.*project_id: \$pid\}\) DETACH DELETE \w+$`)

	// createNodeLabel extracts LABEL from "CREATE (n:LABEL $props) RETURN ...".
	createNodeLabel = regexp.MustCompile(`^CREATE \(n:(\w+) \$props\)`)
	// mergeNodeLabel extracts LABEL and the identity-key clause from
	//   "MERGE (n:LABEL {k1: $id_k1, ...}) SET n += $props RETURN ..."
	mergeNodeLabel = regexp.MustCompile(`^MERGE \(n:(\w+) \{(.*?)\}\) SET n \+= \$props`)
	// edgeTypeCreate / edgeTypeMerge extract the relationship TYPE.
	edgeTypeCreate = regexp.MustCompile(`CREATE \(a\)-\[r:(\w+) `)
	edgeTypeMerge  = regexp.MustCompile(`MERGE \(a\)-\[r:(\w+)\]`)
	// labelClauseSrc / labelClauseDst extract LABEL for the by-key edge upsert.
	labelClauseSrc = regexp.MustCompile(`MATCH \(a:(\w+) \{project_id: \$pid\}\)`)
	labelClauseDst = regexp.MustCompile(`MATCH \(b:(\w+) \{project_id: \$pid\}\)`)
	// knowledgeEdge extracts src LABEL, dst LABEL and the relationship TYPE for
	// the upsertKnowledgeEdge / upsertBackfillEdge templates.
	knowledgeEdgeRE = regexp.MustCompile(`MATCH \(a:(\w+) \{qdrant_id: \$src_id, project_id: \$pid\}\) MATCH \(b:(\w+) \{qdrant_id: \$dst_id, project_id: \$pid\}\) MERGE \(a\)-\[r:(\w+)\]`)
	// propClause matches one "key: $param" pair inside a {...} identity map.
	propClause = regexp.MustCompile(`(\w+): \$(\w+)`)
	// whereEqRE matches "alias.key = $param" equality clauses in a WHERE.
	whereEqRE = regexp.MustCompile(`(\w+)\.(\w+) = \$(\w+)`)
	// matchNodeLabel / matchNodeKey extract LABEL and the single property key
	// from "MATCH (n:LABEL {KEY: $val, project_id: $pid}) ...".
	matchNodeLabel = regexp.MustCompile(`^MATCH \(n:(\w+) \{`)
	matchNodeKey   = regexp.MustCompile(`^MATCH \(n:\w+ \{(\w+): \$val`)
	// edgeTypeInList captures the bracketed type list of a brain-edge count, e.g.
	//   WHERE type(r) IN ['DECIDES','DEPENDS_ON',...]
	edgeTypeInList = regexp.MustCompile(`type\(r\) IN \[(.*?)\]`)
	// quotedToken pulls each 'TYPE' out of the IN-list.
	quotedToken = regexp.MustCompile(`'([^']+)'`)
)

// parseEdgeTypeInList extracts the relationship types from a
// "type(r) IN ['A','B',...]" clause in the given (normalized) Cypher.
func parseEdgeTypeInList(cypher string) []string {
	m := edgeTypeInList.FindStringSubmatch(cypher)
	if len(m) < 2 {
		return nil
	}
	var types []string
	for _, t := range quotedToken.FindAllStringSubmatch(m[1], -1) {
		types = append(types, t[1])
	}
	return types
}

// firstSubmatch returns the first capture group of re against s, or "".
func firstSubmatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// unrecognizedCypher reports a Cypher template that has no SQLite handler.
func unrecognizedCypher(cypher string) error {
	return fmt.Errorf("sqlite graph backend: unrecognized Cypher template (no handler): %q", normalizeCypher(cypher))
}
