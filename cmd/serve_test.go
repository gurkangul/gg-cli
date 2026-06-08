package cmd

import (
	"io/fs"
	"testing"

	"github.com/gurkangul/gg-cli/dashboard"
)

func TestServe_EmbeddedDashboardHasIndex(t *testing.T) {
	dist, err := fs.Sub(dashboard.FS, "dist")
	if err != nil {
		t.Fatalf("dist sub: %v", err)
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		t.Errorf("embedded dashboard missing dist/index.html: %v", err)
	}
}

func TestServe_FirstN(t *testing.T) {
	if got := firstN([]int{1, 2, 3, 4}, 2); len(got) != 2 {
		t.Errorf("firstN over-cap len = %d, want 2", len(got))
	}
	if got := firstN([]int{1}, 5); len(got) != 1 {
		t.Errorf("firstN under-cap len = %d, want 1", len(got))
	}
}
