package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeHost is a store-free ToolHost for exercising the JSON-RPC protocol core
// without opening a real brain. calls is atomic so concurrent HandleLine
// callers (TestServeConcurrentPanicsDoNotCrash) don't race on it.
type fakeHost struct{ calls atomic.Int64 }

func (f *fakeHost) ListTools() []Tool {
	return []Tool{{Name: "gg_search", InputSchema: map[string]any{"type": "object"}}}
}

func (f *fakeHost) CallTool(_ context.Context, name string, _ map[string]any) ([]ContentBlock, bool) {
	f.calls.Add(1)
	if name == "boom" {
		panic("tool exploded")
	}
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
	if h.calls.Load() != 1 {
		t.Fatalf("expected 1 tool call, got %d", h.calls.Load())
	}
}

// TestReadOnlyToolSurface locks in the read-only guarantee: the real Host must
// advertise exactly the seven gg_* read tools and zero write tools.
func TestReadOnlyToolSurface(t *testing.T) {
	tools := NewHost().ListTools()
	want := map[string]bool{
		"gg_search": false, "gg_context": false, "gg_impact": false,
		"gg_def": false, "gg_canon": false, "gg_task_get": false, "gg_bug_get": false,
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
	if len(tools) != 7 {
		t.Fatalf("expected exactly 7 read tools, got %d", len(tools))
	}
}

// TestToolPanicBecomesError locks in panic recovery: a tool that panics returns
// a JSON-RPC internal error instead of crashing the process / killing the session.
func TestToolPanicBecomesError(t *testing.T) {
	s := New(&fakeHost{}, "test")
	resp := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"boom","arguments":{}}}`))
	m := decode(t, resp)
	if m["id"].(float64) != 7 {
		t.Fatalf("id mismatch: %v", m["id"])
	}
	e, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object on panic, got: %s", resp)
	}
	if int(e["code"].(float64)) != codeInternalError {
		t.Fatalf("expected internal error %d, got %v", codeInternalError, e["code"])
	}
	// Session must still serve the next request.
	if next := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":8,"method":"ping"}`)); next == nil {
		t.Fatalf("session died after panic: ping returned nil")
	}
}

// TestNotificationWithIDGetsNoResponse locks in item 4: any notifications/*
// method is fire-and-forget even if a client wrongly attaches an id.
func TestNotificationWithIDGetsNoResponse(t *testing.T) {
	s := New(&fakeHost{}, "test")
	if resp := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":99,"method":"notifications/initialized"}`)); resp != nil {
		t.Fatalf("notifications/initialized with id must produce no response, got: %s", resp)
	}
	// An unknown notifications/* method is also a no-response notification, not
	// a method-not-found error.
	if resp := s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":100,"method":"notifications/cancelled"}`)); resp != nil {
		t.Fatalf("notifications/cancelled with id must produce no response, got: %s", resp)
	}
}

// TestServeOversizeLineDegrades locks in item 2: a single line longer than
// maxLineBytes is skipped with a JSON-RPC error and the loop keeps serving the
// following well-formed request rather than ending the session.
func TestServeOversizeLineDegrades(t *testing.T) {
	var in bytes.Buffer
	// Oversize line: maxLineBytes+10 'a' bytes, then a newline.
	in.Write(bytes.Repeat([]byte("a"), maxLineBytes+10))
	in.WriteByte('\n')
	// A valid request after the oversize line must still be answered.
	in.WriteString(`{"jsonrpc":"2.0","id":42,"method":"ping"}` + "\n")

	var out, logw bytes.Buffer
	s := New(&fakeHost{}, "test")
	if err := s.Serve(context.Background(), &in, &out, &logw); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}

	lines := splitJSONLines(out.Bytes())
	if len(lines) != 2 {
		t.Fatalf("expected 2 response lines (oversize error + ping), got %d:\n%s", len(lines), out.String())
	}
	// First line: the oversize error (id null, invalid request code).
	errResp := decode(t, lines[0])
	e, ok := errResp["error"].(map[string]any)
	if !ok || int(e["code"].(float64)) != codeInvalidRequest {
		t.Fatalf("first line must be an invalid-request error, got: %s", lines[0])
	}
	// Second line: the ping reply, proving the session survived.
	pingResp := decode(t, lines[1])
	if pingResp["id"].(float64) != 42 {
		t.Fatalf("expected ping reply id=42, got: %s", lines[1])
	}
}

func splitJSONLines(b []byte) [][]byte {
	var out [][]byte
	for _, l := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(l)) > 0 {
			out = append(out, l)
		}
	}
	return out
}

// TestServeConcurrentPanicsDoNotCrash drives several panicking tool calls
// through HandleLine concurrently to confirm the recover path is goroutine-safe
// and never propagates.
func TestServeConcurrentPanicsDoNotCrash(t *testing.T) {
	s := New(&fakeHost{}, "test")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.HandleLine(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`))
		}()
	}
	wg.Wait()
}
