package lsp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireGoplsOrSkip skips when gopls is not on PATH, mirroring the project's
// Ollama-skip pattern so CI without gopls passes rather than fails.
func requireGoplsOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH — skipping live LSP test (install: go install golang.org/x/tools/gopls@latest)")
	}
}

// writeFixtureModule lays down a minimal Go module so gopls can resolve
// cross-file references type-aware. Returns the module root.
func writeFixtureModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module lspfixture\n\ngo 1.21\n",
		"lib.go": `package lspfixture

// Greet builds a greeting for name.
func Greet(name string) string {
	return "hi " + name
}
`,
		"use.go": `package lspfixture

func caller() string {
	return Greet("a") + Greet("b")
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestGoplsReferences(t *testing.T) {
	requireGoplsOrSkip(t)
	root := writeFixtureModule(t)
	libFile := filepath.Join(root, "lib.go")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// "Greet" is at line 4 (1-based), col 6 (after "func ").
	res, err := Query(ctx, KindReferences, libFile, 4, 6, root)
	if err != nil {
		t.Fatalf("references: %v", err)
	}
	// Declaration + two call sites in use.go = 3 references.
	if len(res.Locations) < 2 {
		t.Fatalf("expected >=2 references (decl + callers), got %d: %+v", len(res.Locations), res.Locations)
	}
	var sawUse bool
	for _, l := range res.Locations {
		if strings.HasSuffix(URIToPath(l.URI), "use.go") {
			sawUse = true
		}
	}
	if !sawUse {
		t.Fatalf("references did not include the caller in use.go: %+v", res.Locations)
	}
}

func TestGoplsDefinition(t *testing.T) {
	requireGoplsOrSkip(t)
	root := writeFixtureModule(t)
	useFile := filepath.Join(root, "use.go")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// In use.go, the first "Greet(" call is on line 4; col 9 lands on the G.
	res, err := Query(ctx, KindDefinition, useFile, 4, 9, root)
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	if len(res.Locations) == 0 {
		t.Fatalf("expected a definition location, got none")
	}
	if !strings.HasSuffix(URIToPath(res.Locations[0].URI), "lib.go") {
		t.Fatalf("definition should point at lib.go, got %s", URIToPath(res.Locations[0].URI))
	}
}

func TestGoplsHover(t *testing.T) {
	requireGoplsOrSkip(t)
	root := writeFixtureModule(t)
	libFile := filepath.Join(root, "lib.go")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := Query(ctx, KindHover, libFile, 4, 6, root)
	if err != nil {
		t.Fatalf("hover: %v", err)
	}
	if !strings.Contains(res.Hover.PlainText, "Greet") {
		t.Fatalf("hover should mention Greet, got: %q", res.Hover.PlainText)
	}
}
