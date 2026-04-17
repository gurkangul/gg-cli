package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/store"
)

// fakeLiveCounter implements brainLiveCounter for unit tests.
type fakeLiveCounter struct {
	counts map[string]int // kind → number of live records to synthesise
	errOn  string         // if non-empty, ExportBrainCollection returns an error for this kind
}

func (f *fakeLiveCounter) ExportBrainCollection(_ context.Context, kind string) ([]store.BrainRecord, error) {
	if kind == f.errOn {
		return nil, errors.New("simulated scroll error")
	}
	n := f.counts[kind]
	records := make([]store.BrainRecord, n)
	for i := range records {
		records[i] = store.BrainRecord{ID: fmt.Sprintf("%s-%d", kind, i)}
	}
	return records, nil
}

func TestCheckManifestProjectID_Match(t *testing.T) {
	if err := checkManifestProjectID("pid-abc", "pid-abc", false); err != nil {
		t.Errorf("expected nil for matching project_ids, got %v", err)
	}
}

func TestCheckManifestProjectID_Mismatch(t *testing.T) {
	err := checkManifestProjectID("pid-src", "pid-dst", false)
	if err == nil {
		t.Fatal("expected error for project_id mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "pid-src") || !strings.Contains(err.Error(), "pid-dst") {
		t.Errorf("error should name both project_ids, got: %v", err)
	}
}

func TestCheckManifestProjectID_EmptyManifestPID(t *testing.T) {
	// Old snapshots without project_id must always be accepted.
	if err := checkManifestProjectID("", "pid-current", false); err != nil {
		t.Errorf("expected nil for empty manifest project_id, got %v", err)
	}
}

func TestCheckManifestProjectID_ForceFlag(t *testing.T) {
	// --force-project-mismatch bypasses the check even when PIDs differ.
	if err := checkManifestProjectID("pid-a", "pid-b", true); err != nil {
		t.Errorf("expected nil with force=true, got %v", err)
	}
}

func TestBrainStatus_ComputeDrift_InSync(t *testing.T) {
	manifest := brainManifest{
		Counts: map[string]int{"decisions": 3, "tasks": 5},
	}
	counter := &fakeLiveCounter{
		counts: map[string]int{"decisions": 3, "tasks": 5},
	}
	status, delta := computeDrift(context.Background(), counter, manifest)
	if status != "in_sync" {
		t.Errorf("expected in_sync, got %q", status)
	}
	if len(delta) != 0 {
		t.Errorf("expected empty delta, got %v", delta)
	}
}

func TestBrainStatus_ComputeDrift_Drifted(t *testing.T) {
	manifest := brainManifest{
		Counts: map[string]int{"decisions": 3, "tasks": 5},
	}
	counter := &fakeLiveCounter{
		counts: map[string]int{"decisions": 5, "tasks": 5}, // +2 on decisions
	}
	status, delta := computeDrift(context.Background(), counter, manifest)
	if status != "drifted" {
		t.Errorf("expected drifted, got %q", status)
	}
	if delta["decisions"] != 2 {
		t.Errorf("expected decisions delta=2, got %d", delta["decisions"])
	}
}

func TestBrainStatus_ComputeDrift_Unknown(t *testing.T) {
	manifest := brainManifest{
		Counts: map[string]int{"decisions": 3},
	}
	counter := &fakeLiveCounter{errOn: "decisions"}
	status, delta := computeDrift(context.Background(), counter, manifest)
	if status != "unknown" {
		t.Errorf("expected unknown, got %q", status)
	}
	if delta != nil {
		t.Errorf("expected nil delta on scroll error, got %v", delta)
	}
}
