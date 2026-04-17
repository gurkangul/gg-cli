package cmd

import (
	"context"
	"errors"
	"fmt"
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
