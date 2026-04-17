package parser

import (
	"testing"

	scippb "github.com/scip-code/scip/bindings/go/scip"

	"github.com/gurkangul/gg-cli/internal/graph"
)

func TestIsDefinition(t *testing.T) {
	def := &scippb.Occurrence{SymbolRoles: int32(scippb.SymbolRole_Definition)}
	ref := &scippb.Occurrence{SymbolRoles: 0}
	rwDef := &scippb.Occurrence{SymbolRoles: int32(scippb.SymbolRole_Definition) | int32(scippb.SymbolRole_WriteAccess)}

	if !isDefinition(def) {
		t.Error("expected Definition occurrence to be a definition")
	}
	if isDefinition(ref) {
		t.Error("expected reference occurrence to NOT be a definition")
	}
	if !isDefinition(rwDef) {
		t.Error("expected Definition|WriteAccess to still be a definition")
	}
}

func TestScipSymbolName(t *testing.T) {
	cases := []struct {
		sym  string
		want string
	}{
		{"go github.com/gurkangul/gg-cli/internal/graph/Client#New().", "New"},
		{"go github.com/gurkangul/gg-cli/internal/graph/Client#", "Client"},
		{"local abc123", "local abc123"},
		{"", ""},
		{"simple", "simple"},
	}
	for _, c := range cases {
		got := scipSymbolName(c.sym)
		if got != c.want {
			t.Errorf("scipSymbolName(%q) = %q, want %q", c.sym, got, c.want)
		}
	}
}

func TestSymbolNodeVisibility(t *testing.T) {
	// Local symbol → skipped (nil); locals are anonymous and not indexed.
	local := symbolNode("local abc123", "go", symbolMeta{})
	if local != nil {
		t.Errorf("local symbol should be skipped (nil), got %+v", local)
	}

	// Package-level symbol → public
	pub := symbolNode("go github.com/foo/bar/Baz#Method().", "go", symbolMeta{kind: scippb.SymbolInformation_Method})
	if pub.Properties["visibility"] != string(graph.VisPublic) {
		t.Errorf("package symbol should be public, got %v", pub.Properties["visibility"])
	}
	if pub.Properties["boundary"] != true {
		t.Errorf("package symbol should have boundary=true")
	}
}

func TestScipKindMapping(t *testing.T) {
	cases := []struct {
		kind scippb.SymbolInformation_Kind
		want graph.SymbolKind
	}{
		{scippb.SymbolInformation_Function, graph.KindFunction},
		{scippb.SymbolInformation_Method, graph.KindMethod},
		{scippb.SymbolInformation_Class, graph.KindType},
		{scippb.SymbolInformation_Struct, graph.KindType},
		{scippb.SymbolInformation_Interface, graph.KindInterface},
		{scippb.SymbolInformation_Variable, graph.KindVariable},
		{scippb.SymbolInformation_Constant, graph.KindConstant},
		{scippb.SymbolInformation_EnumMember, graph.KindConstant},
		{scippb.SymbolInformation_UnspecifiedKind, graph.KindFunction}, // fallback
	}
	for _, c := range cases {
		got := scipKindToGraphKind(c.kind, "")
		if got != c.want {
			t.Errorf("scipKindToGraphKind(%v) = %v, want %v", c.kind, got, c.want)
		}
	}
}
