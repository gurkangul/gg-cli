package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Write-boundary verification for a source file",
	Long: `Run language-appropriate fast checks on a single file.
Used as a PostToolUse hook to catch regressions at the write boundary
before they compound. Budget: ≤2s per file.

For Go files: gofmt (formatting) + go vet (on the package).
Other languages: skipped (exits 0).

Wire as a Claude Code PostToolUse hook — see docs/claude-code-integration.md.`,
	RunE: runVerify,
}

var verifyFilePath string

func init() {
	verifyCmd.Flags().StringVar(&verifyFilePath, "file", "", "source file to verify (required)")
	_ = verifyCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(verifyCmd)
}

// verifyResult is the structured output for --json.
type verifyResult struct {
	File       string        `json:"file"`
	Lang       string        `json:"lang"`
	Checks     []verifyCheck `json:"checks"`
	Passed     bool          `json:"passed"`
	DurationMs int64         `json:"duration_ms"`
}

type verifyCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Output string `json:"output,omitempty"`
}

func runVerify(_ *cobra.Command, _ []string) error {
	start := time.Now()

	lang := detectLangFromExt(verifyFilePath)
	result := verifyResult{
		File: verifyFilePath,
		Lang: lang,
	}

	switch lang {
	case "go":
		result.Checks = runGoChecks(verifyFilePath)
	default:
		// No checks for this language yet — not a failure.
		result.Checks = []verifyCheck{}
		result.Passed = true
		result.DurationMs = time.Since(start).Milliseconds()
		return outputVerify(result)
	}

	allPassed := true
	for _, c := range result.Checks {
		if !c.Passed {
			allPassed = false
			break
		}
	}
	result.Passed = allPassed
	result.DurationMs = time.Since(start).Milliseconds()

	if err := outputVerify(result); err != nil {
		return err
	}
	if !result.Passed {
		return &ExitError{Code: ExitVerifyFailed, Message: fmt.Sprintf("verify failed: %s", verifyFilePath)}
	}
	return nil
}

func detectLangFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	case ".js", ".jsx":
		return "javascript"
	default:
		return "unknown"
	}
}

func runGoChecks(filePath string) []verifyCheck {
	var checks []verifyCheck

	// gofmt: check whether the file needs formatting.
	checks = append(checks, runGofmt(filePath))

	// go vet: static analysis on the containing package.
	checks = append(checks, runGoVet(filePath))

	return checks
}

func runGofmt(filePath string) verifyCheck {
	//nolint:gosec // filePath is validated by cobra flag
	out, err := exec.Command("gofmt", "-l", filePath).CombinedOutput()
	trimmed := strings.TrimSpace(string(out))
	if err != nil {
		return verifyCheck{Name: "gofmt", Passed: false, Output: fmt.Sprintf("gofmt error: %v", err)}
	}
	if trimmed != "" {
		return verifyCheck{Name: "gofmt", Passed: false, Output: filePath + " needs formatting (run gofmt -w " + filePath + ")"}
	}
	return verifyCheck{Name: "gofmt", Passed: true}
}

func runGoVet(filePath string) verifyCheck {
	// Run go vet on the package containing the file.
	dir := filepath.Dir(filePath)
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	// Vet the file's containing package — faster than the full module and catches
	// the relevant issues. The "." target refers to the package in cmd.Dir.
	//nolint:gosec // dir is derived from a validated file path
	cmd := exec.Command("go", "vet", ".")
	cmd.Dir = dir

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		return verifyCheck{Name: "go-vet", Passed: false, Output: out}
	}
	return verifyCheck{Name: "go-vet", Passed: true}
}

func outputVerify(r verifyResult) error {
	return printJSON(r, func() {
		icon := "✓"
		if !r.Passed {
			icon = "✗"
		}
		fmt.Printf("%s verify %s [%s] %dms\n", icon, r.File, r.Lang, r.DurationMs)
		for _, c := range r.Checks {
			cIcon := "  ✓"
			if !c.Passed {
				cIcon = "  ✗"
			}
			fmt.Printf("%s %s\n", cIcon, c.Name)
			if c.Output != "" {
				fmt.Printf("    %s\n", strings.ReplaceAll(c.Output, "\n", "\n    "))
			}
		}
	})
}

