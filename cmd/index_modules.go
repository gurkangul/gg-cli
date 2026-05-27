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
// root and participates in CodeGraph freshness. Dependency lockfiles are
// intentionally excluded: lockfile-only dependency version churn does not
// change source symbol/import topology, while module manifests can change the
// module layout and indexer inputs. Priority order matters when several
// manifests exist in one directory.
func manifestsForLang(lang runner.Lang) []string {
	switch lang {
	case runner.LangGo:
		return []string{"go.mod"}
	case runner.LangTypeScript:
		return []string{"package.json", "tsconfig.json", "jsconfig.json"}
	case runner.LangPython:
		return []string{"pyproject.toml", "setup.py", "setup.cfg", "Pipfile", "requirements.txt"}
	case runner.LangSwift:
		return []string{"Package.swift", "project.pbxproj", "contents.xcworkspacedata"}
	}
	return nil
}

// discoverModuleDirs walks the project root looking for language manifests.
// Root manifests preserve single-module behavior; nested manifests support
// monorepos using the same depth/skip settings as hook installation.
func discoverModuleDirs(projectRoot string, lang runner.Lang) ([]string, error) {
	if lang == runner.LangSwift {
		return discoverSwiftModuleDirs(projectRoot)
	}
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

func discoverSwiftModuleDirs(projectRoot string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(projectRoot, "Package.swift")); err == nil {
		return []string{projectRoot}, nil
	}
	if hasSwiftProjectAtRoot(projectRoot) {
		return []string{projectRoot}, nil
	}
	packageDirs, err := discoverSwiftPackageDirs(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(packageDirs) > 0 {
		return packageDirs, nil
	}
	dirs, err := discoverSwiftProjectDirs(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(dirs) > 0 {
		return dirs, nil
	}
	if hasSwiftSourceFiles(projectRoot) {
		return []string{projectRoot}, nil
	}
	return nil, nil
}

func hasSwiftProjectAtRoot(projectRoot string) bool {
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".xcodeproj" || ext == ".xcworkspace" {
			return true
		}
	}
	return false
}

func discoverSwiftPackageDirs(projectRoot string) ([]string, error) {
	skipDirs, maxDepth := hookInstallSettings()
	relDirs, err := findManifestDirs(projectRoot, "Package.swift", skipDirs, maxDepth)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	dirs := make([]string, 0, len(relDirs))
	for _, rel := range relDirs {
		if rel == "." {
			continue
		}
		abs := filepath.Join(projectRoot, rel)
		if seen[abs] {
			continue
		}
		seen[abs] = true
		dirs = append(dirs, abs)
	}
	return dirs, nil
}

func discoverSwiftProjectDirs(projectRoot string) ([]string, error) {
	skipDirs, maxDepth := hookInstallSettings()
	seen := make(map[string]bool)
	var dirs []string
	rootClean := filepath.Clean(projectRoot)
	err := filepath.WalkDir(rootClean, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path == rootClean {
			return nil
		}
		rel, relErr := filepath.Rel(rootClean, path)
		if relErr != nil {
			return nil
		}
		if skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		depth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if depth > maxDepth {
			return filepath.SkipDir
		}
		ext := filepath.Ext(d.Name())
		if ext != ".xcodeproj" && ext != ".xcworkspace" {
			return nil
		}
		parent := filepath.Dir(path)
		if !seen[parent] {
			seen[parent] = true
			dirs = append(dirs, parent)
		}
		return filepath.SkipDir
	})
	if err != nil {
		return nil, err
	}
	return dirs, nil
}

func hasSwiftSourceFiles(projectRoot string) bool {
	skipDirs, maxDepth := hookInstallSettings()
	found := false
	rootClean := filepath.Clean(projectRoot)
	_ = filepath.WalkDir(rootClean, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if d.IsDir() {
			if path == rootClean {
				return nil
			}
			rel, relErr := filepath.Rel(rootClean, path)
			if relErr != nil {
				return filepath.SkipDir
			}
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			depth := len(strings.Split(filepath.ToSlash(rel), "/"))
			if depth > maxDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) == ".swift" {
			found = true
		}
		return nil
	})
	return found
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
	case runner.LangSwift:
		return []string{".swift"}
	default:
		return nil
	}
}
