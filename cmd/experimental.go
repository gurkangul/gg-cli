package cmd

// experimentalMarker is the single source of truth for the runtime tier marker
// shown in --help for commands in the experimental stability tier
// (see docs/stability.md §2). Keep this wording identical across all commands.
const experimentalMarker = "[experimental] "

// experimentalNote is the one-line stability caveat appended to a command's
// Long help so the tier is self-evident at runtime.
const experimentalNote = "Experimental: may change or be removed in a MINOR without a deprecation cycle (see docs/stability.md §2)."

// experimentalShort prefixes a command's Short description with the experimental
// marker so the tier is visible in both the root `gg --help` listing and the
// command's own `gg <cmd> --help`.
func experimentalShort(s string) string {
	return experimentalMarker + s
}

// experimentalLong prepends the experimental note to a command's Long help.
// If long is empty, just the note is returned.
func experimentalLong(long string) string {
	if long == "" {
		return experimentalNote
	}
	return experimentalNote + "\n\n" + long
}
