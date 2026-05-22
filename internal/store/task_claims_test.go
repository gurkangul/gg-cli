package store

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/protobuf/reflect/protoreflect"
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
		base := map[string]*qdrant.Value{"existing": taskStringValue("keep")}
		current := &Task{Version: 0}
		got := taskVersionedPayload(current, base, now)
		if got["existing"] != base["existing"] {
			t.Fatal("taskVersionedPayload replaced existing payload value")
		}
		assertValueKind(t, got["task_version"], "integer_value", int64(1))
		assertValueKind(t, got["updated_at"], "string_value", now.UTC().Format(time.RFC3339))

		next := taskVersionedPayload(&Task{Version: 7}, map[string]*qdrant.Value{}, now)
		assertValueKind(t, next["task_version"], "integer_value", int64(8))
	})

	t.Run("taskStringValue and taskIntValue set protobuf kinds", func(t *testing.T) {
		assertValueKind(t, taskStringValue("abc"), "string_value", "abc")
		assertValueKind(t, taskIntValue(42), "integer_value", int64(42))
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

func assertValueKind(t *testing.T, v *qdrant.Value, wantField string, wantValue any) {
	t.Helper()
	if v == nil {
		t.Fatal("value is nil")
	}
	fields := protoFields(v.ProtoReflect())
	if len(fields) != 1 {
		t.Fatalf("expected 1 protobuf field, got %d (%v)", len(fields), fields)
	}
	if fields[0].name != wantField {
		t.Fatalf("field = %q, want %q", fields[0].name, wantField)
	}
	if !reflect.DeepEqual(fields[0].value, wantValue) {
		t.Fatalf("value = %#v, want %#v", fields[0].value, wantValue)
	}
}

type fieldValue struct {
	name  string
	kind  protoreflect.Kind
	value any
}

func protoFields(msg protoreflect.Message) []fieldValue {
	var out []fieldValue
	msg.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		out = append(out, fieldValue{name: string(fd.Name()), kind: fd.Kind(), value: protoreflectValue(fd.Kind(), v)})
		return true
	})
	return out
}

func protoreflectValue(kind protoreflect.Kind, v protoreflect.Value) any {
	switch kind {
	case protoreflect.BoolKind:
		return v.Bool()
	case protoreflect.EnumKind:
		return v.Enum()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int64(v.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return v.Int()
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return uint64(v.Uint())
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return v.Uint()
	case protoreflect.FloatKind:
		return float64(v.Float())
	case protoreflect.DoubleKind:
		return v.Float()
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BytesKind:
		return string(v.Bytes())
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

func assertFilterConditionStrings(t *testing.T, filter *qdrant.Filter, want []string) {
	t.Helper()
	if filter == nil {
		t.Fatal("filter is nil")
	}
	got := fmt.Sprint(filter)
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Fatalf("filter %s missing %q", got, s)
		}
	}
}
