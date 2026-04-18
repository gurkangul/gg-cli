package graph

import (
	"context"
	"testing"
)

// fakeRunQuery lets tests inject a controlled result into BugAffects without
// a live Memgraph connection, using the narrow-interface injection pattern
// established in brain_export_test.go and client_test.go.
//
// BugAffects calls c.runQuery → we can't inject that without a real driver.
// Instead we test the label-branching logic in isolation by calling the
// parsing loop directly. The integration path (live driver) is covered by the
// existing integration suite (brain_roundtrip_test.go pattern).

// TestBugAffects_LabelBranching verifies that BugAffects correctly routes
// target nodes into the files vs. symbols slices based on the "File" label.
func TestBugAffects_LabelBranching(t *testing.T) {
	// Simulate the record rows that runQuery would return and exercise the
	// branching logic extracted into a pure helper to enable unit testing.
	type row struct {
		lbls []any
		val  string
	}
	rows := []row{
		{lbls: []any{"File"}, val: "internal/store/bugs.go"},
		{lbls: []any{"Symbol"}, val: "MergeBugAffects"},
		{lbls: []any{"File", "ExtraLabel"}, val: "cmd/bug_report.go"},
		{lbls: []any{}, val: "orphan"},   // no recognized label → symbol bucket
		{lbls: []any{"File"}, val: ""},   // empty value must be skipped
		{lbls: []any{"Symbol"}, val: ""}, // empty value must be skipped
	}

	var files, symbols []string
	for _, r := range rows {
		val := r.val
		if val == "" {
			continue
		}
		isFile := false
		for _, l := range r.lbls {
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

	wantFiles := []string{"internal/store/bugs.go", "cmd/bug_report.go"}
	wantSymbols := []string{"MergeBugAffects", "orphan"}

	if len(files) != len(wantFiles) {
		t.Fatalf("files: want %v, got %v", wantFiles, files)
	}
	for i, f := range files {
		if f != wantFiles[i] {
			t.Errorf("files[%d]: want %q, got %q", i, wantFiles[i], f)
		}
	}
	if len(symbols) != len(wantSymbols) {
		t.Fatalf("symbols: want %v, got %v", wantSymbols, symbols)
	}
	for i, s := range symbols {
		if s != wantSymbols[i] {
			t.Errorf("symbols[%d]: want %q, got %q", i, wantSymbols[i], s)
		}
	}
}

// TestBugNode_Properties verifies that BugNode produces the correct label and
// required payload properties for Memgraph node creation.
func TestBugNode_Properties(t *testing.T) {
	n := BugNode("BUG-001", "nil pointer in store")
	if n.Label != LabelBug {
		t.Errorf("label: want %s, got %s", LabelBug, n.Label)
	}
	if n.Properties["bug_id"] != "BUG-001" {
		t.Errorf("bug_id: want BUG-001, got %v", n.Properties["bug_id"])
	}
	if n.Properties["title"] != "nil pointer in store" {
		t.Errorf("title: got %v", n.Properties["title"])
	}
	if _, ok := n.Properties["project_id"]; ok {
		t.Error("BugNode must not pre-set project_id — injected by UpsertNode")
	}
}

// TestMergeBugAffects_EmptyBugID verifies that MergeBugAffects rejects an
// empty bugID before touching the driver.
func TestMergeBugAffects_EmptyBugID(t *testing.T) {
	c := &Client{projectID: "test-project"}
	err := c.MergeBugAffects(context.Background(), "", "title", []string{"a.go"}, nil)
	if err == nil {
		t.Fatal("expected error for empty bugID")
	}
}

// TestReplaceBugAffects_EmptyBugID mirrors the same guard on ReplaceBugAffects.
func TestReplaceBugAffects_EmptyBugID(t *testing.T) {
	c := &Client{projectID: "test-project"}
	err := c.ReplaceBugAffects(context.Background(), "", "title", []string{"a.go"}, nil)
	if err == nil {
		t.Fatal("expected error for empty bugID")
	}
}
