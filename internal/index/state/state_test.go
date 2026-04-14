// Package state — unit tests for Read/Write helpers.
// These tests use only t.TempDir() — no network or Qdrant required.
package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRead_MissingFile_ReturnsErrNoState(t *testing.T) {
	dir := t.TempDir()
	_, err := Read(dir)
	if !errors.Is(err, ErrNoState) {
		t.Errorf("expected ErrNoState, got %v", err)
	}
}

func TestWriteRead_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	sha := "abc123def456abc123def456abc123def456abc1"

	if err := Write(dir, sha); err != nil {
		t.Fatalf("Write: %v", err)
	}

	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.LastIndexedSHA != sha {
		t.Errorf("SHA: got %q, want %q", s.LastIndexedSHA, sha)
	}
	if s.IndexedAt == "" {
		t.Error("IndexedAt should not be empty")
	}
}

func TestRead_EmptySHA_ReturnsErrNoState(t *testing.T) {
	dir := t.TempDir()
	// Write a state file with an empty SHA (malformed state).
	path := filepath.Join(dir, "index-state.json")
	if err := os.WriteFile(path, []byte(`{"last_indexed_sha":"","indexed_at":"2024-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Read(dir)
	if !errors.Is(err, ErrNoState) {
		t.Errorf("expected ErrNoState for empty SHA, got %v", err)
	}
}

func TestRead_MalformedJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index-state.json")
	if err := os.WriteFile(path, []byte(`{not valid json`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, err := Read(dir)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if errors.Is(err, ErrNoState) {
		t.Error("malformed JSON should return a parse error, not ErrNoState")
	}
}

func TestWrite_CreatesFileInDir(t *testing.T) {
	dir := t.TempDir()
	sha := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	if err := Write(dir, sha); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path := filepath.Join(dir, "index-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), sha) {
		t.Errorf("state file should contain SHA %q, got: %s", sha, data)
	}
}

func TestWrite_OverwritesPreviousState(t *testing.T) {
	dir := t.TempDir()
	sha1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	sha2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	if err := Write(dir, sha1); err != nil {
		t.Fatalf("Write sha1: %v", err)
	}
	if err := Write(dir, sha2); err != nil {
		t.Fatalf("Write sha2: %v", err)
	}

	s, err := Read(dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.LastIndexedSHA != sha2 {
		t.Errorf("expected sha2 %q, got %q", sha2, s.LastIndexedSHA)
	}
}
