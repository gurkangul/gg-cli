package cmd

import (
	"testing"
)

func TestParseSinceFlag(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"7d", "7 days ago", false},
		{"14d", "14 days ago", false},
		{"30d", "30 days ago", false},
		{"1d", "1 days ago", false},
		{"7", "", true},
		{"d", "", true},
		{"", "", true},
		{"7h", "", true},
	}
	for _, c := range cases {
		got, err := parseSinceFlag(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSinceFlag(%q): expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSinceFlag(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSinceFlag(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFileIsCovered(t *testing.T) {
	corpus := []string{
		"task about cmd/audit.go refactoring the session window logic",
		"decision on internal/store tasks.go payload mapping",
	}

	cases := []struct {
		file string
		want bool
	}{
		// Full path present in corpus.
		{"cmd/audit.go", true},
		// Base name "tasks.go" appears in corpus.
		{"internal/store/tasks.go", true},
		// Neither path nor base name referenced.
		{"cmd/verify.go", false},
		{"README.md", false},
	}

	for _, c := range cases {
		got := fileIsCovered(c.file, corpus)
		if got != c.want {
			t.Errorf("fileIsCovered(%q) = %v, want %v", c.file, got, c.want)
		}
	}
}
