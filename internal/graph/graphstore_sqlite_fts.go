package graph

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// FTS5 lexical symbol index (TASK-505).
//
// The semantic vector store ranks code symbols by embedding similarity, which
// loses to a plain keyword match on exact-identifier lookups (e.g. searching
// "ForwardCallFlow" should surface the symbol of that name even when its
// embedding neighbourhood is crowded). This file adds a lexical tier: an FTS5
// virtual table over Symbol nodes, ranked by bm25(), populated as a side effect
// of the existing node-write path so it is always rebuild-safe derived data.
//
// camelCase handling: the default unicode61 tokenizer treats "ForwardCallFlow"
// as a single token, so "Forward" would not match. We split identifiers at
// case/digit boundaries at index time and store the component words alongside
// the raw name in the `terms` column. That makes "complex" match
// "complexOperation" without a custom tokenizer (re-implements WrongStack's
// camelCase-FTS idea in pure Go over modernc.org/sqlite — no new dependency).

const (
	// ftsSearchSentinel is a routing marker (not executable Cypher) that the Run
	// dispatcher recognises and routes to the FTS handler. Symbol-name lexical
	// search has no Cypher analogue, so the package's choke-point (queries.go)
	// passes this sentinel as the "cypher" with the real query carried in params.
	// Memgraph never saw such a query; the embedded SQLite store is the only
	// backend, so this stays within the package's own template set.
	ftsSearchSentinel = "FTS_SYMBOL_SEARCH"

	// symbolsFTSTable is the FTS5 virtual table backing lexical symbol search.
	symbolsFTSTable = "symbols_fts"

	// symbolsFTSSchema creates the derived FTS index. `nid` mirrors the owning
	// nodes.internal_id (UNINDEXED — stored for join-back, not searched). `pid`
	// scopes results to a project. `terms` carries the searchable text (raw
	// identifier + camelCase-split words + kind + source file).
	symbolsFTSSchema = `CREATE VIRTUAL TABLE IF NOT EXISTS ` + symbolsFTSTable + ` USING fts5(
		nid UNINDEXED,
		pid UNINDEXED,
		terms,
		tokenize='unicode61'
	);`
)

