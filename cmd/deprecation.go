package cmd

import "fmt"

// deprecationMessage builds the single, canonical deprecation phrasing used
// across every deprecated gg command. It is the one source of truth for the
// wording so that the warning shown in --help, command listings, shell
// completion, and the runtime stderr notice are always identical.
//
// The returned string is meant to be assigned to a cobra command's
// `Deprecated:` field. Cobra prefixes it at run time with
// `Command "<name>" is deprecated, ` and prints the whole line to stderr
// (never stdout — pipelines and --json consumers stay clean) exactly once,
// before RunE executes. Because cobra owns the emission, deprecated commands
// must NOT additionally hand-roll their own fmt.Fprintln warning, otherwise
// the user would see the notice twice.
//
// replacement is the canonical command the caller should use instead, e.g.
// "gg record" or "gg record --decision-status=rejected".
func deprecationMessage(replacement string) string {
	return fmt.Sprintf("use '%s' instead. It will be removed in a future major release.", replacement)
}
