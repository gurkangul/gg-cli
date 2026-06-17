package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeHost is a store-free ToolHost for exercising the JSON-RPC protocol core
// without opening a real brain.
type fakeHost struct{ calls int }

func (f *fakeHost) ListTools() []Tool {
	return []Tool{{Name: "gg_search", InputSchema: map[string]any{"type": "object"}}}
}

func (f *fakeHost) CallTool(_ context.Context, name string, _ map[string]any) ([]ContentBlock, bool) {
	f.calls++
	if name != "gg_search" {
		return TextBlock("unknown tool: " + name), true
	}
	return TextBlock("ok"), false
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("response is not valid JSON: %v\nraw: %s", err, raw)
	}
	if m["jsonrpc"] != "2.0" {
		t.Fatalf("missing jsonrpc=2.0: %s", raw)
	}
	return m
}

func TestInitializeHandshake(t *testing.T) {
	s := New(&fakeHost{}, "test")
	resp := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`))
	m := decode(t, resp)
	if m["id"].(float64) != 1 {
		t.Fatalf("id mismatch: %v", m["id"])
	}
	result := m["result"].(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Fatalf("protocolVersion: %v", result["protocolVersion"])
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "gg" {
		t.Fatalf("serverInfo.name: %v", info["name"])
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Fatalf("capabilities.tools missing")
	}
}

func TestNotificationGetsNoResponse(t *testing.T) {
	s := New(&fakeHost{}, "test")
	// notifications/initialized has no id → no response.
	if resp := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); resp != nil {
		t.Fatalf("notification must produce no response, got: %s", resp)
	}
	// blank line → no response.
	if resp := s.HandleLine(context.Background(), []byte("   ")); resp != nil {
		t.Fatalf("blank line must produce no response, got: %s", resp)
	}
}

func TestPingEmptyResult(t *testing.T) {
	s := New(&fakeHost{}, "test")
	m := decode(t, s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":2,"method":"ping"}`)))
	if len(m["result"].(map[string]any)) != 0 {
		t.Fatalf("ping result must be empty: %v", m["result"])
	}
}

func TestUnknownMethodIsMethodNotFound(t *testing.T) {
	s := New(&fakeHost{}, "test")
	m := decode(t, s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":3,"method":"bogus"}`)))
	e := m["error"].(map[string]any)
	if int(e["code"].(float64)) != codeMethodNotFound {
		t.Fatalf("expected -32601, got %v", e["code"])
	}
}

func TestParseErrorOnBadJSON(t *testing.T) {
	s := New(&fakeHost{}, "test")
	m := decode(t, s.HandleLine(context.Background(), []byte(`{not json`)))
	e := m["error"].(map[string]any)
	if int(e["code"].(float64)) != codeParseError {
		t.Fatalf("expected -32700, got %v", e["code"])
	}
}

func TestToolsListAdvertisesSchema(t *testing.T) {
	s := New(&fakeHost{}, "test")
	m := decode(t, s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)))
	tools := m["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "gg_search" {
		t.Fatalf("tool name: %v", tool["name"])
	}
	if _, ok := tool["inputSchema"]; !ok {
		t.Fatalf("tool missing inputSchema")
	}
}

func TestToolsCallContentShape(t *testing.T) {
	h := &fakeHost{}
	s := New(h, "test")
	m := decode(t, s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"gg_search","arguments":{"query":"x"}}}`)))
	res := m["result"].(map[string]any)
	if res["isError"].(bool) {
		t.Fatalf("expected isError=false")
	}
	content := res["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "ok" {
		t.Fatalf("content block: %v", block)
	}
	if h.calls != 1 {
		t.Fatalf("expected 1 tool call, got %d", h.calls)
	}
}

// TestReadOnlyToolSurface locks in the read-only guarantee: the real Host must
// advertise exactly the six gg_* read tools and zero write tools.
func TestReadOnlyToolSurface(t *testing.T) {
	tools := NewHost().ListTools()
	want := map[string]bool{
		"gg_search": false, "gg_context": false, "gg_impact": false,
		"gg_canon": false, "gg_task_get": false, "gg_bug_get": false,
	}
	mutating := []string{"record", "create", "update", "delete", "write", "set", "send", "done", "task_start", "reject", "tell"}
	for _, tool := range tools {
		if _, ok := want[tool.Name]; !ok {
			t.Fatalf("unexpected tool advertised: %q", tool.Name)
		}
		want[tool.Name] = true
		for _, m := range mutating {
			if strings.Contains(tool.Name, m) {
				t.Fatalf("read-only violation: mutating-named tool %q advertised", tool.Name)
			}
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q missing inputSchema", tool.Name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Fatalf("expected read tool %q not advertised", name)
		}
	}
	if len(tools) != 6 {
		t.Fatalf("expected exactly 6 read tools, got %d", len(tools))
	}
}
