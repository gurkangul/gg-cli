// Package dashboard embeds the compiled React dashboard (dist/) so `gg serve`
// can ship it inside the single Go binary — no Node runtime, fully offline. The
// dist/ output is committed so `go build` always works without a frontend
// toolchain; CI rebuilds it fresh before release.
package dashboard

import "embed"

//go:embed all:dist
var FS embed.FS
