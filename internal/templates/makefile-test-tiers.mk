# gg test-tier targets — opt-in test pyramid contract.
#
# Adopt by adding this line near the top of your Makefile:
#     include .gg/templates/makefile-test-tiers.mk
#
# Then override each target with the commands that suit your stack. The
# LEFT-hand-side NAMES are the stable public contract — gg hooks + cross-agent
# conventions map to these, so keep the names even when the recipes change.
#
# Why this exists (dogfood audit 2026-04-19): when unit tests are the only
# cheap layer and e2e is the only truthful one, semantic bugs accumulate
# between them. Named middle tiers give the team a place to put fakes +
# integration checks, and `make test-smoke` gives pre-commit a fast truth
# signal that unit tests alone do not provide.

.PHONY: test-unit test-integration test-smoke test-e2e

# Fast, in-memory, no external services. Goal: under 10s total.
# Typical: `go test -short ./...`  or  `npm run test:unit`
test-unit:
	@echo ">> test-unit placeholder — override in your Makefile"
	@false

# Table-driven tests exercising real package logic against in-process fakes
# (SSH / HTTP / DB). Goal: under 60s. Bridges unit and e2e so semantic bugs
# do not need a live environment to surface.
# Typical: `go test -tags=integration ./...`
test-integration:
	@echo ">> test-integration placeholder — override in your Makefile"
	@false

# Happy-path e2e subset covering critical user journeys. Goal: under 3min
# so it can run per-commit without slowing developers. This is the gate
# that gg pre-task-done hooks invoke by default.
test-smoke:
	@echo ">> test-smoke placeholder — override in your Makefile"
	@false

# Full end-to-end suite — run per-PR and per-release. Honest coverage
# matters more than speed here.
test-e2e:
	@echo ">> test-e2e placeholder — override in your Makefile"
	@false