// SearchSymbolsFTS returns Symbol nodes whose lexical terms match the free-text
// query, bm25()-ranked best-first and capped at limit. It is the lexical tier
// `gg search` layers alongside the semantic vector results: an exact identifier
// like "ForwardCallFlow" — or a camelCase fragment like "complex" matching
// "complexOperation" — surfaces here even when its embedding ranks poorly.
//
// Goes through the package choke-point (runQuery) so project_id is injected and
// the read is scoped to this client's project. A blank query or an empty FTS
// index returns no matches without error.
func (c *Client) SearchSymbolsFTS(ctx context.Context, query string, limit int) ([]SymbolMatch, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	result, cleanup, err := c.runQuery(ctx, ftsSearchSentinel,
		map[string]any{"fts_query": query, "limit": int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("search symbols FTS %q: %w", query, err)
	}
	defer cleanup()

	var matches []SymbolMatch
	for result.Next(ctx) {
		m := symbolMatchFromRecord(result.Record())
		if m.Name != "" {
			matches = append(matches, m)
		}
	}
	if err := result.Err(); err != nil {
		return nil, fmt.Errorf("search symbols FTS %q iterate: %w", query, err)
	}
	return matches, nil
}

// initFTS creates the FTS5 virtual table and backfills it from any pre-existing
// Symbol nodes. Separated from initSchema so a build of modernc.org/sqlite
// without the FTS5 extension surfaces a clear error at store-open time rather
// than a confusing failure on first symbol write.
//
// Backfill makes the lexical tier available to graphs indexed before TASK-505
// without forcing a full re-index: when the FTS table is empty but Symbol nodes
// exist (an upgraded brain), reproject every symbol into the index once. New
// and re-indexed symbols flow through syncSymbolFTS on write, so the backfill
// runs at most once per upgraded store.
func (s *sqliteStore) initFTS() error {
	if _, err := s.db.Exec(symbolsFTSSchema); err != nil {
		return fmt.Errorf("init symbols FTS5 table: %w", err)
	}
	return s.backfillFTSIfEmpty(context.Background())
}

// backfillFTSIfEmpty reprojects all existing Symbol nodes into the FTS index
// when the index is empty but symbols already exist. No-op on a fresh store (no
// symbols) and on an already-populated index.
func (s *sqliteStore) backfillFTSIfEmpty(ctx context.Context) error {
	var ftsRows, symRows int64
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+symbolsFTSTable).Scan(&ftsRows); err != nil {
		return fmt.Errorf("count FTS rows: %w", err)
	}
	if ftsRows > 0 {
		return nil
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM nodes WHERE label = ?`, LabelSymbol).Scan(&symRows); err != nil {
		return fmt.Errorf("count Symbol nodes: %w", err)
	}
	if symRows == 0 {
		return nil
	}
	type pending struct {
		id    int64
		pid   string
		props map[string]any
	}
	var batch []pending
	// Read the full symbol set and CLOSE the cursor before any write: the store
	// pins a single connection (SetMaxOpenConns(1)), so an open read cursor would
	// deadlock the writes below. Materialize first, then write.
	if err := func() error {
		rows, err := s.db.QueryContext(ctx,
			`SELECT internal_id, project_id, props FROM nodes WHERE label = ?`, LabelSymbol)
		if err != nil {
			return fmt.Errorf("backfill FTS scan symbols: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var pid, propsJSON string
			if err := rows.Scan(&id, &pid, &propsJSON); err != nil {
				return fmt.Errorf("backfill FTS scan row: %w", err)
			}
			props, decErr := decodeProps(propsJSON)
			if decErr != nil {
				return decErr
			}
			batch = append(batch, pending{id: id, pid: pid, props: props})
		}
		return rows.Err()
	}(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range batch {
		if err := s.syncSymbolFTS(ctx, LabelSymbol, p.pid, p.id, p.props); err != nil {
			return err
		}
	}
	return nil
}

// syncSymbolFTS refreshes the FTS row for a Symbol node after a write. It is a
// no-op for non-Symbol labels. Caller holds s.mu (it runs inside createNode /
// mergeNode, which already serialize writes). The row is keyed by nid, so an
// update deletes the prior row and re-inserts the current terms — keeping the
// index a faithful projection of the node's current props.
func (s *sqliteStore) syncSymbolFTS(ctx context.Context, label, pid string, nid int64, props map[string]any) error {
	if label != LabelSymbol {
		return nil
	}
	terms := buildSymbolTerms(props)
	if terms == "" {
		// Nothing searchable (no name) — drop any stale row and stop.
		return s.deleteSymbolFTS(ctx, nid)
	}
	if err := s.deleteSymbolFTS(ctx, nid); err != nil {
		return err
	}
	if _, err := s.execWrite(ctx,
		`INSERT INTO `+symbolsFTSTable+`(nid, pid, terms) VALUES(?, ?, ?)`,
		nid, pid, terms); err != nil {
		return fmt.Errorf("sync symbol FTS insert: %w", err)
	}
	return nil
}

// deleteSymbolFTS removes the FTS row for a node id. Safe when no row exists.
func (s *sqliteStore) deleteSymbolFTS(ctx context.Context, nid int64) error {
	if _, err := s.execWrite(ctx,
		`DELETE FROM `+symbolsFTSTable+` WHERE nid = ?`, nid); err != nil {
		return fmt.Errorf("delete symbol FTS row: %w", err)
	}
	return nil
}

// reapSymbolFTS removes FTS rows whose owning node no longer exists. The node
// deletes in this package run as plain SQL DELETEs (no SQLite triggers wired to
// the virtual table), so the derived index can outlive its nodes after a sweep
// or file invalidation. Calling this after a delete keeps the index exact; it
// is a cheap anti-join bounded by the project's symbol count.
func (s *sqliteStore) reapSymbolFTS(ctx context.Context, pid string) error {
	if _, err := s.execWrite(ctx,
		`DELETE FROM `+symbolsFTSTable+`
		 WHERE pid = ? AND nid NOT IN (SELECT internal_id FROM nodes WHERE label = ? AND project_id = ?)`,
		pid, LabelSymbol, pid); err != nil {
		return fmt.Errorf("reap symbol FTS rows: %w", err)
	}
	return nil
}

// buildSymbolTerms assembles the searchable text for a Symbol node: the raw
// identifier, its camelCase/underscore-split component words, the kind, and the
// source file basename. Duplicate-free and lower-cased by the tokenizer.
func buildSymbolTerms(props map[string]any) string {
	name := propStr(props, "name")
	if name == "" {
		return ""
	}
	seen := map[string]bool{}
	var parts []string
	add := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" || seen[strings.ToLower(tok)] {
			return
		}
		seen[strings.ToLower(tok)] = true
		parts = append(parts, tok)
	}
	add(name)
	for _, w := range splitIdentifier(name) {
		add(w)
	}
	if kind := propStr(props, "kind"); kind != "" {
		add(kind)
	}
	if sf := propStr(props, "source_file"); sf != "" {
		if i := strings.LastIndexByte(sf, '/'); i >= 0 {
			sf = sf[i+1:]
		}
		add(sf)
		// also expose the file stem (drop one extension) as a word
		if dot := strings.IndexByte(sf, '.'); dot > 0 {
			add(sf[:dot])
		}
	}
	return strings.Join(parts, " ")
}

// splitIdentifier breaks a code identifier into component words at camelCase,
// digit, and separator (_ - . :) boundaries. "ForwardCallFlow" -> [Forward Call
// Flow]; "complexOperation" -> [complex Operation]; "HTTPServer" -> [HTTP
// Server]; "do_thing_v2" -> [do thing v2]. Single-character fragments are kept
// (they may be a meaningful prefix) but the all-runs logic avoids empty tokens.
func splitIdentifier(s string) []string {
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ':' || r == ' ' || r == '/':
			flush()
			continue
		case i > 0 && unicode.IsUpper(r) && !unicode.IsUpper(runes[i-1]):
			// lower→Upper boundary: "callFlow" -> call|Flow
			flush()
		case i > 0 && unicode.IsUpper(r) && unicode.IsUpper(runes[i-1]) &&
			i+1 < len(runes) && unicode.IsLower(runes[i+1]):
			// acronym→Word boundary: "HTTPServer" -> HTTP|Server
			flush()
		case i > 0 && unicode.IsDigit(r) && !unicode.IsDigit(runes[i-1]):
			// letter→digit boundary: "v2" stays, but "thingV2" already split on V
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// ftsMatchExpr turns a free-text query into an FTS5 MATCH expression that is
// tolerant of how identifiers are typed. Each whitespace-separated term is
// (a) camelCase-split into words and (b) OR-combined with a prefix match so a
// partial identifier still hits. The raw term is quoted to neutralise FTS5
// syntax characters the user did not intend as operators.
func ftsMatchExpr(query string) string {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return ""
	}
	var ors []string
	seen := map[string]bool{}
	addTok := func(tok string, prefix bool) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			return
		}
		key := strings.ToLower(tok)
		if prefix {
			key += "*"
		}
		if seen[key] {
			return
		}
		seen[key] = true
		// Quote the token so embedded punctuation is literal; append '*' outside
		// the quotes for a prefix query (FTS5: "foo" * is invalid, "foo"* is the
		// prefix form).
		q := `"` + strings.ReplaceAll(tok, `"`, `""`) + `"`
		if prefix {
			q += "*"
		}
		ors = append(ors, q)
	}
	for _, f := range fields {
		addTok(f, false)
		addTok(f, true) // prefix variant: "Forward" matches "Forwarding"
		for _, w := range splitIdentifier(f) {
			addTok(w, false)
		}
	}
	return strings.Join(ors, " OR ")
}

