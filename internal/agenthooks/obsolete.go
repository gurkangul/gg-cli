package agenthooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type obsoleteBlock struct {
	path  string
	begin string
	end   string
	label string
}

var obsoleteBlocks = []obsoleteBlock{
	{
		path:  "CLAUDE.md",
		begin: "<!-- gg:master-role:begin v3 -->",
		end:   "<!-- gg:master-role:end -->",
		label: "master-role",
	},
	{
		path:  "CLAUDE.md",
		begin: "<!-- gg:dev-routing:begin v1 -->",
		end:   "<!-- gg:dev-routing:end -->",
		label: "dev-routing",
	},
}

// RemoveObsoleteBlocks strips managed guidance for retired block families. It is
// deliberately narrow: only exact marker families owned by gg are removed, so
// project-local prose is left for explicit human edits.
func RemoveObsoleteBlocks(projectRoot string) ([]string, []error) {
	var lines []string
	var errs []error

	for _, block := range obsoleteBlocks {
		path := filepath.Join(projectRoot, block.path)
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				errs = append(errs, fmt.Errorf("%s: %w", path, err))
			}
			continue
		}

		updated, removed, removeErr := removeMarkedBlock(string(raw), block.begin, block.end)
		if removeErr != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, removeErr))
			continue
		}
		if !removed {
			continue
		}
		if err := writeFile(path, updated); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("  ✓ removed obsolete %s block from %s", block.label, block.path))
	}

	return lines, errs
}

func removeMarkedBlock(content, begin, end string) (string, bool, error) {
	changed := false
	for {
		start := strings.Index(content, begin)
		if start < 0 {
			break
		}
		endStart := strings.Index(content[start+len(begin):], end)
		if endStart < 0 {
			return content, changed, fmt.Errorf("marker %q present without %q", begin, end)
		}
		endPos := start + len(begin) + endStart + len(end)
		if endPos < len(content) && content[endPos] == '\n' {
			endPos++
		}
		content = content[:start] + content[endPos:]
		changed = true
	}
	return strings.TrimRight(content, "\n") + "\n", changed, nil
}
