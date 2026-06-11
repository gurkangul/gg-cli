package cmd

import (
	"context"
	"strings"
	"testing"
)

func TestDockerDaemonDownMsg_PlatformGuidance(t *testing.T) {
	cases := []struct {
		goos string
		want string // platform-specific fragment that must appear
	}{
		{"darwin", "open -a Docker"},
		{"linux", "systemctl start docker"},
		{"windows", "Docker Desktop"},
		{"plan9", "start the daemon"}, // unknown OS → generic Linux-style hint
	}
	for _, tc := range cases {
		t.Run(tc.goos, func(t *testing.T) {
			got := dockerDaemonDownMsg(tc.goos)
			if !strings.Contains(got, "Docker daemon is not running") {
				t.Fatalf("missing daemon-down lead for %s:\n%s", tc.goos, got)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("missing platform guidance %q for %s:\n%s", tc.want, tc.goos, got)
			}
			if !strings.Contains(got, "gg init") {
				t.Fatalf("missing retry hint for %s:\n%s", tc.goos, got)
			}
		})
	}
}

func TestDockerMissingMsg_Actionable(t *testing.T) {
	got := dockerMissingMsg()
	if !strings.Contains(got, "Docker is not installed") {
		t.Fatalf("missing install lead:\n%s", got)
	}
	if !strings.Contains(got, "PATH") {
		t.Fatalf("missing PATH guidance:\n%s", got)
	}
}

func TestComposeFailureMsg_SurfacesRealError(t *testing.T) {
	const compose = "/home/u/.gg/docker-compose.yaml"
	raw := "Error response from daemon: pull access denied for qdrant/qdrant"
	got := composeFailureMsg(compose, raw)

	if !strings.Contains(got, raw) {
		t.Fatalf("compose failure must surface the REAL underlying error, got:\n%s", got)
	}
	if !strings.Contains(got, "docker compose -f "+compose+" up -d") {
		t.Fatalf("compose failure must include manual retry command, got:\n%s", got)
	}
	if !strings.Contains(got, "Underlying error") {
		t.Fatalf("compose failure must label the underlying error, got:\n%s", got)
	}
}

func TestComposeFailureMsg_EmptyRawStillActionable(t *testing.T) {
	got := composeFailureMsg("/tmp/c.yaml", "   ")
	if strings.Contains(got, "Underlying error") {
		t.Fatalf("empty raw error should not print an empty 'Underlying error' block:\n%s", got)
	}
	if !strings.Contains(got, "docker compose -f /tmp/c.yaml up -d") {
		t.Fatalf("must still include retry command:\n%s", got)
	}
}

// TestProbeDockerDaemon_MissingBinary verifies the probe degrades gracefully
// (no crash, dockerMissing) when `docker` is not on PATH — exercised by pointing
// PATH at an empty dir so LookPath fails deterministically.
func TestProbeDockerDaemon_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	res, detail := probeDockerDaemon(context.Background())
	if res != dockerMissing {
		t.Fatalf("expected dockerMissing when docker absent, got res=%d detail=%q", res, detail)
	}
}