// searchSymbolsFTS runs the bm25()-ranked lexical lookup over the Symbol FTS
// index for this project and joins back to the nodes table for full props. It
// is reached through the Run dispatcher (sentinel template ftsSearchSentinel)
// so the GraphStore interface stays unchanged and the choke-point in queries.go
// still owns project_id injection. Lower bm25() is a closer match; results are
// returned best-first and capped at $limit.
func (s *sqliteStore) searchSymbolsFTS(ctx context.Context, params map[string]any) (*GraphResult, error) {
	pid := pidOf(params)
	query, _ := params["fts_query"].(string)
	limit := int64(20)
	if l, ok := params["limit"].(int64); ok && l > 0 {
		limit = l
	}
	expr := ftsMatchExpr(query)
	if expr == "" {
		return idPropsResult(nil), nil
	}
	// JOIN the FTS hits to nodes for the current props; bm25() ranks the match.
	// pid is filtered on both sides so a project can never see another's symbols.
	rows, err := s.queryNodes(ctx,
		`SELECT n.internal_id, n.label, n.props
		   FROM `+symbolsFTSTable+` f
		   JOIN nodes n ON n.internal_id = f.nid
		  WHERE f.terms MATCH ? AND f.pid = ? AND n.project_id = ?
		  ORDER BY bm25(`+symbolsFTSTable+`) ASC
		  LIMIT ?`,
		expr, pid, pid, limit)
	if err != nil {
		return nil, fmt.Errorf("symbol FTS search: %w", err)
	}
	return idPropsResult(rows), nil
}
