#!/bin/sh
# Repro for BUG-017: `gg doctor --install-task-hooks` walked into framework
# build artifact dirs (e.g. web/.output/server for Nuxt, .next/ for Next.js)
# and treated their synthetic package.json as a real workspace. The installed
# hook then broke every `gg task done` with an `npm ci -w` workspace error.
# Fix: DefaultHookInstallSkipDirs extended with .output, .next, .vercel,
# .svelte-kit, .astro, .turbo, .nuxt, .cache, out.
set -e
cd "$(git rev-parse --show-toplevel)"
go test -short -count=1 -run TestInstallTaskHooks_SkipsFrameworkBuildArtifacts_BUG017 ./cmd/
