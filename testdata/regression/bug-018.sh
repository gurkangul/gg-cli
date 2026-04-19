#!/bin/sh
# Repro for BUG-018: Telemetry was opt-in (default OFF). Users who ran
# `gg init` without also writing `telemetry.enabled: true` (or exporting
# GG_TELEMETRY=1) got silently empty telemetry — making the North Star
# dogfood metric untrustworthy. Fix: flip default to ON; config.Enabled
# became *bool so "absent" ≠ "false"; env var still always wins.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run 'TestRecord_EnabledByDefault_BUG018|TestRecord_DisabledViaConfig|TestRecord_DisabledViaEnv' ./internal/telemetry/
