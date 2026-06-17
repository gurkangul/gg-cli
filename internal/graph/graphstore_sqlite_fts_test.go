package graph

import (
	"context"
	"reflect"
	"testing"
)

// seedSymbol upserts a Symbol node with the given name/kind/file for FTS tests.
func seedSymbol(t *testing.T, c *Client, name, kind, file string) {
	t.Helper()
	n := &Node{Label: LabelSymbol, Properties: map[string]any{
		"name": name, "lang": "go", "kind": kind, "source_file": file,
	}}
	if err := c.UpsertNode(context.Background(), n, []string{"name", "source_file"}); err != nil {
		t.Fatalf("seed symbol %s: %v", name, err)
	}
}

// names extracts the matched symbol names for assertion.
func names(ms []SymbolMatch) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestFTS_ExactIdentifier is the AC anchor: searching the exact identifier
// surfaces the symbol via lexical match.
func TestFTS_ExactIdentifier(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-fts-exact")
	seedSymbol(t, c, "ForwardCallFlow", "method", "internal/graph/symbol_flow.go")
	seedSymbol(t, c, "BoundarySymbols", "method", "internal/graph/schema.go")

	got, err := c.SearchSymbolsFTS(ctx, "ForwardCallFlow", 10)
	if err != nil {
		t.Fatalf("SearchSymbolsFTS: %v", err)
	}
	if !contains(names(got), "ForwardCallFlow") {
		t.Fatalf("exact identifier did not surface; got %v", names(got))
	}
}

// TestFTS_CamelCaseFragment proves a camelCase component word matches the full
// identifier ("complex" -> "complexOperation"), the WrongStack behaviour.
func TestFTS_CamelCaseFragment(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-fts-camel")
	seedSymbol(t, c, "complexOperation", "function", "a.go")
	seedSymbol(t, c, "simpleThing", "function", "b.go")

	got, err := c.SearchSymbolsFTS(ctx, "complex", 10)
	if err != nil {
		t.Fatalf("SearchSymbolsFTS: %v", err)
	}
	if !contains(names(got), "complexOperation") {
		t.Fatalf("camelCase fragment did not match; got %v", names(got))
	}
	// And the inner word too: "Operation" should match the same symbol.
	got2, _ := c.SearchSymbolsFTS(ctx, "Operation", 10)
	if !contains(names(got2), "complexOperation") {
		t.Fatalf("inner camelCase word did not match; got %v", names(got2))
	}
}

// TestFTS_PrefixMatch verifies a partial identifier hits via the prefix variant.
func TestFTS_PrefixMatch(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-fts-prefix")
	seedSymbol(t, c, "ForwardCallFlow", "method", "x.go")
	got, err := c.SearchSymbolsFTS(ctx, "Forward", 10)
	if err != nil {
		t.Fatalf("SearchSymbolsFTS: %v", err)
	}
	if !contains(names(got), "ForwardCallFlow") {
		t.Fatalf("prefix match did not surface; got %v", names(got))
	}
}

// TestFTS_RebuiltOnUpsert confirms the index follows node updates: renaming a
// symbol (new merge identity) and re-indexing leaves no stale hit for the old
// name when the node is invalidated.
func TestFTS_RebuiltOnUpsert(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-fts-rebuild")
	seedSymbol(t, c, "OldName", "function", "f.go")
	if got, _ := c.SearchSymbolsFTS(ctx, "OldName", 10); !contains(names(got), "OldName") {
		t.Fatalf("OldName not indexed; got %v", names(got))
	}
	// Invalidate the file (the --changed reaping path) — FTS row must be reaped.
	if err := c.InvalidateFile(ctx, "f.go"); err != nil {
		t.Fatalf("InvalidateFile: %v", err)
	}
	if got, _ := c.SearchSymbolsFTS(ctx, "OldName", 10); len(got) != 0 {
		t.Fatalf("stale FTS hit after invalidate; got %v", names(got))
	}
}

// TestFTS_ProjectIsolation ensures one project's symbols never leak into another.
func TestFTS_ProjectIsolation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cA, err := New(dir, "proj-A")
	if err != nil {
		t.Fatalf("New A: %v", err)
	}
	t.Cleanup(func() { _ = cA.Close(ctx) })
	cB, err := New(dir, "proj-B")
	if err != nil {
		t.Fatalf("New B: %v", err)
	}
	t.Cleanup(func() { _ = cB.Close(ctx) })

	seedSymbol(t, cA, "SecretSymbol", "function", "a.go")
	got, err := cB.SearchSymbolsFTS(ctx, "SecretSymbol", 10)
	if err != nil {
		t.Fatalf("B FTS: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cross-project FTS leak: B saw %v", names(got))
	}
}

// TestSplitIdentifier covers the camelCase / acronym / separator splitter.
func TestSplitIdentifier(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"ForwardCallFlow", []string{"Forward", "Call", "Flow"}},
		{"complexOperation", []string{"complex", "Operation"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"do_thing_v2", []string{"do", "thing", "v", "2"}},
		{"plain", []string{"plain"}},
	}
	for _, tc := range cases {
		got := splitIdentifier(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitIdentifier(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestFTS_BackfillOnOpen simulates a graph indexed before the FTS tier existed:
// symbols are present but the FTS index is empty. Re-opening the store must
// backfill the index from the existing Symbol nodes, no re-index required.
func TestFTS_BackfillOnOpen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	c1, err := New(dir, "proj-backfill")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	seedSymbol(t, c1, "ForwardCallFlow", "method", "internal/graph/symbol_flow.go")
	seedSymbol(t, c1, "complexOperation", "function", "a.go")

	// Reach the underlying store and wipe the FTS index to emulate a pre-TASK-505
	// brain (symbols exist, no lexical index yet).
	ss := c1.gs.(*sqliteStore)
	if _, err := ss.db.ExecContext(ctx, `DELETE FROM `+symbolsFTSTable); err != nil {
		t.Fatalf("wipe FTS: %v", err)
	}
	if got, _ := c1.SearchSymbolsFTS(ctx, "ForwardCallFlow", 10); len(got) != 0 {
		t.Fatalf("expected empty FTS after wipe, got %v", names(got))
	}
	_ = c1.Close(ctx)

	// Re-open: backfillFTSIfEmpty should reproject the existing symbols.
	c2, err := New(dir, "proj-backfill")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = c2.Close(ctx) })
	got, err := c2.SearchSymbolsFTS(ctx, "ForwardCallFlow", 10)
	if err != nil {
		t.Fatalf("post-backfill search: %v", err)
	}
	if !contains(names(got), "ForwardCallFlow") {
		t.Fatalf("backfill did not reproject symbols; got %v", names(got))
	}
	if got2, _ := c2.SearchSymbolsFTS(ctx, "complex", 10); !contains(names(got2), "complexOperation") {
		t.Fatalf("backfill missed camelCase symbol; got %v", names(got2))
	}
}

// TestFTS_BlankQuery returns no matches without error.
func TestFTS_BlankQuery(t *testing.T) {
	ctx := context.Background()
	c := newSQLiteClient(t, "proj-fts-blank")
	seedSymbol(t, c, "Anything", "function", "a.go")
	got, err := c.SearchSymbolsFTS(ctx, "   ", 10)
	if err != nil {
		t.Fatalf("blank query: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("blank query returned %v", names(got))
	}
}
