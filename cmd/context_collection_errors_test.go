package cmd

import (
	"errors"
	"testing"
)

// BUG-076: a partial-collection failure must be reported per-collection so an
// empty list (e.g. rejections) is not misread as authoritative-empty.
func TestBundleCollectionErrors(t *testing.T) {
	b := contextBundle{
		rejErr: errors.New("rejections collection unavailable"),
	}
	got := bundleCollectionErrors(b)
	if _, ok := got["rejections"]; !ok {
		t.Fatalf("expected rejections error reported, got %v", got)
	}
	if _, ok := got["decisions"]; ok {
		t.Fatalf("decisions queried OK — must not appear in error map: %v", got)
	}

	// All-OK bundle => empty map (no false alarms).
	if len(bundleCollectionErrors(contextBundle{})) != 0 {
		t.Fatal("clean bundle must produce no collection_errors")
	}
}
