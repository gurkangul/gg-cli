// Package cmd — tests for `gg doctor --install-task-hooks` language detection
// and idempotent installation of pre/post task-done hook templates.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hookInstallFixture stands up a fake project with the requested manifests
// (go.mod, package.json) and a minimal .gg/config.yaml, changes into it, and
// returns the absolute paths used by the installer so tests can assert
// file presence / content.
type hookInstallFixture struct {
	root    string
	ggDir   string
	preDir  string
	postDir string
}

func newHookInstallFixture(t *testing.T, withGoMod, withPackageJSON bool) *hookInstallFixture {
	t.Helper()
	// Reuse setupGGDir for the .gg/config.yaml + HOME isolation + chdir.
	ggDir := setupGGDir(t)
	root := filepath.Dir(ggDir)

	if withGoMod {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
	}
	if withPackageJSON {
		if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"test","scripts":{"build":"echo ok","test":"echo ok"}}`), 0o644); err != nil {
			t.Fatalf("write package.json: %v", err)
		}
	}

	return &hookInstallFixture{
		root:    root,
		ggDir:   ggDir,
		preDir:  filepath.Join(ggDir, "hooks", "pre-task-done.d"),
		postDir: filepath.Join(ggDir, "hooks", "task-done.d"),
	}
}

// installerShebang returns true if the file at path starts with #!/ (any
// shebang), proving an executable script was written rather than an empty
// placeholder or wrong content.
func installerShebang(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.HasPrefix(string(data), "#!/")
}

func TestInstallTaskHooks_GoProject_InstallsPreAndPost(t *testing.T) {
	f := newHookInstallFixture(t, true /*go.mod*/, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	prePath := filepath.Join(f.preDir, "10-go-verify.sh")
	postPath := filepath.Join(f.postDir, "10-go-quality.sh")

	if !installerShebang(t, prePath) {
		t.Errorf("expected Go verify pre-hook at %s", prePath)
	}
	if !installerShebang(t, postPath) {
		t.Errorf("expected Go quality post-hook at %s", postPath)
	}

	// No Node pre-hook should be written when package.json is absent.
	nodePath := filepath.Join(f.preDir, "10-node-verify.sh")
	if _, err := os.Stat(nodePath); err == nil {
		t.Errorf("unexpected Node pre-hook written at %s", nodePath)
	}

	// Scripts must be executable (0100 owner-exec bit set) so hooks.RunHooks
	// will actually pick them up in lexicographic order.
	for _, p := range []string{prePath, postPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if info.Mode()&0o100 == 0 {
			t.Errorf("%s should be executable, got mode %v", p, info.Mode())
		}
	}
}

func TestInstallTaskHooks_NodeProject_InstallsPreOnly(t *testing.T) {
	f := newHookInstallFixture(t, false, true /*package.json*/)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	prePath := filepath.Join(f.preDir, "10-node-verify.sh")
	if !installerShebang(t, prePath) {
		t.Errorf("expected Node verify pre-hook at %s", prePath)
	}

	// No Go hooks when go.mod is absent.
	if _, err := os.Stat(filepath.Join(f.preDir, "10-go-verify.sh")); err == nil {
		t.Error("unexpected Go verify pre-hook in a Node-only project")
	}
	if _, err := os.Stat(filepath.Join(f.postDir, "10-go-quality.sh")); err == nil {
		t.Error("unexpected Go quality post-hook in a Node-only project")
	}
}

func TestInstallTaskHooks_PolyglotProject_InstallsBoth(t *testing.T) {
	f := newHookInstallFixture(t, true, true)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	for _, path := range []string{
		filepath.Join(f.preDir, "10-go-verify.sh"),
		filepath.Join(f.preDir, "10-node-verify.sh"),
		filepath.Join(f.postDir, "10-go-quality.sh"),
	} {
		if !installerShebang(t, path) {
			t.Errorf("expected hook at %s", path)
		}
	}
}

func TestInstallTaskHooks_Idempotent_DoesNotOverwriteUserEdits(t *testing.T) {
	f := newHookInstallFixture(t, true, false)

	if err := os.MkdirAll(f.preDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	custom := filepath.Join(f.preDir, "10-go-verify.sh")
	customBody := "#!/bin/sh\n# user customisation — must survive reinstall\nexit 0\n"
	if err := os.WriteFile(custom, []byte(customBody), 0o755); err != nil {
		t.Fatalf("write custom hook: %v", err)
	}

	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("read post-install: %v", err)
	}
	if string(got) != customBody {
		t.Errorf("installer overwrote user edits:\nwant:\n%s\ngot:\n%s", customBody, string(got))
	}
}

// Legacy post-hook idempotency: a user who ran the old installer has a
// monolithic .gg/hooks/task-done.d/10-go-quality.sh with no cd prefix and
// their own tweaks. The new installer MUST NOT overwrite it when re-run at
// the root — same guarantee the pre-hook enjoys (TASK-186).
func TestInstallTaskHooks_Idempotent_LegacyPostHookSurvives(t *testing.T) {
	f := newHookInstallFixture(t, true /*go.mod at root*/, false)

	if err := os.MkdirAll(f.postDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := filepath.Join(f.postDir, "10-go-quality.sh")
	legacyBody := "#!/bin/sh\n# pre-0.2 monolithic hook — user edits must survive\ngo test ./...\n"
	if err := os.WriteFile(legacy, []byte(legacyBody), 0o755); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	got, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatalf("read post-install: %v", err)
	}
	if string(got) != legacyBody {
		t.Errorf("installer clobbered legacy post-hook:\nwant:\n%s\ngot:\n%s", legacyBody, string(got))
	}
}

func TestInstallTaskHooks_UnknownProject_PrintsManualInstructionsAndExitsClean(t *testing.T) {
	newHookInstallFixture(t, false, false)
	// No go.mod, no package.json — installer must not error out; it prints a
	// manual-setup message and returns nil so CI scripts do not break on
	// projects without a detected language.
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Errorf("installer should no-op cleanly on unknown project, got: %v", err)
	}
}

// Monorepo: go.mod lives in lift-cli/, not the repo root. Installer must
// walk into the subdir, write a disambiguated filename, and inject the
// subdir path into the template body via the __GG_SUBDIR__ placeholder.
func TestInstallTaskHooks_MonorepoGoInSubdir_InstallsSubdirHook(t *testing.T) {
	f := newHookInstallFixture(t, false /* root has no go.mod */, false)
	subDir := filepath.Join(f.root, "lift-cli")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "go.mod"), []byte("module lift\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("write subdir go.mod: %v", err)
	}

	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	// Filename must be disambiguated so a second subdir can also land without
	// collision — base + '-' + slugified subpath.
	prePath := filepath.Join(f.preDir, "10-go-verify-lift-cli.sh")
	body, err := os.ReadFile(prePath)
	if err != nil {
		t.Fatalf("expected pre-hook at %s: %v", prePath, err)
	}
	s := string(body)
	if !strings.Contains(s, `cd "lift-cli"`) {
		t.Errorf("template must cd into lift-cli; got:\n%s", s)
	}
	if strings.Contains(s, "__GG_SUBDIR__") {
		t.Error("placeholder __GG_SUBDIR__ was not substituted")
	}
	// The post-hook wrapper should also land for the legacy advisory hook.
	postPath := filepath.Join(f.postDir, "10-go-quality-lift-cli.sh")
	postBody, err := os.ReadFile(postPath)
	if err != nil {
		t.Fatalf("expected wrapped post-hook at %s: %v", postPath, err)
	}
	ps := string(postBody)
	// Wrapped body must cd into the subdir so the advisory hook runs in the right directory.
	if !strings.Contains(ps, `cd "lift-cli"`) {
		t.Errorf("wrapped post-hook must cd into lift-cli; got:\n%s", ps)
	}
	// Exactly one shebang — wrapping must strip the original before prepending the new one.
	if n := strings.Count(ps, "#!/"); n != 1 {
		t.Errorf("wrapped post-hook must have exactly 1 shebang, got %d:\n%s", n, ps)
	}
	// The root-level bare filename must NOT exist — that would imply the
	// installer falsely detected a root manifest.
	if _, err := os.Stat(filepath.Join(f.preDir, "10-go-verify.sh")); err == nil {
		t.Error("unexpected root-level 10-go-verify.sh in a subdir-only project")
	}
}

// Skip-dir pruning: a go.mod placed inside node_modules (or similar) must be
// invisible to the installer. Otherwise every dep would add a verify gate.
func TestInstallTaskHooks_SkipsNodeModulesAndBuildArtifacts(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	for _, skip := range []string{"node_modules", ".git", "vendor", "dist"} {
		dir := filepath.Join(f.root, skip, "fake-pkg")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", skip, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n"), 0o644); err != nil {
			t.Fatalf("write fake go.mod: %v", err)
		}
	}
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	// No Go hook should have been installed — the only go.mod files live in
	// skipped directories.
	entries, _ := os.ReadDir(f.preDir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "go-verify") {
			t.Errorf("installer picked up a go.mod inside a skipped directory: %s", e.Name())
		}
	}
}

// Regression for BUG-017: Nuxt's bundled build output emits a synthetic
// `web/.output/server/package.json`. Before the fix, the installer treated
// that path as a real Node workspace and wrote a pre-task-done hook that
// later broke every `gg task done` with an `npm ci -w` workspace error.
// Modern framework build dirs (.output, .next, .vercel, .svelte-kit, .astro,
// .turbo, .nuxt, .cache, out) must be pruned from the walk.
func TestInstallTaskHooks_SkipsFrameworkBuildArtifacts_BUG017(t *testing.T) {
	f := newHookInstallFixture(t, false, true /*root package.json — a legit Node project*/)
	// Drop synthetic package.json files into each framework build output dir
	// so the installer has a chance to walk into them.
	artifactDirs := []string{
		"web/.output/server",
		"web/.next/standalone",
		".vercel/output/functions",
		".svelte-kit/output/server",
		".astro/server",
		".turbo/daemon",
		".nuxt/dist",
		".cache/build",
		"out/server",
	}
	for _, sub := range artifactDirs {
		dir := filepath.Join(f.root, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"build-artifact","private":true}`), 0o644); err != nil {
			t.Fatalf("write synthetic package.json in %s: %v", sub, err)
		}
	}

	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	// The only legit Node hook should be the root-level one — nothing slugified
	// after a framework build dir.
	entries, _ := os.ReadDir(f.preDir)
	for _, e := range entries {
		name := e.Name()
		if !strings.Contains(name, "node-verify") {
			continue
		}
		for _, bad := range []string{".output", ".next", ".vercel", ".svelte-kit", ".astro", ".turbo", ".nuxt", ".cache", "out-"} {
			if strings.Contains(name, bad) {
				t.Errorf("installer reached a framework build artifact: %s", name)
			}
		}
	}
}

