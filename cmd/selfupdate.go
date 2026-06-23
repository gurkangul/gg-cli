package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// GitHub release metadata for gg-cli self-update. Replacing the old
// `go install …@latest` path (which can never reach v2.x because the module
// path lacks a /v2 suffix), self-update downloads the platform release asset
// published by goreleaser and atomically swaps the running binary.
const (
	// ggReleaseAPIDefault is the GitHub latest-release endpoint. Overridable in
	// tests via ggReleaseAPIBase so no real network is touched.
	ggReleaseAPIDefault = "https://api.github.com/repos/gurkangul/gg-cli/releases/latest"

	// ggUserAgent identifies gg to the GitHub API (the API rejects requests
	// without a User-Agent header with 403).
	ggUserAgent = "gg-cli-self-update"

	// checksumsAssetName is the goreleaser checksum manifest published alongside
	// the per-platform archives.
	checksumsAssetName = "checksums.txt"
)

var (
	// ggReleaseAPIBase is the latest-release endpoint. Tests point this at an
	// httptest server. Production uses the GitHub API.
	ggReleaseAPIBase = ggReleaseAPIDefault

	// selfUpdateExecutable resolves the running binary path. Tests override it so
	// the real gg binary is never clobbered.
	selfUpdateExecutable = os.Executable

	// selfUpdateHTTPClient is the HTTP client used for release + asset fetches.
	// Default timeout is generous enough for the asset download; the latest-tag
	// lookup uses its own short per-request context.
	selfUpdateHTTPClient = &http.Client{Timeout: 60 * time.Second}
)

// ghAsset is one published release asset.
type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// ghRelease is the subset of the GitHub latest-release payload we consume.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// isDevBuild reports whether v is a source/dev build rather than a clean
// release tag. Release binaries get an exact `vMAJOR.MINOR.PATCH` ldflag; source
// builds get "dev", "" or a Go VCS pseudo-version (which carries a '-'). A
// dev build must never be clobbered by a release binary without --force, so the
// maintainer's `gg update --from-source` binary survives auto-update.
func isDevBuild(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return true
	}
	// A clean release tag is exactly "vMAJOR.MINOR.PATCH" with no pre-release or
	// build suffix. Anything carrying '-' (dev-<hash>, v1.1.1-0.<date>-<hash>)
	// is a source/pseudo-version build.
	if strings.ContainsAny(v, "-+") {
		return true
	}
	_, _, ok := parseSemverCore(v)
	return !ok
}

// releaseAssetName returns the goreleaser archive name for the running platform.
// goreleaser names archives `gg_<VERSION>_<OS>_<ARCH>.tar.gz` where VERSION has
// no leading 'v', OS is runtime.GOOS verbatim (lowercase: darwin/linux/windows)
// and ARCH is runtime.GOARCH. tag is the release tag ("vX.Y.Z"); the leading 'v'
// is stripped.
func releaseAssetName(tag, goos, goarch string) string {
	version := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("gg_%s_%s_%s.%s", version, goos, goarch, ext)
}

// fetchLatestRelease resolves the latest gg release via the GitHub API. The
// caller supplies a bounded context so a slow/offline network can never hang.
func fetchLatestRelease(ctx context.Context) (ghRelease, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ggReleaseAPIBase, nil)
	if err != nil {
		return ghRelease{}, fmt.Errorf("build release request: %w", err)
	}
	req.Header.Set("User-Agent", ggUserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := selfUpdateHTTPClient.Do(req)
	if err != nil {
		return ghRelease{}, fmt.Errorf("contact GitHub release API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return ghRelease{}, fmt.Errorf("GitHub release API rate-limited (HTTP 403); retry later or use `gg update --from-source`")
	}
	if resp.StatusCode != http.StatusOK {
		return ghRelease{}, fmt.Errorf("GitHub release API returned HTTP %d", resp.StatusCode)
	}

	var rel ghRelease
	// Bound the body read so a hostile/huge response can't exhaust memory.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return ghRelease{}, fmt.Errorf("parse GitHub release JSON: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return ghRelease{}, fmt.Errorf("GitHub release API returned no tag_name")
	}
	return rel, nil
}

// findAsset returns the asset matching name (case-insensitive), or false.
func findAsset(rel ghRelease, name string) (ghAsset, bool) {
	for _, a := range rel.Assets {
		if strings.EqualFold(a.Name, name) {
			return a, true
		}
	}
	return ghAsset{}, false
}

// selfUpdateOutcome reports the result of selfUpdateToRelease.
type selfUpdateOutcome struct {
	OldVersion string
	NewVersion string
	ExePath    string
}

