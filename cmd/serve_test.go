package cmd

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServe_IndexServesDashboard(t *testing.T) {
	s := &dashboardServer{}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "project brain") {
		t.Error("embedded dashboard HTML not served")
	}
}

func TestServe_IndexNotFoundForOtherPaths(t *testing.T) {
	s := &dashboardServer{}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/secret", nil))
	if rec.Code != 404 {
		t.Errorf("status = %d, want 404 for non-root path", rec.Code)
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
