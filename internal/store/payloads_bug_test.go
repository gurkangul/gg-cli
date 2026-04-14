// Package store — unit tests for bugFromPayload, isConnectivityError,
// valueToAny, extractPayload, and extractVector helpers.
package store

import (
	"errors"
	"testing"

	"github.com/qdrant/go-client/qdrant"
)

// ── bugFromPayload ────────────────────────────────────────────────────────────

func TestBugFromPayload_Full(t *testing.T) {
	pay, err := qdrant.TryValueMap(map[string]any{
		"bug_id":      "BUG-005",
		"title":       "panic on nil vector",
		"detail":      "search handler dereferences nil embedding",
		"severity":    "critical",
		"status":      "open",
		"root_cause":  "",
		"fix_summary": "",
		"task_id":     "TASK-042",
		"tags":        []any{"crash", "search"},
		"created_at":  "2026-03-01T00:00:00Z",
		"updated_at":  "2026-03-02T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("TryValueMap: %v", err)
	}

	b := bugFromPayload(pay)

	if b.ID != "BUG-005" {
		t.Errorf("ID: got %q", b.ID)
	}
	if b.Title != "panic on nil vector" {
		t.Errorf("Title: got %q", b.Title)
	}
	if b.Severity != "critical" {
		t.Errorf("Severity: got %q", b.Severity)
	}
	if b.Status != "open" {
		t.Errorf("Status: got %q", b.Status)
	}
	if b.TaskID != "TASK-042" {
		t.Errorf("TaskID: got %q", b.TaskID)
	}
	if len(b.Tags) != 2 || b.Tags[0] != "crash" {
		t.Errorf("Tags: got %v", b.Tags)
	}
}

func TestBugFromPayload_Empty(t *testing.T) {
	pay := map[string]*qdrant.Value{}
	b := bugFromPayload(pay)
	if b.ID != "" {
		t.Errorf("expected empty ID, got %q", b.ID)
	}
}

// ── isConnectivityError ───────────────────────────────────────────────────────

func TestIsConnectivityError_Nil(t *testing.T) {
	if isConnectivityError(nil) {
		t.Error("expected false for nil error")
	}
}

func TestIsConnectivityError_ConnectionRefused(t *testing.T) {
	err := errors.New("dial tcp 127.0.0.1:6334: connect: connection refused")
	if !isConnectivityError(err) {
		t.Errorf("expected true for connection refused error, got false")
	}
}

func TestIsConnectivityError_DialTCP(t *testing.T) {
	err := errors.New("dial tcp: lookup no-such-host: no such host")
	if !isConnectivityError(err) {
		t.Errorf("expected true for dial tcp error, got false")
	}
}

func TestIsConnectivityError_Unavailable(t *testing.T) {
	err := errors.New("rpc error: code = Unavailable desc = transport error")
	if !isConnectivityError(err) {
		t.Errorf("expected true for code = unavailable error, got false")
	}
}

// TestIsConnectivityError_ContextDeadline verifies that a context deadline is
// NOT classified as a connectivity failure — Qdrant is reachable but slow,
// which callers must signal differently from "unreachable".
func TestIsConnectivityError_ContextDeadline(t *testing.T) {
	err := errors.New("context deadline exceeded")
	if isConnectivityError(err) {
		t.Errorf("deadline exceeded must not be treated as connectivity error (Qdrant may just be slow)")
	}
	if !isTimeoutError(err) {
		t.Errorf("expected isTimeoutError=true for deadline exceeded error")
	}
}

func TestIsConnectivityError_UnrelatedError(t *testing.T) {
	err := errors.New("invalid collection name: decisions_proj123")
	if isConnectivityError(err) {
		t.Errorf("expected false for unrelated error, got true")
	}
}

func TestIsConnectivityError_WrongVector(t *testing.T) {
	err := errors.New("wrong vector dimension: expected 768, got 384")
	if isConnectivityError(err) {
		t.Errorf("expected false for dimension error, got true")
	}
}

// ── export helpers (pure functions) ──────────────────────────────────────────

func TestValueToAny_Nil(t *testing.T) {
	if got := valueToAny(nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

func TestValueToAny_String(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: "hello"}}
	got := valueToAny(v)
	s, ok := got.(string)
	if !ok || s != "hello" {
		t.Errorf("expected string 'hello', got %T(%v)", got, got)
	}
}

func TestValueToAny_Integer(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_IntegerValue{IntegerValue: 42}}
	got := valueToAny(v)
	n, ok := got.(int64)
	if !ok || n != 42 {
		t.Errorf("expected int64(42), got %T(%v)", got, got)
	}
}

