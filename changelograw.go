// Package gg embeds CHANGELOG.md so the changelog parser can access it
// without requiring the file on disk at runtime. This file must live at the
// module root (same directory as CHANGELOG.md) so the embed path has no
// ".." components — Go's embed directive does not follow symlinks and does
// not allow ".." in patterns.
package gg

import _ "embed"

//go:embed CHANGELOG.md
var ChangelogRaw string
