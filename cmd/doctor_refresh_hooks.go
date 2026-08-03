package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/templates"
)

type hookTemplateSpec struct {
	path string
	name string
	body string
}

type hookTemplateState string

const (
	hookTemplateInSync         hookTemplateState = "in-sync"
	hookTemplateDrift          hookTemplateState = "drift"
	hookTemplateUserCustomized hookTemplateState = "user-customized"
)

type hookTemplateCheck struct {
	spec       hookTemplateSpec
	state      hookTemplateState
	markerSHA  string
	currentSHA string
	actualSHA  string
}

func collectHookTemplateSpecs() ([]hookTemplateSpec, error) {
	ggDir, err := config.GGDir()
	if err != nil {
		return nil, err
	}
	projectRoot, err := config.FindRoot()
	if err != nil {
		return nil, err
	}

	preDir := filepath.Join(ggDir, "hooks", "pre-task-done.d")
	postDir := filepath.Join(ggDir, "hooks", "task-done.d")
	preCommitDir := filepath.Join(ggDir, "hooks", "pre-commit.d")

	skipDirs, maxDepth := hookInstallSettings()
	goDirs, err := findManifestDirs(projectRoot, "go.mod", skipDirs, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("walk for go.mod: %w", err)
	}
	nodeDirs, err := findManifestDirs(projectRoot, "package.json", skipDirs, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("walk for package.json: %w", err)
	}

	var specs []hookTemplateSpec
	for _, sub := range goDirs {
		specs = append(specs,
			hookTemplateSpec{
				path: filepath.Join(preDir, hookFileName("10-go-verify", sub)),
				name: "PreTaskDoneGoHook",
				body: strings.ReplaceAll(templates.PreTaskDoneGoHook, "__GG_SUBDIR__", sub),
			},
			hookTemplateSpec{
				path: filepath.Join(postDir, hookFileName("10-go-quality", sub)),
				name: "TaskDoneGoHook",
				body: wrapLegacyPostHook(templates.TaskDoneGoHook, sub),
			},
		)
	}
	for _, sub := range nodeDirs {
		specs = append(specs, hookTemplateSpec{
			path: filepath.Join(preDir, hookFileName("10-node-verify", sub)),
			name: "PreTaskDoneNodeHook",
			body: strings.ReplaceAll(templates.PreTaskDoneNodeHook, "__GG_SUBDIR__", sub),
		})
	}

	specs = append(specs,
		hookTemplateSpec{path: filepath.Join(preDir, "90-bug-repros.sh"), name: "BugReprosHook", body: templates.BugReprosHook},
		hookTemplateSpec{path: filepath.Join(preDir, "05-smoke-e2e.sh"), name: "SmokeE2EHook", body: templates.SmokeE2EHook},
		hookTemplateSpec{path: filepath.Join(preDir, "20-decide-capture.sh"), name: "PreTaskDoneDecideCaptureHook", body: templates.PreTaskDoneDecideCaptureHook},
		hookTemplateSpec{path: filepath.Join(preDir, "30-file-size.sh"), name: "FileSizeGateHook", body: templates.FileSizeGateHook},
		hookTemplateSpec{path: filepath.Join(preDir, "35-stub-scan.sh"), name: "StubScanGateHook", body: templates.StubScanGateHook},
		hookTemplateSpec{path: filepath.Join(preDir, "45-dependency-justification.sh"), name: "DependencyJustificationGateHook", body: templates.DependencyJustificationGateHook},
		hookTemplateSpec{path: filepath.Join(preDir, "40-review-required.sh"), name: "PreTaskDoneReviewRequiredHook", body: templates.PreTaskDoneReviewRequiredHook},
		hookTemplateSpec{path: filepath.Join(preDir, "50-ac-attestation.sh"), name: "PreTaskDoneACAttestationHook", body: templates.PreTaskDoneACAttestationHook},
		hookTemplateSpec{path: filepath.Join(preDir, "60-lint-gate.sh"), name: "PreTaskDoneLintGateHook", body: templates.PreTaskDoneLintGateHook},
		hookTemplateSpec{path: filepath.Join(preDir, "60-impact-attestation.sh"), name: "PreTaskDoneImpactAttestationHook", body: templates.PreTaskDoneImpactAttestationHook},
		hookTemplateSpec{path: filepath.Join(preCommitDir, "20-secret-scan.sh"), name: "PreCommitSecretScanHook", body: templates.PreCommitSecretScanHook},
	)
	return specs, nil
}

