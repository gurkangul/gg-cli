package store

import "fmt"

// This file (with vectypes_request.go / vectypes_filter.go) defines the native
// Go vector-store DTOs used as internal data carriers. The shapes deliberately
// mirror the call-sites (same names, same getters, same oneof "Kind" tagging) so
// the rest of the package builds requests and reads results unchanged. Nothing
// here talks to a server; these are pure in-process structs marshalled to the
// embedded SQLite store.

// --- payload value model (oneof-tagged scalar/list/struct) ---

// isValueKind is the sealed interface implemented by every payload value variant.
// It reproduces the protobuf oneof wrapper shape so existing literals like
// &Value{Kind: &Value_IntegerValue{IntegerValue: 42}} keep compiling and
// type-switches on v.GetKind() keep working.
type isValueKind interface{ isValueKind() }

// Value is a single payload field value. Exactly one Kind variant is set (or
// none, meaning an unset/empty value, treated as null).
type Value struct {
	Kind isValueKind
}

type Value_StringValue struct{ StringValue string }
type Value_IntegerValue struct{ IntegerValue int64 }
type Value_DoubleValue struct{ DoubleValue float64 }
type Value_BoolValue struct{ BoolValue bool }
type Value_ListValue struct{ ListValue *ListValue }
type Value_StructValue struct{ StructValue *Struct }
type Value_NullValue struct{ NullValue NullValue }

func (*Value_StringValue) isValueKind()  {}
func (*Value_IntegerValue) isValueKind() {}
func (*Value_DoubleValue) isValueKind()  {}
func (*Value_BoolValue) isValueKind()    {}
func (*Value_ListValue) isValueKind()    {}
func (*Value_StructValue) isValueKind()  {}
func (*Value_NullValue) isValueKind()    {}

// NullValue is the payload null-marker enum; only the NULL_VALUE member is used.
type NullValue int32

const NullValue_NULL_VALUE NullValue = 0

// ListValue is an ordered list of payload values.
type ListValue struct {
	Values []*Value
}

// Struct is a string-keyed map of payload values (the payload container shape).
type Struct struct {
	Fields map[string]*Value
}

// GetKind returns the set oneof variant (nil when unset). Callers type-switch on
// the concrete *Value_* type, exactly as with the former protobuf oneof.
func (v *Value) GetKind() isValueKind {
	if v == nil {
		return nil
	}
	return v.Kind
}

func (v *Value) GetStringValue() string {
	if v == nil {
		return ""
	}
	if k, ok := v.Kind.(*Value_StringValue); ok {
		return k.StringValue
	}
	return ""
}

func (v *Value) GetIntegerValue() int64 {
	if v == nil {
		return 0
	}
	if k, ok := v.Kind.(*Value_IntegerValue); ok {
		return k.IntegerValue
	}
	return 0
}

func (v *Value) GetDoubleValue() float64 {
	if v == nil {
		return 0
	}
	if k, ok := v.Kind.(*Value_DoubleValue); ok {
		return k.DoubleValue
	}
	return 0
}

func (v *Value) GetBoolValue() bool {
	if v == nil {
		return false
	}
	if k, ok := v.Kind.(*Value_BoolValue); ok {
		return k.BoolValue
	}
	return false
}

func (v *Value) GetListValue() *ListValue {
	if v == nil {
		return nil
	}
	if k, ok := v.Kind.(*Value_ListValue); ok {
		return k.ListValue
	}
	return nil
}

func (v *Value) GetStructValue() *Struct {
	if v == nil {
		return nil
	}
	if k, ok := v.Kind.(*Value_StructValue); ok {
		return k.StructValue
	}
	return nil
}

func (v *Value) GetNullValue() NullValue {
	if v == nil {
		return NullValue_NULL_VALUE
	}
	if k, ok := v.Kind.(*Value_NullValue); ok {
		return k.NullValue
	}
	return NullValue_NULL_VALUE
}

func (l *ListValue) GetValues() []*Value {
	if l == nil {
		return nil
	}
	return l.Values
}

func (s *Struct) GetFields() map[string]*Value {
	if s == nil {
		return nil
	}
	return s.Fields
}

// --- Value constructors ---

