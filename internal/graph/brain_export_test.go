package graph

import (
	"testing"
)

// TestStableDomainKey verifies that stableDomainKey produces the expected
// deterministic composite key for each known label.
func TestStableDomainKey(t *testing.T) {
	cases := []struct {
		name  string
		label string
		props map[string]any
		want  string
	}{
		{
			name:  "File",
			label: LabelFile,
			props: map[string]any{"path": "internal/graph/client.go", "lang": "go"},
			want:  "file:internal/graph/client.go",
		},
		{
			name:  "Symbol",
			label: LabelSymbol,
			props: map[string]any{"source_file": "cmd/main.go", "name": "Run", "kind": "function"},
			want:  "symbol:cmd/main.go#Run",
		},
		{
			name:  "Package",
			label: LabelPackage,
			props: map[string]any{"import_path": "github.com/foo/bar/internal/x", "name": "x"},
			want:  "package:github.com/foo/bar/internal/x",
		},
		{
			name:  "UnknownLabelWithName",
			label: "CustomNode",
			props: map[string]any{"name": "my-thing"},
			want:  "node:CustomNode:my-thing",
		},
		{
			name:  "FileMissingPath",
			label: LabelFile,
			props: map[string]any{"lang": "go"},
			want:  "", // no usable key
		},
		{
			name:  "SymbolMissingName",
			label: LabelSymbol,
			props: map[string]any{"source_file": "cmd/main.go"},
			want:  "", // name required
		},
		{
			name:  "SymbolMissingSourceFile",
			label: LabelSymbol,
			props: map[string]any{"name": "Run"},
			want:  "", // source_file required
		},
		{
			name:  "PackageMissingImportPath",
			label: LabelPackage,
			props: map[string]any{"name": "x"},
			want:  "", // import_path required
		},
		{
			name:  "UnknownLabelMissingName",
			label: "Orphan",
			props: map[string]any{"something": "else"},
			want:  "", // no name, no fallback
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stableDomainKey(tc.label, tc.props)
			if got != tc.want {
				t.Errorf("stableDomainKey(%q, %v) = %q; want %q", tc.label, tc.props, got, tc.want)
			}
		})
	}
}

// TestParseDomainKey verifies that parseDomainKey correctly decodes keys
// produced by stableDomainKey.
func TestParseDomainKey(t *testing.T) {
	cases := []struct {
		key        string
		wantLabel  string
		wantMerge  map[string]any
		wantOK     bool
	}{
		{
			key:       "file:internal/graph/client.go",
			wantLabel: LabelFile,
			wantMerge: map[string]any{"path": "internal/graph/client.go"},
			wantOK:    true,
		},
		{
			key:       "symbol:cmd/main.go#Run",
			wantLabel: LabelSymbol,
			wantMerge: map[string]any{"source_file": "cmd/main.go", "name": "Run"},
			wantOK:    true,
		},
		{
			// Symbol name that itself contains "#" — LastIndex picks the rightmost.
			key:       "symbol:path/to/file.go#Func#WithHash",
			wantLabel: LabelSymbol,
			wantMerge: map[string]any{"source_file": "path/to/file.go#Func", "name": "WithHash"},
			wantOK:    true,
		},
		{
			key:       "package:github.com/foo/bar/internal/x",
			wantLabel: LabelPackage,
			wantMerge: map[string]any{"import_path": "github.com/foo/bar/internal/x"},
			wantOK:    true,
		},
		{
			key:       "node:CustomNode:my-thing",
			wantLabel: "CustomNode",
			wantMerge: map[string]any{"name": "my-thing"},
			wantOK:    true,
		},
		{key: "symbol:no-hash-at-all", wantOK: false},
		{key: "symbol:file.go#", wantOK: false},  // name empty
		{key: "node:LabelOnly:", wantOK: false},   // name empty
		{key: "node:nocodon", wantOK: false},      // no colon after label
		{key: "unknown:format", wantOK: false},
		{key: "", wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			label, merge, ok := parseDomainKey(tc.key)
			if ok != tc.wantOK {
				t.Fatalf("parseDomainKey(%q) ok=%v; want %v", tc.key, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if label != tc.wantLabel {
				t.Errorf("label=%q; want %q", label, tc.wantLabel)
			}
			for k, wantV := range tc.wantMerge {
				if gotV, exists := merge[k]; !exists || gotV != wantV {
					t.Errorf("merge[%q]=%v; want %v", k, gotV, wantV)
				}
			}
			if len(merge) != len(tc.wantMerge) {
				t.Errorf("merge len=%d; want %d: %v", len(merge), len(tc.wantMerge), merge)
			}
		})
	}
}

// TestStableDomainKeyRoundTrip verifies that parseDomainKey(stableDomainKey(...))
// round-trips for all known labels.
func TestStableDomainKeyRoundTrip(t *testing.T) {
	cases := []struct {
		label string
		props map[string]any
	}{
		{LabelFile, map[string]any{"path": "cmd/gg/main.go"}},
		{LabelSymbol, map[string]any{"source_file": "internal/graph/crud.go", "name": "UpsertNode"}},
		{LabelPackage, map[string]any{"import_path": "github.com/gurkangul/gg-cli/internal/graph"}},
		{"Custom", map[string]any{"name": "singleton"}},
	}

	for _, tc := range cases {
		key := stableDomainKey(tc.label, tc.props)
		if key == "" {
			t.Errorf("stableDomainKey(%q, %v) returned empty string", tc.label, tc.props)
			continue
		}
		gotLabel, gotMerge, ok := parseDomainKey(key)
		if !ok {
			t.Errorf("parseDomainKey(%q) failed (label=%q, props=%v)", key, tc.label, tc.props)
			continue
		}
		if gotLabel != tc.label {
			t.Errorf("round-trip label: got %q; want %q (key=%q)", gotLabel, tc.label, key)
		}
		mergeKeys := mergeKeysForLabel(tc.label)
		for _, k := range mergeKeys {
			origV := tc.props[k]
			rtV, exists := gotMerge[k]
			if !exists {
				t.Errorf("round-trip: merge key %q missing (key=%q)", k, key)
				continue
			}
			if rtV != origV {
				t.Errorf("round-trip: merge[%q]=%v; want %v (key=%q)", k, rtV, origV, key)
			}
		}
	}
}