// selfUpdateToRelease downloads the platform asset for rel, verifies its
// sha256 against the published checksums.txt, extracts the `gg` binary from the
// tar.gz, and atomically replaces the running executable. On ANY failure the
// existing binary is left untouched (the swap is the very last, atomic step).
//
// This is the single self-update routine shared by `gg update` and the
// throttled session-start auto-update.
func selfUpdateToRelease(ctx context.Context, rel ghRelease, current string) (selfUpdateOutcome, error) {
	if runtime.GOOS == "windows" {
		return selfUpdateOutcome{}, fmt.Errorf("automatic binary replacement is not supported on windows; download %s from the release page and replace gg.exe manually",
			releaseAssetName(rel.TagName, runtime.GOOS, runtime.GOARCH))
	}

	exePath, err := selfUpdateExecutable()
	if err != nil {
		return selfUpdateOutcome{}, fmt.Errorf("locate running gg binary: %w", err)
	}
	if resolved, rErr := filepath.EvalSymlinks(exePath); rErr == nil {
		exePath = resolved
	}

	assetName := releaseAssetName(rel.TagName, runtime.GOOS, runtime.GOARCH)
	asset, ok := findAsset(rel, assetName)
	if !ok {
		return selfUpdateOutcome{}, fmt.Errorf("release %s has no asset for this platform (%s)", rel.TagName, assetName)
	}

	// Download the archive into memory (release archives are a few MB).
	archive, err := downloadAsset(ctx, asset.URL)
	if err != nil {
		return selfUpdateOutcome{}, fmt.Errorf("download %s: %w", assetName, err)
	}

	// Verify sha256 against checksums.txt before trusting the bytes.
	if err := verifyAssetChecksum(ctx, rel, assetName, archive); err != nil {
		return selfUpdateOutcome{}, err
	}

	// Extract the `gg` binary from the verified tar.gz.
	binary, err := extractGGFromTarGz(archive)
	if err != nil {
		return selfUpdateOutcome{}, fmt.Errorf("extract gg from %s: %w", assetName, err)
	}

	// Atomic replace: write to a temp file in the SAME directory as the running
	// binary, chmod 0755, then rename over it (works on unix while running).
	if err := atomicReplaceExecutable(exePath, binary); err != nil {
		return selfUpdateOutcome{}, err
	}

	return selfUpdateOutcome{OldVersion: current, NewVersion: rel.TagName, ExePath: exePath}, nil
}

// downloadAsset GETs url and returns the full body, bounded to 128 MiB.
func downloadAsset(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ggUserAgent)
	resp, err := selfUpdateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

// verifyAssetChecksum downloads checksums.txt for rel and confirms the sha256 of
// archive matches the line for assetName. A mismatch (or a missing checksum
// line) is a hard error so a corrupted/tampered download never replaces gg.
func verifyAssetChecksum(ctx context.Context, rel ghRelease, assetName string, archive []byte) error {
	sums, ok := findAsset(rel, checksumsAssetName)
	if !ok {
		return fmt.Errorf("release %s has no %s; refusing to install unverified binary", rel.TagName, checksumsAssetName)
	}
	body, err := downloadAsset(ctx, sums.URL)
	if err != nil {
		return fmt.Errorf("download %s: %w", checksumsAssetName, err)
	}
	want, ok := checksumFor(string(body), assetName)
	if !ok {
		return fmt.Errorf("%s has no checksum entry for %s", checksumsAssetName, assetName)
	}
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s — aborting, binary untouched", assetName, want, got)
	}
	return nil
}

// checksumFor parses a goreleaser checksums.txt body (`<sha256>  <filename>`
// per line) and returns the hex digest for name.
func checksumFor(manifest, name string) (string, bool) {
	for _, line := range strings.Split(manifest, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if fields[1] == name {
			return fields[0], true
		}
	}
	return "", false
}

// extractGGFromTarGz reads a gzip-compressed tar archive and returns the bytes
// of the entry named "gg" (the binary goreleaser packs into each archive).
func extractGGFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if filepath.Base(hdr.Name) == "gg" && hdr.Typeflag == tar.TypeReg {
			data, err := io.ReadAll(io.LimitReader(tr, 256<<20))
			if err != nil {
				return nil, fmt.Errorf("read gg entry: %w", err)
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("archive does not contain a `gg` binary")
}

// atomicReplaceExecutable writes binary to a temp file in the same directory as
// exePath, makes it executable, and renames it over exePath. Same-directory
// rename keeps the swap atomic and avoids cross-device EXDEV. On unix the rename
// succeeds even while the old binary is executing.
func atomicReplaceExecutable(exePath string, binary []byte) error {
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, ".gg-update-*")
	if err != nil {
		return fmt.Errorf("create temp binary in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(binary); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp binary: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod temp binary: %w", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("replace %s: %w", exePath, err)
	}
	return nil
}
