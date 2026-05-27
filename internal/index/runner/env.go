package runner

import (
	"os"
	"strings"
)

// sensitiveEnvPrefixes lists the env-var prefixes that must never be passed to
// child indexer processes. Indexers are third-party SCIP binaries that should
// not have access to database credentials or internal secrets held in the
// parent process.
var sensitiveEnvPrefixes = []string{
	"MEMGRAPH_",  // Memgraph DB credentials (MEMGRAPH_PASSWORD, etc.)
	"GG_SECRET_", // gg internal secrets
	"GG_API_KEY", // gg API keys
}

// filteredEnv returns a copy of os.Environ() with sensitive variables removed.
// Use this as exec.Cmd.Env for all indexer subprocess launches to prevent
// credential exfiltration through compromised or malicious indexer binaries.
func filteredEnv() []string {
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		if isSensitive(kv) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// isSensitive reports whether an env var of the form "KEY=VALUE" matches any
// of the sensitive prefixes.
func isSensitive(kv string) bool {
	for _, prefix := range sensitiveEnvPrefixes {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}
