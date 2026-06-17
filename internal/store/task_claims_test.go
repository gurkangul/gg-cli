package store

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTaskClaimHelpers(t *testing.T) {
	t.Run("requireTaskOwner trims and rejects empty", func(t *testing.T) {
		got, err := requireTaskOwner("  codex  ")
		if err != nil {
			t.Fatalf("requireTaskOwner(trim): %v", err)
		}
		if got != "codex" {
			t.Fatalf("requireTaskOwner(trim) = %q, want codex", got)
		}

		if _, err := requireTaskOwner(" \t "); err == nil {
			t.Fatal("requireTaskOwner(empty) expected error")
		}
	})

	t.Run("requirePositiveLease validates duration", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			in   time.Duration
			want bool
		}{
			{name: "positive", in: time.Second, want: false},
			{name: "zero", in: 0, want: true},
			{name: "negative", in: -time.Second, want: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := requirePositiveLease(tc.in)
				if tc.want && err == nil {
					t.Fatalf("requirePositiveLease(%v) expected error", tc.in)
				}
				if !tc.want && err != nil {
					t.Fatalf("requirePositiveLease(%v): %v", tc.in, err)
				}
			})
		}
	})

	t.Run("taskLeaseActive handles blank malformed expired and active", func(t *testing.T) {
		now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
		cases := []struct {
			name string
			task *Task
			want bool
		}{
			{name: "nil", task: nil, want: false},
			{name: "blank", task: &Task{LeaseUntil: "   "}, want: false},
			{name: "malformed", task: &Task{LeaseUntil: "not-a-time"}, want: false},
			{name: "expired", task: &Task{LeaseUntil: now.Add(-time.Second).Format(time.RFC3339)}, want: false},
			{name: "active", task: &Task{LeaseUntil: now.Add(time.Second).Format(time.RFC3339)}, want: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := taskLeaseActive(tc.task, now); got != tc.want {
					t.Fatalf("taskLeaseActive() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("taskVersionedPayload increments and preserves keys", func(t *testing.T) {
		now := time.Date(2026, 5, 22, 12, 34, 56, 0, time.FixedZone("UTC+2", 2*60*60))
		base := map[string]*Value{"existing": taskStringValue("keep")}
		current := &Task{Version: 0}
		got := taskVersionedPayload(current, base, now)
		if got["existing"] != base["existing"] {
			t.Fatal("taskVersionedPayload replaced existing payload value")
		}
		assertValueKind(t, got["task_version"], "integer_value", int64(1))
		assertValueKind(t, got["updated_at"], "string_value", now.UTC().Format(time.RFC3339))

		next := taskVersionedPayload(&Task{Version: 7}, map[string]*Value{}, now)
		assertValueKind(t, next["task_version"], "integer_value", int64(8))
	})

	t.Run("taskStringValue and taskIntValue set protobuf kinds", func(t *testing.T) {
		assertValueKind(t, taskStringValue("abc"), "string_value", "abc")
		assertValueKind(t, taskIntValue(42), "integer_value", int64(42))
	})

	t.Run("readyForLiveActionForStatus marks updates", func(t *testing.T) {
		if got := readyForLiveActionForStatus("pending"); got != "ready_for_live" {
			t.Fatalf("pending action = %q", got)
		}
		if got := readyForLiveActionForStatus("ready_for_live"); got != "ready_for_live_updated" {
			t.Fatalf("ready_for_live action = %q", got)
		}
	})

	t.Run("mutation filters include task_id status owner and version conditions", func(t *testing.T) {
		single := taskMutationFilter("task-1", "pending")
		assertFilterConditionStrings(t, single, []string{
			"task_id",
			"status",
			"pending",
		})

		multi := taskMutationFilter("task-2", "pending", "in_progress")
		assertFilterConditionStrings(t, multi, []string{
			"task_id",
			"status",
			"pending",
			"in_progress",
		})

		current := &Task{ID: "task-3", Version: 5}
		currentFilter := taskCurrentMutationFilter(current, "pending")
		assertFilterConditionStrings(t, currentFilter, []string{
			"task_id",
			"status",
			"pending",
			"task_version",
			"5",
		})

		owned := taskOwnedMutationFilter("task-4", "pending", "codex")
		assertFilterConditionStrings(t, owned, []string{
			"task_id",
			"status",
			"pending",
			"owner",
			"codex",
		})

		currentOwned := taskCurrentOwnedMutationFilter(&Task{ID: "task-5", Version: 9}, "pending", "codex")
		assertFilterConditionStrings(t, currentOwned, []string{
			"task_id",
			"status",
			"pending",
			"owner",
			"codex",
			"task_version",
			"9",
		})
	})
}

func assertValueKind(t *testing.T, v *Value, wantField string, wantValue any) {
	t.Helper()
	if v == nil {
		t.Fatal("value is nil")
	}
	gotField, gotValue := valueKindField(v)
	if gotField != wantField {
		t.Fatalf("field = %q, want %q", gotField, wantField)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("value = %#v, want %#v", gotValue, wantValue)
	}
}

// valueKindField reports the oneof variant name (matching the former protobuf
// field names, e.g. "integer_value") and the underlying Go value of a *Value.
func valueKindField(v *Value) (string, any) {
	switch k := v.GetKind().(type) {
	case *Value_StringValue:
		return "string_value", k.StringValue
	case *Value_IntegerValue:
		return "integer_value", k.IntegerValue
	case *Value_DoubleValue:
		return "double_value", k.DoubleValue
	case *Value_BoolValue:
		return "bool_value", k.BoolValue
	case *Value_ListValue:
		return "list_value", k.ListValue
	case *Value_StructValue:
		return "struct_value", k.StructValue
	case *Value_NullValue:
		return "null_value", k.NullValue
	default:
		return "", nil
	}
}

func assertFilterConditionStrings(t *testing.T, filter *Filter, want []string) {
	t.Helper()
	if filter == nil {
		t.Fatal("filter is nil")
	}
	var tokens []string
	for _, c := range filter.GetMust() {
		tokens = append(tokens, conditionTokens(c)...)
	}
	for _, c := range filter.GetMustNot() {
		tokens = append(tokens, conditionTokens(c)...)
	}
	for _, c := range filter.GetShould() {
		tokens = append(tokens, conditionTokens(c)...)
	}
	got := strings.Join(tokens, " ")
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Fatalf("filter %s missing %q", got, s)
		}
	}
}

// conditionTokens flattens a Condition into the field key and all match operands
// (keywords, integers, booleans) as strings, so assertFilterConditionStrings can
// substring-match the way the former fmt.Sprint(proto) check did.
func conditionTokens(c *Condition) []string {
	fc := c.GetField()
	if fc == nil {
		return nil
	}
	tokens := []string{fc.GetKey()}
	m := fc.GetMatch()
	if m == nil {
		return tokens
	}
	switch mv := m.GetMatchValue().(type) {
	case *Match_Keyword:
		tokens = append(tokens, mv.Keyword)
	case *Match_Keywords:
		tokens = append(tokens, mv.Keywords.GetStrings()...)
	case *Match_ExceptKeywords:
		tokens = append(tokens, mv.ExceptKeywords.GetStrings()...)
	case *Match_Boolean:
		tokens = append(tokens, fmt.Sprintf("%v", mv.Boolean))
	case *Match_Integer:
		tokens = append(tokens, fmt.Sprintf("%d", mv.Integer))
	case *Match_Integers:
		for _, iv := range mv.Integers.GetIntegers() {
			tokens = append(tokens, fmt.Sprintf("%d", iv))
		}
	}
	return tokens
}
