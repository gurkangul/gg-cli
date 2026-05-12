#!/usr/bin/env bash
set -euo pipefail

# BUG-037 regression guard: deployed hook copies must carry gg template markers
# and gg doctor must expose an explicit refresh path for drifted copies.
# Pre-fix: --refresh-hooks is unknown and installed hooks lack marker headers.
# Post-fix: refresh command exists and marker helpers are wired into installer.

grep -q 'refresh-hooks' cmd/doctor.go
grep -q 'WithHookTemplateMarker' cmd/doctor_install.go
grep -q 'gg-template-sha256' internal/templates/marker.go
grep -q 'Hook templates:' cmd/doctor.go
grep -q 'would refresh %d drifted hook(s)' cmd/system_sync.go