// NewValue builds a *Value from a generic Go value. Supported concrete types are
// the JSON/payload primitives the store handles: string, integers (all widths),
// floats, bool, nil, []any (list), and map[string]any (struct). Nested lists and
// maps recurse. An unsupported type returns an error so callers fail loudly
// rather than silently dropping data.
func NewValue(v any) (*Value, error) {
	switch x := v.(type) {
	case nil:
		return &Value{Kind: &Value_NullValue{NullValue: NullValue_NULL_VALUE}}, nil
	case string:
		return &Value{Kind: &Value_StringValue{StringValue: x}}, nil
	case bool:
		return &Value{Kind: &Value_BoolValue{BoolValue: x}}, nil
	case int:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: int64(x)}}, nil
	case int32:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: int64(x)}}, nil
	case int64:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: x}}, nil
	case uint:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: int64(x)}}, nil
	case uint32:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: int64(x)}}, nil
	case uint64:
		return &Value{Kind: &Value_IntegerValue{IntegerValue: int64(x)}}, nil
	case float32:
		return &Value{Kind: &Value_DoubleValue{DoubleValue: float64(x)}}, nil
	case float64:
		return &Value{Kind: &Value_DoubleValue{DoubleValue: x}}, nil
	case []any:
		lv := &ListValue{Values: make([]*Value, 0, len(x))}
		for i, item := range x {
			iv, err := NewValue(item)
			if err != nil {
				return nil, fmt.Errorf("list element %d: %w", i, err)
			}
			lv.Values = append(lv.Values, iv)
		}
		return &Value{Kind: &Value_ListValue{ListValue: lv}}, nil
	case []string:
		lv := &ListValue{Values: make([]*Value, 0, len(x))}
		for _, item := range x {
			lv.Values = append(lv.Values, &Value{Kind: &Value_StringValue{StringValue: item}})
		}
		return &Value{Kind: &Value_ListValue{ListValue: lv}}, nil
	case map[string]any:
		sv := &Struct{Fields: make(map[string]*Value, len(x))}
		for k, item := range x {
			iv, err := NewValue(item)
			if err != nil {
				return nil, fmt.Errorf("struct field %q: %w", k, err)
			}
			sv.Fields[k] = iv
		}
		return &Value{Kind: &Value_StructValue{StructValue: sv}}, nil
	default:
		return nil, fmt.Errorf("unsupported payload value type %T", v)
	}
}

// NewValueString is a convenience constructor for a string payload value.
func NewValueString(s string) *Value {
	return &Value{Kind: &Value_StringValue{StringValue: s}}
}

// NewValueInt is a convenience constructor for an integer payload value.
func NewValueInt(n int64) *Value {
	return &Value{Kind: &Value_IntegerValue{IntegerValue: n}}
}

// TryValueMap converts a plain-Go payload map into the native *Value map shape,
// returning an error if any value has an unsupported type. It is the single
// build-payload entry point used by the upsert/set-payload call-sites.
func TryValueMap(in map[string]any) (map[string]*Value, error) {
	out := make(map[string]*Value, len(in))
	for k, raw := range in {
		v, err := NewValue(raw)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", k, err)
		}
		out[k] = v
	}
	return out, nil
}

// valueUnderlying returns the plain-Go projection of a single payload value, used
// by the JSON payload codec on encode. It is the inverse of NewValue.
func valueUnderlying(v *Value) any {
	if v == nil {
		return nil
	}
	switch x := v.GetKind().(type) {
	case *Value_StringValue:
		return x.StringValue
	case *Value_IntegerValue:
		return x.IntegerValue
	case *Value_DoubleValue:
		return x.DoubleValue
	case *Value_BoolValue:
		return x.BoolValue
	case *Value_NullValue:
		return nil
	case *Value_ListValue:
		if x.ListValue == nil {
			return []any{}
		}
		items := make([]any, len(x.ListValue.GetValues()))
		for i, item := range x.ListValue.GetValues() {
			items[i] = valueUnderlying(item)
		}
		return items
	case *Value_StructValue:
		if x.StructValue == nil {
			return map[string]any{}
		}
		m := make(map[string]any, len(x.StructValue.GetFields()))
		for k, sv := range x.StructValue.GetFields() {
			m[k] = valueUnderlying(sv)
		}
		return m
	default:
		return nil
	}
}

// PtrOf returns a pointer to a copy of t, for the few call-sites that build
// optional scalar request fields (e.g. ScoreThreshold).
func PtrOf[T any](t T) *T { return &t }
