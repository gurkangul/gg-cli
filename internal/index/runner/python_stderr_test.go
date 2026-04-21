package runner

import (
	"strings"
	"testing"
)

func TestSanitizePythonStderr_CollapsesPathDistributionTraceback(t *testing.T) {
	raw := `Python script failed with code 1: Traceback (most recent call last):
  File "/tmp/scip-python-xxx/get_packages.py", line 37, in <module>
    package_info = get_package_info(package_names)
  File "/tmp/scip-python-xxx/get_packages.py", line 11, in get_package_info
    if dist.name in package_set:
AttributeError: 'PathDistribution' object has no attribute 'name'

Falling back to pip show approach
extra info line
`
	got := string(sanitizePythonStderr([]byte(raw)))
	if strings.Contains(got, "Traceback") {
		t.Fatalf("traceback not collapsed: %s", got)
	}
	if strings.Contains(got, "PathDistribution") {
		t.Fatalf("PathDistribution line leaked: %s", got)
	}
	if !strings.Contains(got, "scip-python fallback engaged") {
		t.Fatalf("replacement notice missing: %s", got)
	}
	if !strings.Contains(got, "extra info line") {
		t.Fatalf("lines after fallback should be preserved: %s", got)
	}
}

func TestSanitizePythonStderr_PassesThroughUnknownErrors(t *testing.T) {
	raw := `Traceback (most recent call last):
  some other error
AttributeError: different attribute
`
	got := string(sanitizePythonStderr([]byte(raw)))
	if got != raw {
		t.Fatalf("unknown error should pass through unchanged; got: %s", got)
	}
}

func TestSanitizePythonStderr_EmptyInput(t *testing.T) {
	if got := sanitizePythonStderr(nil); len(got) != 0 {
		t.Fatalf("empty input should return empty; got: %q", got)
	}
}