// ── smoke gate + test-tier template tests (TASK-222) ──────────────────────────

// Smoke hook is always installed regardless of detected language, so teams
// can adopt the test-tier Makefile pattern incrementally. The hook self-skips
// at runtime when no Makefile / no test-smoke target is present.
func TestInstallTaskHooks_SmokeGate_AlwaysInstalled(t *testing.T) {
	// No go.mod, no package.json — but smoke hook must still land.
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}

	smokePath := filepath.Join(f.preDir, "05-smoke-e2e.sh")
	if !installerShebang(t, smokePath) {
		t.Errorf("expected smoke pre-hook at %s", smokePath)
	}
	// The file must be executable.
	info, err := os.Stat(smokePath)
	if err != nil {
		t.Fatalf("stat %s: %v", smokePath, err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("%s should be executable, got mode %v", smokePath, info.Mode())
	}
}

// Smoke hook also installed alongside language-specific hooks for Go projects.
func TestInstallTaskHooks_SmokeGate_InstalledWithGoProject(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	smokePath := filepath.Join(f.preDir, "05-smoke-e2e.sh")
	if !installerShebang(t, smokePath) {
		t.Errorf("expected smoke pre-hook at %s", smokePath)
	}
}

// Smoke hook content must contain the GG_NO_SMOKE opt-out and the make target
// check — these are the two runtime skip paths.
func TestInstallTaskHooks_SmokeGate_ContentHasSkipPaths(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(f.preDir, "05-smoke-e2e.sh"))
	if err != nil {
		t.Fatalf("read smoke hook: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"GG_NO_SMOKE",
		"test-smoke",
		"make test-smoke",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("smoke hook missing expected content %q", want)
		}
	}
}