func TestValueToAny_Double(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_DoubleValue{DoubleValue: 3.14}}
	got := valueToAny(v)
	f, ok := got.(float64)
	if !ok || f != 3.14 {
		t.Errorf("expected float64(3.14), got %T(%v)", got, got)
	}
}

func TestValueToAny_Bool(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_BoolValue{BoolValue: true}}
	got := valueToAny(v)
	b, ok := got.(bool)
	if !ok || !b {
		t.Errorf("expected bool(true), got %T(%v)", got, got)
	}
}

func TestValueToAny_List(t *testing.T) {
	v := &qdrant.Value{
		Kind: &qdrant.Value_ListValue{
			ListValue: &qdrant.ListValue{
				Values: []*qdrant.Value{
					{Kind: &qdrant.Value_StringValue{StringValue: "a"}},
					{Kind: &qdrant.Value_StringValue{StringValue: "b"}},
				},
			},
		},
	}
	got := valueToAny(v)
	list, ok := got.([]any)
	if !ok || len(list) != 2 {
		t.Errorf("expected []any len 2, got %T(%v)", got, got)
	}
}

func TestValueToAny_NilList(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_ListValue{ListValue: nil}}
	got := valueToAny(v)
	list, ok := got.([]any)
	if !ok || len(list) != 0 {
		t.Errorf("expected empty []any, got %T(%v)", got, got)
	}
}

func TestValueToAny_Struct(t *testing.T) {
	v := &qdrant.Value{
		Kind: &qdrant.Value_StructValue{
			StructValue: &qdrant.Struct{
				Fields: map[string]*qdrant.Value{
					"key": {Kind: &qdrant.Value_StringValue{StringValue: "val"}},
				},
			},
		},
	}
	got := valueToAny(v)
	m, ok := got.(map[string]any)
	if !ok || m["key"] != "val" {
		t.Errorf("expected map with key='val', got %T(%v)", got, got)
	}
}

func TestValueToAny_NilStruct(t *testing.T) {
	v := &qdrant.Value{Kind: &qdrant.Value_StructValue{StructValue: nil}}
	got := valueToAny(v)
	m, ok := got.(map[string]any)
	if !ok || len(m) != 0 {
		t.Errorf("expected empty map, got %T(%v)", got, got)
	}
}

func TestExtractPayload_NonEmpty(t *testing.T) {
	pay := map[string]*qdrant.Value{
		"name":  {Kind: &qdrant.Value_StringValue{StringValue: "Alice"}},
		"score": {Kind: &qdrant.Value_IntegerValue{IntegerValue: 99}},
	}
	out := extractPayload(pay)
	if out["name"] != "Alice" {
		t.Errorf("name: got %v", out["name"])
	}
	if out["score"] != int64(99) {
		t.Errorf("score: got %v", out["score"])
	}
}

func TestExtractPayload_Empty(t *testing.T) {
	out := extractPayload(map[string]*qdrant.Value{})
	if len(out) != 0 {
		t.Errorf("expected empty map, got %v", out)
	}
}

func TestExtractVector_Nil(t *testing.T) {
	if got := extractVector(nil); got != nil {
		t.Errorf("expected nil for nil VectorsOutput, got %v", got)
	}
}

func TestExtractVector_NilVector(t *testing.T) {
	if got := extractVector(&qdrant.VectorsOutput{}); got != nil {
		t.Errorf("expected nil when no vector set, got %v", got)
	}
}
