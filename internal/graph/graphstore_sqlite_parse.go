package graph

import "strings"

// parseIdentityClause resolves a MERGE identity clause like
// "k1: $id_k1, project_id: $id_project_id" into a {key: value} map by looking
// each $param up in params. Keys whose param is absent are skipped.
func parseIdentityClause(clause string, params map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range propClause.FindAllStringSubmatch(clause, -1) {
		key, param := m[1], m[2]
		if v, ok := params[param]; ok {
			out[key] = v
		}
	}
	return out
}

// parseNodePatternProps resolves the inline property map of a node pattern like
// "(n:Symbol {source_file: $path, project_id: $pid})" into a {key: value} map
// (excluding project_id, which the caller filters separately). Values are
// resolved from params by their $param name.
func parseNodePatternProps(cypher string, params map[string]any) map[string]any {
	out := map[string]any{}
	open := strings.Index(cypher, "{")
	close := strings.Index(cypher, "}")
	if open < 0 || close < 0 || close < open {
		return out
	}
	inner := cypher[open+1 : close]
	for _, m := range propClause.FindAllStringSubmatch(inner, -1) {
		key, param := m[1], m[2]
		if key == "project_id" {
			continue
		}
		if v, ok := params[param]; ok {
			out[key] = v
		}
	}
	return out
}

// parseWhereEqualityClauses resolves a WHERE clause of the form
// "a.k1 = $src_k1 AND a.k2 = $src_k2" into a {bareKey: value} map for the given
// node alias prefix (e.g. alias "a" yields keys without the "a." prefix). Values
// are resolved from params by their $param name.
func parseWhereEqualityClauses(cypher, alias string, params map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range whereEqRE.FindAllStringSubmatch(cypher, -1) {
		gotAlias, key, param := m[1], m[2], m[3]
		if gotAlias != alias {
			continue
		}
		if v, ok := params[param]; ok {
			out[key] = v
		}
	}
	return out
}
