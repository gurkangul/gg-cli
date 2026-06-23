package cmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
		ok   bool
	}{
		{name: "older", a: "v0.1.0", b: "v0.2.0", want: -1, ok: true},
		{name: "newer", a: "v1.2.1", b: "v1.2.0", want: 1, ok: true},
		{name: "same", a: "v1.2.3", b: "v1.2.3", want: 0, ok: true},
		{name: "release beats prerelease", a: "v1.0.0", b: "v1.0.0-alpha", want: 1, ok: true},
		{name: "dev not comparable", a: "dev-abcdef0", b: "v1.0.0", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := compareSemver(tt.a, tt.b)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Fatalf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{v: "", want: true},
		{v: "dev", want: true},
		{v: "dev-abc1234", want: true},
		{v: "dev-abc1234-dirty", want: true},
		{v: "v1.1.1-0.20240101120000-abcdef012345", want: true}, // VCS pseudo-version
		{v: "v1.0.0-rc1", want: true},
		{v: "v2.3.3+meta", want: true},
		{v: "garbage", want: true},
		{v: "v2.3.3", want: false},
		{v: "v0.0.1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.v, func(t *testing.T) {
			if got := isDevBuild(tt.v); got != tt.want {
				t.Fatalf("isDevBuild(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}

func TestReleaseAssetName(t *testing.T) {
	tests := []struct {
		tag    string
		goos   string
		goarch string
		want   string
	}{
		{tag: "v2.3.3", goos: "darwin", goarch: "arm64", want: "gg_2.3.3_darwin_arm64.tar.gz"},
		{tag: "v2.3.3", goos: "darwin", goarch: "amd64", want: "gg_2.3.3_darwin_amd64.tar.gz"},
		{tag: "v2.3.3", goos: "linux", goarch: "amd64", want: "gg_2.3.3_linux_amd64.tar.gz"},
		{tag: "v2.3.3", goos: "linux", goarch: "arm64", want: "gg_2.3.3_linux_arm64.tar.gz"},
		{tag: "2.3.3", goos: "linux", goarch: "amd64", want: "gg_2.3.3_linux_amd64.tar.gz"}, // tag without leading v
		{tag: "v2.3.3", goos: "windows", goarch: "amd64", want: "gg_2.3.3_windows_amd64.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"_"+tt.goarch, func(t *testing.T) {
			if got := releaseAssetName(tt.tag, tt.goos, tt.goarch); got != tt.want {
				t.Fatalf("releaseAssetName(%q,%q,%q) = %q, want %q", tt.tag, tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestChecksumFor(t *testing.T) {
	manifest := "deadbeef  gg_2.3.3_linux_amd64.tar.gz\n" +
		"cafef00d  gg_2.3.3_darwin_arm64.tar.gz\n"
	if sum, ok := checksumFor(manifest, "gg_2.3.3_darwin_arm64.tar.gz"); !ok || sum != "cafef00d" {
		t.Fatalf("checksumFor darwin = %q/%v, want cafef00d/true", sum, ok)
	}
	if _, ok := checksumFor(manifest, "gg_9.9.9_linux_amd64.tar.gz"); ok {
		t.Fatalf("checksumFor missing entry returned ok=true")
	}
}

func TestBuildUpdateInfo(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		latest     string
		wantUpdate bool
		wantDev    bool
		wantCmp    bool
	}{
		{name: "newer available", current: "v2.3.0", latest: "v2.3.3", wantUpdate: true, wantCmp: true},
		{name: "up to date", current: "v2.3.3", latest: "v2.3.3", wantUpdate: false, wantCmp: true},
		{name: "current ahead", current: "v2.4.0", latest: "v2.3.3", wantUpdate: false, wantCmp: true},
		{name: "dev build", current: "dev-abc1234", latest: "v2.3.3", wantUpdate: false, wantDev: true, wantCmp: false},
		{name: "pseudo-version", current: "v1.1.1-0.20240101-abc", latest: "v2.3.3", wantUpdate: true, wantDev: true, wantCmp: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := buildUpdateInfo(tt.current, tt.latest)
			if info.Update != tt.wantUpdate {
				t.Fatalf("Update = %v, want %v", info.Update, tt.wantUpdate)
			}
			if info.DevBuild != tt.wantDev {
				t.Fatalf("DevBuild = %v, want %v", info.DevBuild, tt.wantDev)
			}
			if info.Comparable != tt.wantCmp {
				t.Fatalf("Comparable = %v, want %v", info.Comparable, tt.wantCmp)
			}
		})
	}
}

// makeReleaseArchive builds an in-memory gzip-tar containing a `gg` binary with
// the given payload, plus extra non-matching entries to exercise the scan.
func makeReleaseArchive(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// Decoy README to confirm extraction picks the `gg` entry specifically.
	writeTarEntry(t, tw, "README.md", []byte("readme"))
	writeTarEntry(t, tw, "gg", payload)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTarEntry(t *testing.T, tw *tar.Writer, name string, data []byte) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Mode:     0o755,
		Size:     int64(len(data)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
}

func TestExtractGGFromTarGz(t *testing.T) {
	payload := []byte("#!/bin/sh\necho fake-gg\n")
	archive := makeReleaseArchive(t, payload)
	got, err := extractGGFromTarGz(archive)
	if err != nil {
		t.Fatalf("extractGGFromTarGz: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("extracted %q, want %q", got, payload)
	}

	if _, err := extractGGFromTarGz([]byte("not a gzip")); err == nil {
		t.Fatalf("expected error on non-gzip input")
	}
}

// releaseServer stubs the GitHub latest-release API + asset downloads. assetName
// is the platform archive name for the running runtime so selfUpdateToRelease
// finds it. Returns the configured release tag.
func releaseServer(t *testing.T, tag string, archive []byte, corruptChecksum bool) (*httptest.Server, ghRelease) {
	t.Helper()
	assetName := releaseAssetName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(archive)
	checksum := hex.EncodeToString(sum[:])
	if corruptChecksum {
		checksum = strings.Repeat("0", len(checksum))
	}
	manifest := fmt.Sprintf("%s  %s\n", checksum, assetName)

	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/asset", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/checksums", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, manifest)
	})
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			http.Error(w, "missing UA", http.StatusForbidden)
			return
		}
		rel := ghRelease{
			TagName: tag,
			Assets: []ghAsset{
				{Name: assetName, URL: srv.URL + "/asset"},
				{Name: checksumsAssetName, URL: srv.URL + "/checksums"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rel)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rel := ghRelease{
		TagName: tag,
		Assets: []ghAsset{
			{Name: assetName, URL: srv.URL + "/asset"},
			{Name: checksumsAssetName, URL: srv.URL + "/checksums"},
		},
	}
	return srv, rel
}

func withStubbedExe(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	exePath := filepath.Join(dir, "gg")
	if err := os.WriteFile(exePath, []byte("OLD-BINARY"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldExe := selfUpdateExecutable
	t.Cleanup(func() { selfUpdateExecutable = oldExe })
	selfUpdateExecutable = func() (string, error) { return exePath, nil }
	return exePath
}

func TestSelfUpdateToRelease_ReplacesBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update binary replacement not supported on windows")
	}
	payload := []byte("NEW-BINARY-CONTENT")
	archive := makeReleaseArchive(t, payload)
	_, rel := releaseServer(t, "v9.9.9", archive, false)
	exePath := withStubbedExe(t)

	out, err := selfUpdateToRelease(context.Background(), rel, "v2.3.3")
	if err != nil {
		t.Fatalf("selfUpdateToRelease: %v", err)
	}
	if out.NewVersion != "v9.9.9" || out.OldVersion != "v2.3.3" {
		t.Fatalf("outcome = %+v, want old=v2.3.3 new=v9.9.9", out)
	}
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary content = %q, want %q", got, payload)
	}
}

func TestSelfUpdateToRelease_ChecksumMismatchAborts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update binary replacement not supported on windows")
	}
	archive := makeReleaseArchive(t, []byte("NEW-BINARY"))
	_, rel := releaseServer(t, "v9.9.9", archive, true) // corrupt checksum
	exePath := withStubbedExe(t)

	_, err := selfUpdateToRelease(context.Background(), rel, "v2.3.3")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
	// Binary must be untouched.
	got, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OLD-BINARY" {
		t.Fatalf("binary was modified despite checksum mismatch: %q", got)
	}
}

func TestRunUpdate_DevBuildGuardNoClobber(t *testing.T) {
	// A dev/source build must NOT download or clobber without --force.
	oldVer := version
	oldJSON := jsonOutput
	oldForce := updateForce
	oldFromSource := updateFromSource
	version = "v1.1.1-0.20240101120000-abcdef012345" // pseudo-version => dev build
	jsonOutput = false
	updateForce = false
	updateFromSource = false
	t.Cleanup(func() {
		version = oldVer
		jsonOutput = oldJSON
		updateForce = oldForce
		updateFromSource = oldFromSource
	})

	// Point the API at a server that, if hit for an asset, would replace the
	// binary — proving the guard short-circuits BEFORE any download.
	payload := []byte("RELEASE-BINARY")
	archive := makeReleaseArchive(t, payload)
	srv, _ := releaseServer(t, "v9.9.9", archive, false)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL + "/latest"
	exePath := withStubbedExe(t)

	stdout, _, err := captureUpdateOutput(t, func() error {
		cmd := *updateCmd
		cmd.SetContext(context.Background())
		return runUpdate(&cmd, nil)
	})
	if err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if !strings.Contains(stdout, "source build") || !strings.Contains(stdout, "--from-source") {
		t.Fatalf("expected dev-build guard message, got:\n%s", stdout)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != "OLD-BINARY" {
		t.Fatalf("dev build was clobbered: %q", got)
	}
}

func TestRunUpdate_ForceInstallsOnDevBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update binary replacement not supported on windows")
	}
	oldVer := version
	oldJSON := jsonOutput
	oldForce := updateForce
	oldFromSource := updateFromSource
	oldSkip := updateSkipSync
	version = "dev-abc1234"
	jsonOutput = false
	updateForce = true
	updateFromSource = false
	updateSkipSync = true // avoid invoking the freshly-written fake binary
	t.Cleanup(func() {
		version = oldVer
		jsonOutput = oldJSON
		updateForce = oldForce
		updateFromSource = oldFromSource
		updateSkipSync = oldSkip
	})

	payload := []byte("FORCED-RELEASE-BINARY")
	archive := makeReleaseArchive(t, payload)
	srv, _ := releaseServer(t, "v9.9.9", archive, false)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL + "/latest"

	oldLook := updateLookPath
	t.Cleanup(func() { updateLookPath = oldLook })
	updateLookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	exePath := withStubbedExe(t)

	stdout, _, err := captureUpdateOutput(t, func() error {
		cmd := *updateCmd
		cmd.SetContext(context.Background())
		return runUpdate(&cmd, nil)
	})
	if err != nil {
		t.Fatalf("runUpdate --force: %v", err)
	}
	if !strings.Contains(stdout, "updated dev-abc1234 → v9.9.9") {
		t.Fatalf("expected update line, got:\n%s", stdout)
	}
	got, _ := os.ReadFile(exePath)
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary not replaced on --force: %q", got)
	}
}

func TestFetchLatestRelease_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL

	_, err := fetchLatestRelease(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
}

func TestEnvTruthy(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on", "y"} {
		if !envTruthy(v) {
			t.Fatalf("envTruthy(%q) = false, want true", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off", "no"} {
		if envTruthy(v) {
			t.Fatalf("envTruthy(%q) = true, want false", v)
		}
	}
}

func TestEmitUpdateNotice_OptInOnly(t *testing.T) {
	oldVer := version
	version = "v0.1.0"
	t.Cleanup(func() { version = oldVer })

	payload := []byte("x")
	archive := makeReleaseArchive(t, payload)
	srv, _ := releaseServer(t, "v0.2.0", archive, false)
	oldBase := ggReleaseAPIBase
	t.Cleanup(func() { ggReleaseAPIBase = oldBase })
	ggReleaseAPIBase = srv.URL + "/latest"

	t.Setenv(updateCheckEnvVar, "")
	var buf bytes.Buffer
	emitUpdateNotice(&buf)
	if buf.Len() != 0 {
		t.Fatalf("notice emitted without opt-in:\n%s", buf.String())
	}

	t.Setenv(updateCheckEnvVar, "1")
	emitUpdateNotice(&buf)
	got := buf.String()
	if !strings.Contains(got, "GG UPDATE AVAILABLE: v0.1.0") || !strings.Contains(got, "run: gg update") {
		t.Fatalf("missing update notice:\n%s", got)
	}
}

func TestSourceInstallTargetPrefersPathGG(t *testing.T) {
	oldLookPath := updateLookPath
	t.Cleanup(func() { updateLookPath = oldLookPath })

	dir := t.TempDir()
	active := filepath.Join(dir, "gg")
	if err := os.WriteFile(active, []byte("fake"), 0o755); err != nil {
		t.Fatalf("write fake gg: %v", err)
	}
	updateLookPath = func(string) (string, error) { return active, nil }

	target, err := sourceInstallTarget(context.Background())
	if err != nil {
		t.Fatalf("sourceInstallTarget: %v", err)
	}
	if normalizeTestPath(t, target) != normalizeTestPath(t, active) {
		t.Fatalf("target = %q, want PATH gg %q", target, active)
	}
}

func TestInstalledBinaryWarningDetectsPATHSkew(t *testing.T) {
	oldExecutable := updateExecutable
	oldLookPath := updateLookPath
	t.Cleanup(func() {
		updateExecutable = oldExecutable
		updateLookPath = oldLookPath
	})

	dir := t.TempDir()
	active := filepath.Join(dir, "old", "gg")
	target := filepath.Join(dir, "new", "gg")
	updateExecutable = func() (string, error) { return active, nil }
	updateLookPath = func(string) (string, error) { return active, nil }

	warning := installedBinaryWarning(target)
	if !strings.Contains(warning, active) || !strings.Contains(warning, filepath.Dir(target)) {
		t.Fatalf("expected active/path skew warning, got %q", warning)
	}
}

func normalizeTestPath(t *testing.T, p string) string {
	t.Helper()

	clean := filepath.Clean(p)
	resolved, err := filepath.EvalSymlinks(clean)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return clean
}

func captureUpdateOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = stdoutW
	os.Stderr = stderrW
	runErr := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	outBytes, _ := io.ReadAll(stdoutR)
	errBytes, _ := io.ReadAll(stderrR)
	_ = stdoutR.Close()
	_ = stderrR.Close()
	return string(outBytes), string(errBytes), runErr
}
