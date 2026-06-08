package cmd

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gurkangul/gg-cli/internal/config"
	"github.com/gurkangul/gg-cli/internal/index/changed"
	"github.com/gurkangul/gg-cli/internal/index/runner"
)

type codeGraphChangeSummary struct {
	ChangedFiles int
	NewFiles     int
	DeletedFiles int
	ModuleFiles  int
}

func (s codeGraphChangeSummary) hasChanges() bool {
	return s.ChangedFiles+s.NewFiles+s.DeletedFiles+s.ModuleFiles > 0
}

func (s codeGraphChangeSummary) detail(command string) string {
	var parts []string
	if s.ChangedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d changed %s", s.ChangedFiles, codeGraphPlural(s.ChangedFiles, "file", "files")))
	}
	if s.NewFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d new %s", s.NewFiles, codeGraphPlural(s.NewFiles, "file", "files")))
	}
	if s.DeletedFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted %s", s.DeletedFiles, codeGraphPlural(s.DeletedFiles, "file", "files")))
	}
	if s.ModuleFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d module %s changed", s.ModuleFiles, codeGraphPlural(s.ModuleFiles, "file", "files")))
	}
	msg := "code graph stale"
	if len(parts) > 0 {
		msg += ": " + strings.Join(parts, ", ") + " since last index"
	}
	if command != "" {
		msg += "; run " + command
	}
	return msg
}

func codeGraphPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func codeGraphSourceExtensions() []string {
	seen := make(map[string]bool)
	var out []string
	for _, lang := range runner.SupportedLangs() {
		for _, ext := range langExtensions(lang) {
			if !seen[ext] {
				seen[ext] = true
				out = append(out, ext)
			}
		}
	}
	return out
}

