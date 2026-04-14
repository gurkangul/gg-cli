package compat

import (
	"runtime/debug"
)

// scipLibVersionFromBuildInfo reads the version of the scip-code/scip module
// from the binary's embedded build info. Returns "unknown" on failure.
func scipLibVersionFromBuildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	const target = "github.com/scip-code/scip/bindings/go/scip"
	for _, dep := range info.Deps {
		if dep.Path == target {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}
