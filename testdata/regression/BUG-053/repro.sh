#!/bin/sh
set -eu
cd "$(git rev-parse --show-toplevel)"
# BUG-053: doctor/impact must surface stale or incomplete code graph guidance.
grep -q 'doctorCheckCodeGraphFreshness' cmd/doctor_checks.go
grep -q 'Code graph freshness' cmd/doctor.go
grep -q 'collectCodeGraphStatus' cmd/index_status.go
grep -q 'impactGraphFreshnessWarnings' cmd/impact.go
grep -q 'WorkingTreeFingerprint' internal/index/changed/changed.go
