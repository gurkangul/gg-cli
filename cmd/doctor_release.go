package cmd

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// doctor_release.go — advisory: commits that landed but were never released.
//
// Committing and pushing a fix is not the same as shipping it. gg is delivered
// as a tagged release: `gg update` installs the newest RELEASE binary and then
// syncs every registered project from it. So a fix sitting on main behind the
// latest tag reaches nobody — not other projects, not other machines, not even
// the maintainer's own shell — while looking completely done in git.
//
// That gap is easy to walk into: close the bug, push, move on, and the tag never
// happens. It is exactly the kind of thing a person should not have to hold in
// their head, so doctor holds it instead.
//
// Maintainer-facing by construction: it only speaks inside a local gg-cli source
// checkout (findGGCLISourceDir), and is silent everywhere else.

// doctorCheckReleaseAdvisory reports commits on HEAD that are newer than the
// most recent tag. Advisory only — unreleased work is a normal mid-development
// state, so this warns and never fails.
func doctorCheckReleaseAdvisory(report *doctorReport) {
	srcDir, err := findGGCLISourceDir()
	if err != nil {
		return // not a gg-cli checkout — nothing meaningful to say
	}

	tag, err := latestReachableTag(srcDir)
	if err != nil || tag == "" {
		report.ok("gg release", "no tags yet — nothing to compare")
		return
	}

	n, err := commitsSince(srcDir, tag)
	if err != nil {
		report.warn("gg release", "could not count commits since "+tag)
		return
	}
	if n == 0 {
		report.ok("gg release", "HEAD is released ("+tag+")")
		return
	}

	noun, verb, pronoun := "commits", "are", "they"
	if n == 1 {
		noun, verb, pronoun = "commit", "is", "it"
	}
	report.warn("gg release", fmt.Sprintf(
		"%d %s after %s %s unreleased — %s cannot reach `gg update` or `gg system sync` until a tag is pushed",
		n, noun, tag, verb, pronoun))
}

// latestReachableTag returns the most recent tag reachable from HEAD.
// An empty string (with no error) means the repo has no tags.
func latestReachableTag(srcDir string) (string, error) {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		// git exits non-zero when no tag is reachable; that is not a failure.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// commitsSince counts commits in ref..HEAD.
func commitsSince(srcDir, ref string) (int, error) {
	cmd := exec.Command("git", "rev-list", "--count", ref+"..HEAD")
	cmd.Dir = srcDir
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}
