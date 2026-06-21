#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-052: degraded zero-vector replay and semantic canary must be visible to doctor.
grep -q 'gg_vector_degraded' internal/store/offline.go
grep -q 'DegradedVectorCounts' internal/store/offline.go
grep -q 'DegradedVectorCounts' cmd/semantic_health.go
grep -q 'semantic canary' cmd/doctor_embedding.go
grep -q 'ollama semantic canary' cmd/doctor_embedding.go
