package changelog_test

import (
	"strings"
	"testing"

	"github.com/gurkangul/gg-cli/internal/changelog"
)

const testChangelog = `# Changelog

## [Unreleased]

### Added

- unreleased feature

## [0.3.0] - 2026-01-01

### Added

- new 0.3.0 feature

## [0.2.0] - 2025-12-01

### Added

- new 0.2.0 feature

## [0.1.0] - 2025-11-01

### Added

- initial release
`

func init() {
	changelog.SetContent(testChangelog)
}

func TestSinceSameVersion(t *testing.T) {
	if changelog.Since("0.2.0", "0.2.0") != "" {
		t.Error("same version should return empty string")
	}
}

func TestSinceSpecificVersion(t *testing.T) {
	out := changelog.Since("0.1.0", "0.2.0")
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "0.2.0") {
		t.Errorf("output should contain 0.2.0, got: %s", out)
	}
	if strings.Contains(out, "## [0.1.0]") {
		t.Error("output should not include 0.1.0 section")
	}
	if strings.Contains(out, "## [0.3.0]") {
		t.Error("output should not include 0.3.0 section")
	}
}

func TestSinceMultipleVersions(t *testing.T) {
	out := changelog.Since("0.1.0", "0.3.0")
	if !strings.Contains(out, "0.3.0") {
		t.Error("should include 0.3.0")
	}
	if !strings.Contains(out, "0.2.0") {
		t.Error("should include 0.2.0 as intermediate")
	}
	if strings.Contains(out, "0.1.0") {
		t.Error("should not include 0.1.0")
	}
}

func TestSinceDevVersion(t *testing.T) {
	out := changelog.Since("0.3.0", "dev")
	if !strings.Contains(strings.ToLower(out), "unreleased") {
		t.Error("dev version should return Unreleased section")
	}
}

func TestSinceVPrefix(t *testing.T) {
	a := changelog.Since("0.1.0", "0.2.0")
	b := changelog.Since("v0.1.0", "v0.2.0")
	if a != b {
		t.Error("v-prefixed and unprefixed should produce same output")
	}
}

func TestSinceEmptyPrev(t *testing.T) {
	out := changelog.Since("", "0.2.0")
	if out == "" {
		t.Fatal("empty prev should return content through currVersion")
	}
	if !strings.Contains(out, "0.2.0") {
		t.Error("should include 0.2.0")
	}
}

func TestVersions(t *testing.T) {
	vers := changelog.Versions()
	if len(vers) == 0 {
		t.Fatal("expected at least one version")
	}
	if vers[0] != "" {
		t.Errorf("first version should be Unreleased (\"\"), got %q", vers[0])
	}
	found := false
	for _, v := range vers {
		if v == "0.2.0" {
			found = true
		}
	}
	if !found {
		t.Error("0.2.0 not found in versions list")
	}
}