func codeGraphAllManifestNames() []string {
	seen := make(map[string]bool)
	var out []string
	for _, lang := range runner.SupportedLangs() {
		for _, name := range manifestsForLang(lang) {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func codeGraphChangesSince(ctx context.Context, root, baseSHA string, extensions, manifests []string) (codeGraphChangeSummary, error) {
	changes, err := changed.DetailedFiles(ctx, root, baseSHA, nil)
	if err != nil {
		return codeGraphChangeSummary{}, err
	}
	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		extSet[ext] = true
	}
	manifestSet := make(map[string]bool, len(manifests))
	for _, name := range manifests {
		manifestSet[name] = true
	}
	var summary codeGraphChangeSummary
	for _, change := range changes {
		rel := filepath.ToSlash(change.Path)
		if r, err := filepath.Rel(root, change.Path); err == nil {
			rel = filepath.ToSlash(r)
		}
		if manifestSet[filepath.Base(rel)] {
			summary.ModuleFiles++
			continue
		}
		if len(extSet) > 0 && !extSet[filepath.Ext(rel)] {
			continue
		}
		switch change.Kind {
		case changed.FileAdded:
			summary.NewFiles++
		case changed.FileDeleted:
			summary.DeletedFiles++
		default:
			summary.ChangedFiles++
		}
	}
	return summary, nil
}

func detectCodeGraphLanguages(root string) ([]runner.Lang, bool, error) {
	seen := make(map[runner.Lang]bool)
	for _, lang := range runner.SupportedLangs() {
		dirs, err := discoverModuleDirs(root, lang)
		if err != nil {
			return nil, false, err
		}
		if len(dirs) > 0 {
			seen[lang] = true
		}
	}
	_, hasSource, err := detectCodeGraphSourceLanguages(root)
	if err != nil {
		return nil, false, err
	}
	langs := make([]runner.Lang, 0, len(seen))
	for lang := range seen {
		langs = append(langs, lang)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs, hasSource, nil
}

func detectCodeGraphSourceLanguages(root string) ([]runner.Lang, bool, error) {
	extLang := make(map[string]runner.Lang)
	for _, lang := range runner.SupportedLangs() {
		for _, ext := range langExtensions(lang) {
			extLang[ext] = lang
		}
	}
	skipDirs := make(map[string]bool, len(config.DefaultHookInstallSkipDirs)+6)
	for _, name := range config.DefaultHookInstallSkipDirs {
		skipDirs[name] = true
	}
	for _, name := range []string{".git", config.DirName, "node_modules", "vendor", "dist", "build"} {
		skipDirs[name] = true
	}
	seen := make(map[runner.Lang]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if lang, ok := extLang[filepath.Ext(d.Name())]; ok {
			seen[lang] = true
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	langs := make([]runner.Lang, 0, len(seen))
	for lang := range seen {
		langs = append(langs, lang)
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs, len(langs) > 0, nil
}

func codeGraphMissingDetail(langs []runner.Lang, hasSupportedSource bool) string {
	if len(langs) > 0 {
		return fmt.Sprintf("project gained code since init; detected language(s): %s - run %s", strings.Join(langNames(langs), ", "), codeGraphIndexSuggestion(langs))
	}
	if hasSupportedSource {
		return "project gained supported source files since init, but no indexable module was found - add a supported module manifest (go.mod, package.json, tsconfig.json, pyproject.toml, setup.py, Package.swift, or an Xcode project/workspace)"
	}
	return "index-state.json missing - run gg index --lang <lang>"
}

func codeGraphIndexSuggestion(langs []runner.Lang) string {
	names := langNames(langs)
	if len(names) == 0 {
		return ""
	}
	cmds := make([]string, 0, len(names))
	for _, name := range names {
		cmds = append(cmds, "gg index --lang "+name+" --changed")
	}
	return strings.Join(cmds, " && ")
}

func codeGraphFullIndexSuggestion(langs []runner.Lang) string {
	names := langNames(langs)
	if len(names) == 0 {
		return ""
	}
	cmds := make([]string, 0, len(names))
	for _, name := range names {
		cmds = append(cmds, "gg index --lang "+name)
	}
	return strings.Join(cmds, " && ")
}

func codeGraphFullIndexSuggestionForStatus(status codeGraphStatus) string {
	names := status.DetectedLanguages
	if len(names) == 0 {
		names = status.IndexedLanguages
	}
	return codeGraphFullIndexSuggestion(langsFromNames(names))
}

func codeGraphNeedsAction(status string) bool {
	switch status {
	case "stale", "partial", "missing", "non_ancestor":
		return true
	default:
		return false
	}
}

func codeGraphAgentWarning(s codeGraphStatus) string {
	return codeGraphNoticeOneLine(s)
}

func codeGraphActionWarning(ctx context.Context) string {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	root, err := config.FindRoot()
	if err != nil {
		return ""
	}
	status := collectCodeGraphStatus(ctx, root, filepath.Join(root, config.DirName), cfg)
	return codeGraphAgentWarning(status)
}

func emitCodeGraphNotice(ctx context.Context, w io.Writer, cfg *config.Config) {
	if cfg == nil {
		return
	}
	root, err := config.FindRoot()
	if err != nil {
		return
	}
	status := collectCodeGraphStatus(ctx, root, filepath.Join(root, config.DirName), cfg)
	lines := codeGraphNoticeLines(status)
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(w, "─── CODE GRAPH NOTICE ───")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	fmt.Fprintln(w)
}

func langsFromNames(names []string) []runner.Lang {
	langs := make([]runner.Lang, 0, len(names))
	for _, name := range names {
		langs = append(langs, runner.Lang(name))
	}
	sort.Slice(langs, func(i, j int) bool { return langs[i] < langs[j] })
	return langs
}

func langNames(langs []runner.Lang) []string {
	names := make([]string, 0, len(langs))
	for _, lang := range langs {
		names = append(names, string(lang))
	}
	sort.Strings(names)
	return names
}

func indexStatusShortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	if sha == "" {
		return "-"
	}
	return sha
}

func boolWord(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func renderCodeGraphStatusCompact(s codeGraphStatus) string {
	fresh := s.freshnessContract()
	var parts []string
	parts = append(parts, "CodeGraph "+fresh.Status)
	if fresh.Reason != "" {
		parts = append(parts, "reason="+fresh.Reason)
	}
	if s.LastIndexedSHA != "" || s.HeadSHA != "" {
		parts = append(parts, fmt.Sprintf("idx=%s head=%s", indexStatusShortSHA(s.LastIndexedSHA), indexStatusShortSHA(s.HeadSHA)))
	}
	if len(s.IndexedLanguages) > 0 {
		parts = append(parts, "langs="+strings.Join(s.IndexedLanguages, ","))
	}
	if len(s.DetectedLanguages) > 0 {
		parts = append(parts, "detected="+strings.Join(s.DetectedLanguages, ","))
	}
	parts = append(parts, fmt.Sprintf("memgraph=%s", boolWord(s.MemgraphAvailable, "ok", "down")))
	parts = append(parts, fmt.Sprintf("files=%d sym=%d edges=%d", s.Stats.Files, s.Stats.Symbols, s.Stats.Edges))
	if s.ChangedFiles+s.NewFiles+s.DeletedFiles+s.ModuleFiles > 0 {
		parts = append(parts, fmt.Sprintf("changes=%d new=%d deleted=%d modules=%d", s.ChangedFiles, s.NewFiles, s.DeletedFiles, s.ModuleFiles))
	}
	if s.PendingOutbox > 0 {
		parts = append(parts, fmt.Sprintf("outbox=%d", s.PendingOutbox))
	}
	if s.Detail != "" {
		parts = append(parts, compactTrim(s.Detail, 90))
	}
	if fresh.SuggestedCommand != "" && fresh.NeedsNotice() {
		parts = append(parts, "run="+compactTrim(fresh.SuggestedCommand, 90))
	}
	if fresh.ForegroundWatchAvailable && fresh.ForegroundWatchCommand != "" && fresh.NeedsNotice() {
		parts = append(parts, "watch="+compactTrim(fresh.ForegroundWatchCommand, 50))
	}
	if !s.NoWatcherStarted {
		parts = append(parts, "watcher="+compactTrim(s.Watcher, 50))
	}
	return strings.Join(parts, "  ")
}

func codeGraphStatusWithTimeout(root, ggDir string, cfg *config.Config) codeGraphStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return collectCodeGraphStatus(ctx, root, ggDir, cfg)
}
