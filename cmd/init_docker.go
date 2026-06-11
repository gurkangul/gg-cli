package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// dockerDaemonDownMsg is the actionable message shown when the Docker CLI is
// present but the daemon is not responding. It is a standalone formatter (no
// I/O) so tests can assert the guidance without a live Docker.
//
// goos selects the platform-specific "start Docker" line; pass runtime.GOOS.
func dockerDaemonDownMsg(goos string) string {
	var start string
	switch goos {
	case "darwin":
		start = "start Docker Desktop (open -a Docker)"
	case "windows":
		start = "start Docker Desktop"
	default:
		start = "start the daemon (e.g. `sudo systemctl start docker`)"
	}
	return "Docker daemon is not running — " + start + ", then retry `gg init`."
}

// dockerMissingMsg is shown when the docker binary itself is not on PATH.
func dockerMissingMsg() string {
	return "Docker is not installed or not on PATH — install Docker " +
		"(https://docs.docker.com/get-docker/) and ensure `docker` is on PATH, then retry `gg init`."
}

// dockerProbeResult classifies the outcome of probing the local Docker setup.
type dockerProbeResult int

const (
	dockerReady   dockerProbeResult = iota // CLI present and daemon responding
	dockerMissing                          // docker binary not found on PATH
	dockerDown                             // CLI present but daemon not responding
)

// probeDockerDaemon checks whether Docker is usable before we attempt to bring
// services up. It runs `docker info` with a short timeout. The probe degrades
// gracefully: a missing binary yields dockerMissing (never a crash), and any
// daemon-level failure yields dockerDown carrying the real stderr so the caller
// can surface the underlying cause rather than a generic line.
func probeDockerDaemon(ctx context.Context) (dockerProbeResult, string) {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerMissing, ""
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "docker", "info", "--format", "{{.ServerVersion}}")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return dockerDown, detail
	}
	_ = out
	return dockerReady, ""
}

// composeFailureMsg builds the message shown when `docker compose up` fails,
// surfacing the REAL underlying error (raw stderr) alongside the manual-retry
// command — not just a generic "start manually" line. Standalone (no I/O) so
// the formatting is unit-testable.
func composeFailureMsg(composePath, rawErr string) string {
	raw := strings.TrimSpace(rawErr)
	msg := "Docker compose failed to start shared services."
	if raw != "" {
		msg += "\n  Underlying error:\n    " + strings.ReplaceAll(raw, "\n", "\n    ")
	}
	msg += fmt.Sprintf("\n  Retry manually: docker compose -f %s up -d", composePath)
	return msg
}

// runtimeGOOS is a seam so tests can exercise the platform-specific Docker-down
// message; production always reads the real runtime.GOOS.
var runtimeGOOS = func() string { return runtime.GOOS }
