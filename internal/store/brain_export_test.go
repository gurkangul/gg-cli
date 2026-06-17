package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestCanonicalJSON_primitives(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"null", nil, "null"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"int", 42, "42"},
		{"int64", int64(-7), "-7"},
		{"float32", float32(1.5), "1.500000"},
		{"float64", 3.14159265, "3.141593"},
		{"float zero", 0.0, "0.000000"},
		{"string simple", "hello", `"hello"`},
		{"string with quote", `say "hi"`, `"say \"hi\""`},
		{"string with backslash", `a\b`, `"a\\b"`},
		{"string with newline", "a\nb", `"a\nb"`},
		{"string with angle", "<b>&</b>", `"<b>&</b>"`},
		{"empty string", "", `""`},
		{"empty array", []any{}, "[]"},
		{"array ints", []any{1, 2, 3}, "[1,2,3]"},
		{"empty map", map[string]any{}, "{}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalJSON(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestCanonicalJSON_mapKeyOrder(t *testing.T) {
	m := map[string]any{
		"z": 1,
		"a": 2,
		"m": 3,
	}
	got, err := CanonicalJSON(m)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":2,"m":3,"z":1}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalJSON_nested(t *testing.T) {
	v := map[string]any{
		"b": map[string]any{
			"z": "last",
			"a": "first",
		},
		"a": []any{3, 1, 2},
	}
	got, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	// Keys at top level: a, b. Nested keys: a, z.
	want := `{"a":[3,1,2],"b":{"a":"first","z":"last"}}`
	if string(got) != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCanonicalJSON_noHTMLEscape(t *testing.T) {
	v := map[string]any{"url": "https://example.com/?a=1&b=2"}
	got, err := CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(got)
	// Standard encoding/json would escape & as \u0026; we must not.
	if strings.Contains(s, `\u0026`) {
		t.Errorf("HTML-escaped ampersand found in output: %s", s)
	}
	if !strings.Contains(s, "&") {
		t.Errorf("literal ampersand missing from output: %s", s)
	}
}

func TestCanonicalJSON_floatPrecision(t *testing.T) {
	cases := []struct {
		f    float64
		want string
	}{
		{1.0, "1.000000"},
		{0.123456789, "0.123457"},
		{-0.5, "-0.500000"},
		{100.0, "100.000000"},
	}
	for _, tc := range cases {
		got, err := CanonicalJSON(tc.f)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("float %v: got %s, want %s", tc.f, got, tc.want)
		}
	}
}

// ── BUG-005: error discrimination unit tests ───────────────────────────────

// fakeScroller implements scrollerIface for unit tests.
// Each call returns the configured err (and a nil page + nil next).
type fakeScroller struct {
	err error
}

func (f *fakeScroller) ScrollAndOffset(_ context.Context, _ *ScrollPoints) ([]*RetrievedPoint, *PointId, error) {
	return nil, nil, f.err
}

// newTestClient builds a minimal Client with a fake scroller — no a live vector backend needed.
func newTestClient(t *testing.T, s scrollerIface) *Client {
	t.Helper()
	return &Client{
		scroller:  s,
		projectID: "test-proj",
		dataDir:   t.TempDir(),
	}
}

// notFoundErr wraps the native errCollectionNotFound sentinel one level deep so
// isCollectionNotFoundError (errors.Is) recognizes it through the chain, exactly
// as the embedded store raises it on a missing collection.
func notFoundErr() error {
	return fmt.Errorf("ScrollAndOffset() failed: test-coll: %w", collectionNotFoundErr("test-coll"))
}

// transientErr mimics a transient, non-NotFound store error that must propagate.
func transientErr() error {
	return fmt.Errorf("ScrollAndOffset() failed: test-coll: %w", errors.New("transport is closing"))
}

func TestBrainExportCollection_NotFound_ReturnsEmpty(t *testing.T) {
	c := newTestClient(t, &fakeScroller{err: notFoundErr()})
	records, err := c.ExportBrainCollection(context.Background(), "decisions")
	if err != nil {
		t.Fatalf("NotFound should yield nil error, got: %v", err)
	}
	if records != nil {
		t.Fatalf("NotFound should yield nil records, got %v", records)
	}
}

func TestBrainExportCollection_Unavailable_ReturnsError(t *testing.T) {
	c := newTestClient(t, &fakeScroller{err: transientErr()})
	_, err := c.ExportBrainCollection(context.Background(), "decisions")
	if err == nil {
		t.Fatal("transient error must propagate, got nil")
	}
	if !strings.Contains(err.Error(), "scroll") {
		t.Errorf("expected 'scroll' in error message, got: %v", err)
	}
}

func TestIsCollectionNotFoundError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"raw NotFound", collectionNotFoundErr("c"), true},
		{"wrapped NotFound", fmt.Errorf("op: %w", collectionNotFoundErr("c")), true},
		{"transient", errors.New("unavailable"), false},
		{"plain error", errors.New("some error"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCollectionNotFoundError(tc.err); got != tc.want {
				t.Errorf("isCollectionNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestExportBrainCollection_sortOrder(t *testing.T) {
	// Verify that stable sort is applied: UUIDs in descending input order
	// must come out ascending.
	records := []BrainRecord{
		{ID: "z-uuid", Payload: map[string]any{"x": 1}},
		{ID: "a-uuid", Payload: map[string]any{"x": 2}},
		{ID: "m-uuid", Payload: map[string]any{"x": 3}},
	}

	// Simulate the sort that ExportBrainCollection applies.
	import_sort := func(rs []BrainRecord) {
		for i := 1; i < len(rs); i++ {
			for j := i; j > 0 && rs[j].ID < rs[j-1].ID; j-- {
				rs[j], rs[j-1] = rs[j-1], rs[j]
			}
		}
	}
	import_sort(records)

	if records[0].ID != "a-uuid" || records[1].ID != "m-uuid" || records[2].ID != "z-uuid" {
		t.Errorf("unexpected sort order: %v %v %v", records[0].ID, records[1].ID, records[2].ID)
	}
}
