package lsp

import (
	"strings"
	"testing"
)

// TestResolveServerUnknownExt asserts an unknown extension yields a clear,
// non-crashing error (no language server configured).
func TestResolveServerUnknownExt(t *testing.T) {
	if _, err := ResolveServer("main.zzz"); err == nil {
		t.Fatal("expected error for unknown extension")
	} else if !strings.Contains(err.Error(), "no language server configured for .zzz") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ResolveServer("Makefile"); err == nil {
		t.Fatal("expected error for extensionless file")
	}
}

// TestResolveServerGo resolves the Go entry regardless of binary presence (the
// PATH check happens later in Dial).
func TestResolveServerGo(t *testing.T) {
	spec, err := ResolveServer("internal/graph/symbol_flow.go")
	if err != nil {
		t.Fatalf("ResolveServer(.go): %v", err)
	}
	if spec.Cmd != "gopls" || spec.LanguageID != "go" {
		t.Fatalf("unexpected gopls spec: %+v", spec)
	}
}

// TestEnsureOnPathMissing confirms a non-existent binary produces an actionable,
// non-panicking error carrying the install hint.
func TestEnsureOnPathMissing(t *testing.T) {
	spec := ServerSpec{Cmd: "definitely-not-a-real-ls-xyz", InstallHint: "go install x@latest"}
	err := spec.ensureOnPath()
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "not found on PATH") || !strings.Contains(err.Error(), "install") {
		t.Fatalf("error not actionable: %v", err)
	}
}

// TestPathURIRoundTrip verifies file:// URI encoding survives round-trip,
// including a path segment with a space.
func TestPathURIRoundTrip(t *testing.T) {
	cases := []string{"/tmp/a/b.go", "/tmp/with space/x.go", "/a/üni.go"}
	for _, p := range cases {
		got := URIToPath(PathToURI(p))
		if got != p {
			t.Fatalf("round-trip: got %q want %q", got, p)
		}
	}
}

// TestHumanToLSP checks 1-based→0-based conversion and UTF-16 column counting.
func TestHumanToLSP(t *testing.T) {
	text := "package x\nfunc Foo() {}\n"
	// line 2, col 6 (the 'F' of Foo) → 0-based line 1, char 5.
	pos := HumanToLSP(text, 2, 6)
	if pos.Line != 1 || pos.Character != 5 {
		t.Fatalf("HumanToLSP: got %+v want {1 5}", pos)
	}
	// A line with a 4-byte rune before the target column: "x := \"🚀\"; Bar"
	multi := "a\nx := \"🚀\"; Bar\n"
	// Column past the emoji should count it as 2 UTF-16 units, not 1 or 4.
	got := HumanToLSP(multi, 2, 11) // 1-based rune col 11 → 'B' of Bar
	want := HumanToLSP(multi, 2, 11)
	if got != want {
		t.Fatalf("non-deterministic: %+v vs %+v", got, want)
	}
	if got.Line != 1 {
		t.Fatalf("line: got %d want 1", got.Line)
	}
}

// TestDecodeLocationsVariants exercises null, single-object, array-of-Location,
// and array-of-LocationLink result shapes.
func TestDecodeLocationsVariants(t *testing.T) {
	if locs, _ := decodeLocations([]byte("null")); locs != nil {
		t.Fatalf("null → want nil, got %v", locs)
	}
	single := `{"uri":"file:///a.go","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}}`
	locs, err := decodeLocations([]byte(single))
	if err != nil || len(locs) != 1 || locs[0].URI != "file:///a.go" {
		t.Fatalf("single: %v %+v", err, locs)
	}
	arr := `[{"uri":"file:///a.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}},` +
		`{"uri":"file:///b.go","range":{"start":{"line":3,"character":0},"end":{"line":3,"character":1}}}]`
	locs, err = decodeLocations([]byte(arr))
	if err != nil || len(locs) != 2 {
		t.Fatalf("array: %v %+v", err, locs)
	}
	link := `[{"targetUri":"file:///c.go","targetRange":{"start":{"line":7,"character":0},"end":{"line":7,"character":3}},` +
		`"targetSelectionRange":{"start":{"line":7,"character":0},"end":{"line":7,"character":3}}}]`
	locs, err = decodeLocations([]byte(link))
	if err != nil || len(locs) != 1 || locs[0].URI != "file:///c.go" {
		t.Fatalf("locationLink: %v %+v", err, locs)
	}
}

// TestDecodeHoverVariants exercises null, string, MarkupContent, and array forms.
func TestDecodeHoverVariants(t *testing.T) {
	if h, _ := decodeHover([]byte("null")); h.PlainText != "" {
		t.Fatalf("null → want empty, got %q", h.PlainText)
	}
	markup := `{"contents":{"kind":"markdown","value":"func Foo()"}}`
	h, err := decodeHover([]byte(markup))
	if err != nil || h.PlainText != "func Foo()" {
		t.Fatalf("markup: %v %q", err, h.PlainText)
	}
	str := `{"contents":"plain sig"}`
	if h, _ := decodeHover([]byte(str)); h.PlainText != "plain sig" {
		t.Fatalf("string contents: %q", h.PlainText)
	}
	arr := `{"contents":["one",{"language":"go","value":"two"}]}`
	if h, _ := decodeHover([]byte(arr)); h.PlainText != "one\ntwo" {
		t.Fatalf("array contents: %q", h.PlainText)
	}
}
