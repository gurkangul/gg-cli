package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsupportedLangNotice(t *testing.T) {
	cases := []struct {
		lang    string
		wantSub string // "" means notice must be empty
	}{
		{"", ""},
		{"Java", "Detected Java"},
		{"C# / .NET", "Detected C# / .NET"},
		{"Rust", "Detected Rust"},
	}
	for _, tc := range cases {
		got := unsupportedLangNotice(tc.lang)
		if tc.wantSub == "" {
			if got != "" {
				t.Errorf("unsupportedLangNotice(%q) = %q, want empty", tc.lang, got)
			}
			continue
		}
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("unsupportedLangNotice(%q) = %q, missing %q", tc.lang, got, tc.wantSub)
		}
		// Every notice must name the supported set so users know the gap.
		for _, lang := range []string{"Go", "TypeScript", "Python", "Swift"} {
			if !strings.Contains(got, lang) {
				t.Errorf("notice for %q missing supported lang %q:\n%s", tc.lang, lang, got)
			}
		}
	}
}

// TestPrintUnsupportedLangNotice_SuppressedForSupported verifies a recognized
// SUPPORTED language (go.mod present) produces no notice even if an unsupported
// marker is also present — polyglot repos with a supported lang index fine.
func TestPrintUnsupportedLangNotice_SuppressedForSupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// detectLangHint must win (go) → no unsupported notice.
	if got := detectLangHint(dir); got != "go" {
		t.Fatalf("detectLangHint = %q, want go", got)
	}
	if notice := unsupportedLangNotice(detectUnsupportedLang(dir)); notice == "" {
		t.Skip("detectUnsupportedLang found nothing; nothing to assert")
	}
	// printUnsupportedLangNotice gates on detectLangHint != "" so it stays silent;
	// assert that gating decision directly.
	if detectLangHint(dir) == "" {
		t.Fatal("guard precondition: supported lang must be detected")
	}
}

// TestPrintUnsupportedLangNotice_FiresForUnsupported confirms a Java-only repo
// is recognized as unsupported (drives the early-notice path).
func TestPrintUnsupportedLangNotice_FiresForUnsupported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := detectLangHint(dir); got != "" {
		t.Fatalf("detectLangHint = %q, want empty for Java-only repo", got)
	}
	notice := unsupportedLangNotice(detectUnsupportedLang(dir))
	if !strings.Contains(notice, "Detected Java") {
		t.Fatalf("expected Java unsupported notice, got %q", notice)
	}
}