// Makefile tier template is always written to .gg/templates/ so it is
// discoverable even on projects without a detected language manifest.
func TestInstallTaskHooks_MakefileTemplate_AlwaysInstalled(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	tmplPath := filepath.Join(f.ggDir, "templates", "makefile-test-tiers.mk")
	data, err := os.ReadFile(tmplPath)
	if err != nil {
		t.Fatalf("expected makefile template at %s: %v", tmplPath, err)
	}
	body := string(data)
	for _, target := range []string{"test-unit", "test-integration", "test-smoke", "test-e2e"} {
		if !strings.Contains(body, target) {
			t.Errorf("makefile template missing target %q", target)
		}
	}
}

// Re-running the installer must not overwrite the Makefile template if it
// already exists (idempotent, matches behavior of hook file installs).
func TestInstallTaskHooks_MakefileTemplate_Idempotent(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	tmplDir := filepath.Join(f.ggDir, "templates")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	custom := filepath.Join(tmplDir, "makefile-test-tiers.mk")
	customBody := "# my custom overrides\ntest-smoke:\n\techo ok\n"
	if err := os.WriteFile(custom, []byte(customBody), 0o644); err != nil {
		t.Fatalf("write custom template: %v", err)
	}
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("read after install: %v", err)
	}
	if string(got) != customBody {
		t.Errorf("installer overwrote custom makefile template")
	}
}

