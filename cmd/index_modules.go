package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gurkangul/gg-cli/internal/index/runner"
)

// readModulePath reads the Go module path from go.mod in the project root.
// Returns "" if go.mod is absent or the module line is not found.
func readModulePath(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.SplitN(string(data), "\n", 20) {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

// manifestsForLang returns manifest filenames whose presence marks a module
// root. Priority order matters when several manifests exist in one directory.
func manifestsForLang(lang runner.Lang) []string {
	switch lang {
	case runner.LangGo:
		return []string{"go.mod"}
	case runner.LangTypeScript:
		return []string{"package.json", "tsconfig.json", "jsconfig.json"}
	case runner.LangPython:
		return []string{"pyproject.toml", "setup.py", "setup.cfg", "Pipfile", "requirements.txt"}
	}
	return nil
}

// discoverModuleDirs walks the project root looking for language manifests.
// Root manifests preserve single-module behavior; nested manifests support
// monorepos using the same depth/skip settings as hook installation.
func discoverModuleDirs(projectRoot string, lang runner.Lang) ([]string, error) {
	manifests := manifestsForLang(lang)
	if len(manifests) == 0 {
		return nil, fmt.Errorf("unsupported language %q", lang)
	}
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(projectRoot, m)); err == nil {
			if lang == runner.LangTypeScript && m == "package.json" && !hasTypeScriptConfig(projectRoot) {
				nested, nestedErr := discoverNestedModuleDirs(projectRoot, lang)
				if nestedErr != nil {
					return nil, nestedErr
				}
				if len(nested) > 0 {
					return nested, nil
				}
			}
			return []string{projectRoot}, nil
		}
	}
	skipDirs, maxDepth := hookInstallSettings()
	seen := make(map[string]bool)
	var absDirs []string
	for _, m := range manifests {
		relDirs, err := findManifestDirs(projectRoot, m, skipDirs, maxDepth)
		if err != nil {
			return nil, err
		}
		for _, rel := range relDirs {
			abs := projectRoot
			if rel != "." {
				abs = filepath.Join(projectRoot, rel)
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			absDirs = append(absDirs, abs)
		}
	}
	if len(absDirs) == 0 && hasSourceDirForLang(projectRoot, lang) {
		return []string{projectRoot}, nil
	}
	return absDirs, nil
}

func hasSourceDirForLang(projectRoot string, lang runner.Lang) bool {
	if lang == runner.LangGo {
		return false
	}
	srcDir := filepath.Join(projectRoot, "src")
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return false
	}
	extSet := make(map[string]bool)
	for _, ext := range langExtensions(lang) {
		extSet[ext] = true
	}
	found := false
	_ = filepath.WalkDir(srcDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if extSet[filepath.Ext(d.Name())] {
			found = true
		}
		return nil
	})
	return found
}

func discoverNestedModuleDirs(projectRoot string, lang runner.Lang) ([]string, error) {
	manifests := manifestsForLang(lang)
	skipDirs, maxDepth := hookInstallSettings()
	seen := make(map[string]bool)
	var absDirs []string
	for _, m := range manifests {
		relDirs, err := findManifestDirs(projectRoot, m, skipDirs, maxDepth)
		if err != nil {
			return nil, err
		}
		for _, rel := range relDirs {
			if rel == "." {
				continue
			}
			abs := filepath.Join(projectRoot, rel)
			if seen[abs] {
				continue
			}
			seen[abs] = true
			absDirs = append(absDirs, abs)
		}
	}
	return absDirs, nil
}

func hasTypeScriptConfig(dir string) bool {
	for _, name := range []string{"tsconfig.json", "jsconfig.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// langExtensions maps a Lang to the file extensions its indexer handles.
func langExtensions(lang runner.Lang) []string {
	switch lang {
	case runner.LangGo:
		return []string{".go"}
	case runner.LangPython:
		return []string{".py"}
	case runner.LangTypeScript:
		return []string{".ts", ".tsx", ".js", ".jsx"}
	default:
		return nil
	}
}
