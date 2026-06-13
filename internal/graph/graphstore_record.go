package graph

import "fmt"

// errSingleRecord builds the error returned by GraphResult.Single when the
// remaining row count is not exactly one. The message mirrors the neo4j driver's
// "expected exactly one record" contract closely enough for callers that wrap it.
func errSingleRecord(remaining int) error {
	return fmt.Errorf("expected exactly one record, found %d", remaining)
}

// recordValue extracts a typed value for key from a GraphRecord. It is the
// backend-neutral replacement for neo4j.GetRecordValue[T]: it returns the
// zero value of T (and ok=false) when the key is absent or the stored value is
// not assignable to T, matching how the existing call-sites ignore the ok/err
// return and fall back to the zero value.
func recordValue[T any](rec GraphRecord, key string) (T, bool, error) {
	var zero T
	v, present := rec.Get(key)
	if !present {
		return zero, false, fmt.Errorf("record has no key %q", key)
	}
	if v == nil {
		return zero, false, nil
	}
	tv, ok := v.(T)
	if !ok {
		return zero, false, fmt.Errorf("record key %q is %T, not %T", key, v, zero)
	}
	return tv, true, nil
}