// ── wrapLegacyPostHook unit tests ─────────────────────────────────────────────

func TestWrapLegacyPostHook_SubdirInjectsCDAndSingleShebang(t *testing.T) {
	body := "#!/bin/sh\nset -e\ngo build ./...\n"
	got := wrapLegacyPostHook(body, "lift-cli")

	// Exactly one shebang — the original must be stripped before the new header is prepended.
	if n := strings.Count(got, "#!/"); n != 1 {
		t.Errorf("expected exactly 1 shebang, got %d:\n%s", n, got)
	}
	// cd injection into the named subdirectory.
	if !strings.Contains(got, `cd "lift-cli"`) {
		t.Errorf("expected cd into lift-cli:\n%s", got)
	}
	// Original commands must still be present.
	if !strings.Contains(got, "go build ./...") {
		t.Errorf("original body was lost:\n%s", got)
	}
}

func TestWrapLegacyPostHook_RootCaseReturnsBodyUnchanged(t *testing.T) {
	body := "#!/bin/sh\nset -e\ngo build ./...\n"
	for _, sub := range []string{".", ""} {
		got := wrapLegacyPostHook(body, sub)
		if got != body {
			t.Errorf("sub=%q: body should be unchanged, got:\n%s", sub, got)
		}
	}
}

func TestWrapLegacyPostHook_BodyWithoutShebang_WrapsCleanly(t *testing.T) {
	body := "set -e\ngo build ./...\n"
	got := wrapLegacyPostHook(body, "sub")
	if n := strings.Count(got, "#!/"); n != 1 {
		t.Errorf("expected 1 shebang from wrapper, got %d:\n%s", n, got)
	}
}

// ── lint gate tests (TASK-301) ────────────────────────────────────────────────

// 60-lint-gate.sh is always installed alongside the other language-agnostic
// gates. It self-skips at runtime when no lint-baseline.json is present.
func TestInstallTaskHooks_LintGate_AlwaysInstalled(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	lintPath := filepath.Join(f.preDir, "60-lint-gate.sh")
	if !installerShebang(t, lintPath) {
		t.Errorf("expected lint gate at %s", lintPath)
	}
	// Must be executable.
	info, err := os.Stat(lintPath)
	if err != nil {
		t.Fatalf("stat %s: %v", lintPath, err)
	}
	if info.Mode()&0o100 == 0 {
		t.Errorf("%s should be executable, got mode %v", lintPath, info.Mode())
	}
}

// Lint gate is also installed alongside Go-project hooks.
func TestInstallTaskHooks_LintGate_InstalledWithGoProject(t *testing.T) {
	f := newHookInstallFixture(t, true, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	lintPath := filepath.Join(f.preDir, "60-lint-gate.sh")
	if !installerShebang(t, lintPath) {
		t.Errorf("expected lint gate at %s", lintPath)
	}
}

// Lint gate content must reference the baseline file path and the bypass env var.
func TestInstallTaskHooks_LintGate_ContentHasKeySignals(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(f.preDir, "60-lint-gate.sh"))
	if err != nil {
		t.Fatalf("read lint hook: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		"lint-baseline.json",
		"GG_LINT_GATE",
		"GG_ALLOW_LINT_REGRESSIONS",
		"golangci-lint",
		"issue_count",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("lint gate hook missing expected content %q", body)
		}
	}
}

// Idempotency: re-running installer must not overwrite user edits to lint gate.
func TestInstallTaskHooks_LintGate_Idempotent(t *testing.T) {
	f := newHookInstallFixture(t, false, false)
	if err := os.MkdirAll(f.preDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	custom := filepath.Join(f.preDir, "60-lint-gate.sh")
	customBody := "#!/bin/sh\n# user override — must survive reinstall\nexit 0\n"
	if err := os.WriteFile(custom, []byte(customBody), 0o755); err != nil {
		t.Fatalf("write custom lint gate: %v", err)
	}
	if err := runDoctorInstallTaskHooks(); err != nil {
		t.Fatalf("installer: %v", err)
	}
	got, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("read post-install: %v", err)
	}
	if string(got) != customBody {
		t.Errorf("installer overwrote user edits to lint gate")
	}
}
