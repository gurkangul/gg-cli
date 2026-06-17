package store

// matchFilter reports whether a point's payload satisfies a filter,
// replicating the server-side filtering the previous backend performed. Only the condition
// types actually used in this package are implemented:
//
//   - Must     — every condition must match (AND).
//   - MustNot  — no condition may match (NOT AND).
//   - Should   — at least one condition must match (OR), when present.
//   - FieldCondition + Match: Keyword, Keywords (any-of), Boolean, Integer.
//   - IsEmpty  — key missing / null / empty list (BUG-085 parity for
//     gg_vector_degraded exclusion).
//
// Unsupported condition types fail closed (return false) so a silent parity gap
// can never widen a result set beyond what the vector store would have returned.
func matchFilter(payload map[string]*Value, f *Filter) bool {
	if f == nil {
		return true
	}
	for _, c := range f.GetMust() {
		if !matchCondition(payload, c) {
			return false
		}
	}
	for _, c := range f.GetMustNot() {
		if matchCondition(payload, c) {
			return false
		}
	}
	if should := f.GetShould(); len(should) > 0 {
		any := false
		for _, c := range should {
			if matchCondition(payload, c) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	return true
}

func matchCondition(payload map[string]*Value, c *Condition) bool {
	if c == nil {
		return false
	}
	switch opt := c.GetConditionOneOf().(type) {
	case *Condition_Field:
		return matchField(payload, opt.Field)
	case *Condition_IsEmpty:
		return isEmptyValue(payload[opt.IsEmpty.GetKey()])
	case *Condition_IsNull:
		v, ok := payload[opt.IsNull.GetKey()]
		return ok && v.GetNullValue() == NullValue_NULL_VALUE
	case *Condition_Filter:
		return matchFilter(payload, opt.Filter)
	default:
		// has_id / nested / geo / range / has_vector are unused here. Fail
		// closed so we never over-match relative to the vector store.
		return false
	}
}

func matchField(payload map[string]*Value, fc *FieldCondition) bool {
	if fc == nil {
		return false
	}
	v, ok := payload[fc.GetKey()]
	if !ok {
		return false
	}
	m := fc.GetMatch()
	if m == nil {
		// A field condition with no match (range/geo) is unused; fail closed.
		return false
	}
	switch mv := m.GetMatchValue().(type) {
	case *Match_Keyword:
		return valueHasKeyword(v, mv.Keyword)
	case *Match_Keywords:
		for _, kw := range mv.Keywords.GetStrings() {
			if valueHasKeyword(v, kw) {
				return true
			}
		}
		return false
	case *Match_ExceptKeywords:
		for _, kw := range mv.ExceptKeywords.GetStrings() {
			if valueHasKeyword(v, kw) {
				return false
			}
		}
		return true
	case *Match_Boolean:
		return v.GetBoolValue() == mv.Boolean
	case *Match_Integer:
		return v.GetIntegerValue() == mv.Integer
	case *Match_Integers:
		for _, iv := range mv.Integers.GetIntegers() {
			if v.GetIntegerValue() == iv {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// valueHasKeyword reports whether a payload Value matches a keyword. the vector store
// matches a keyword against a scalar string OR any element of a list-valued
// field (e.g. tags, read_by), so both shapes are handled.
func valueHasKeyword(v *Value, keyword string) bool {
	if v == nil {
		return false
	}
	if lv := v.GetListValue(); lv != nil {
		for _, item := range lv.GetValues() {
			if item.GetStringValue() == keyword {
				return true
			}
		}
		return false
	}
	return v.GetStringValue() == keyword
}

// isEmptyValue mirrors the vector store's is_empty semantics: a key counts as empty when
// it is absent, explicitly null, or an empty list. A present non-empty scalar or
// list is NOT empty. This is what keeps normal records (which never set
// gg_vector_degraded) in search results while dropping marked degraded ones.
func isEmptyValue(v *Value) bool {
	if v == nil {
		return true
	}
	switch v.GetKind().(type) {
	case *Value_NullValue:
		return true
	case *Value_ListValue:
		return len(v.GetListValue().GetValues()) == 0
	default:
		return false
	}
}