func checkHookTemplates() ([]hookTemplateCheck, error) {
	specs, err := collectHookTemplateSpecs()
	if err != nil {
		return nil, err
	}
	checks := make([]hookTemplateCheck, 0, len(specs))
	for _, spec := range specs {
		data, err := os.ReadFile(spec.path) //nolint:gosec
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", spec.path, err)
		}
		body := string(data)
		_, markerSHA, marked := templates.ParseHookTemplateMarker(body)
		currentSHA := templates.HookTemplateHash(spec.body)
		actualSHA := templates.HookTemplateHash(body)
		state := hookTemplateInSync
		switch {
		case !marked:
			state = hookTemplateUserCustomized
		case markerSHA != currentSHA || actualSHA != currentSHA:
			state = hookTemplateDrift
		}
		checks = append(checks, hookTemplateCheck{
			spec:       spec,
			state:      state,
			markerSHA:  markerSHA,
			currentSHA: currentSHA,
			actualSHA:  actualSHA,
		})
	}
	return checks, nil
}

func doctorCheckHookTemplates(report *doctorReport) (driftCount int, customizedCount int) {
	checks, err := checkHookTemplates()
	if err != nil {
		report.fail("hook templates", err.Error())
		return 1, 0
	}
	if len(checks) == 0 {
		report.ok("hook templates", "no deployed hook templates found")
		return 0, 0
	}
	for _, check := range checks {
		label := filepath.ToSlash(check.spec.path)
		switch check.state {
		case hookTemplateInSync:
			report.ok(label, "in-sync")
		case hookTemplateDrift:
			driftCount++
			report.fail(label, "drift — run `gg doctor --refresh-hooks` ("+hookTemplateDriftDetail(check)+")")
		case hookTemplateUserCustomized:
			customizedCount++
			report.warn(label, "user-customized — no gg-template-sha256 marker; left alone")
		}
	}
	return driftCount, customizedCount
}

func runDoctorRefreshHooks(force bool) error {
	checks, err := checkHookTemplates()
	if err != nil {
		return err
	}
	if force {
		fmt.Println("--refresh-hooks-force will overwrite user-customized hooks; backups in .bak.<ts>")
	}

	var refreshed, skipped, drifted int
	ts := time.Now().Unix()
	for _, check := range checks {
		shouldRefresh := check.state == hookTemplateDrift || (force && check.state == hookTemplateUserCustomized)
		if check.state == hookTemplateDrift {
			drifted++
		}
		if check.state == hookTemplateUserCustomized && !force {
			fmt.Printf("skipped %s (no marker — user-customized)\n", check.spec.path)
			skipped++
			continue
		}
		if !shouldRefresh {
			continue
		}
		backup := fmt.Sprintf("%s.bak.%d", check.spec.path, ts)
		if err := copyFile(check.spec.path, backup, 0o755); err != nil {
			return fmt.Errorf("backup %s: %w", check.spec.path, err)
		}
		body := templates.WithHookTemplateMarker(check.spec.name, check.spec.body)
		if err := os.WriteFile(check.spec.path, []byte(body), 0o755); err != nil {
			return fmt.Errorf("write %s: %w", check.spec.path, err)
		}
		fmt.Printf("refreshed %s (%s); backup at %s\n", check.spec.path, hookTemplateDriftDetail(check), backup)
		refreshed++
	}
	if refreshed == 0 && skipped == 0 && drifted == 0 {
		fmt.Println("all hook templates in sync")
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src) //nolint:gosec
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode) //nolint:gosec // dst is selected from managed hook specs, not user input.
}

func shortSHA(sha string) string {
	if len(sha) < 8 {
		if sha == "" {
			return "none"
		}
		return sha
	}
	return sha[:8]
}

func hookTemplateDriftDetail(check hookTemplateCheck) string {
	if check.markerSHA != check.currentSHA {
		return fmt.Sprintf("was %s, now %s", shortSHA(check.markerSHA), shortSHA(check.currentSHA))
	}
	if check.actualSHA != check.currentSHA {
		return fmt.Sprintf("body %s, template %s", shortSHA(check.actualSHA), shortSHA(check.currentSHA))
	}
	return fmt.Sprintf("was %s, now %s", shortSHA(check.markerSHA), shortSHA(check.currentSHA))
}
