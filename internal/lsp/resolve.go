package lsp

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ServerSpec describes how to launch the language server for one file
// extension: the command, its args, and the LSP languageId to advertise in
// textDocument/didOpen.
type ServerSpec struct {
	Cmd        string
	Args       []string
	LanguageID string
	// InstallHint is shown when Cmd is not on PATH so the error is actionable.
	InstallHint string
}

// servers maps a lowercased file extension (with leading dot) to its language
// server. MVP wires Go (gopls); adding ts/py/rust later is a one-line entry —
// e.g. ".ts" -> typescript-language-server --stdio, ".py" -> pylsp.
var servers = map[string]ServerSpec{
	".go": {
		Cmd:         "gopls",
		Args:        nil,
		LanguageID:  "go",
		InstallHint: "go install golang.org/x/tools/gopls@latest",
	},
}

// ResolveServer returns the ServerSpec for the target file's extension. An
// unknown extension yields a clear "no language server configured" error.
func ResolveServer(path string) (ServerSpec, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return ServerSpec{}, fmt.Errorf("no language server configured for %q (no file extension)", filepath.Base(path))
	}
	spec, ok := servers[ext]
	if !ok {
		return ServerSpec{}, fmt.Errorf("no language server configured for %s", ext)
	}
	return spec, nil
}

// ensureOnPath verifies the server binary is resolvable on PATH, returning an
// actionable error (with the install hint) when it is not. This is checked
// before spawning so a missing server never panics — it surfaces as a clean,
// non-crashing CLI error.
func (s ServerSpec) ensureOnPath() error {
	if _, err := exec.LookPath(s.Cmd); err != nil {
		hint := ""
		if s.InstallHint != "" {
			hint = " — install: " + s.InstallHint
		}
		return fmt.Errorf("%s not found on PATH%s", s.Cmd, hint)
	}
	return nil
}
