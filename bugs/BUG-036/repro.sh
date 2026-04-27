#!/bin/sh
# BUG-036: closeWorkerPaneForTask could close the master pane when panes registry
#           pointed at the master surface (no protection check before cmux close).
# Pre-fix: closeWorkerPaneForTask called term.Close unconditionally on the registered surface.
# Post-fix (05e4f40): isProtectedMasterSurface gate refuses to close when the surface
#           equals GG_SURFACE_ID of the caller or the heartbeat surface of the master;
#           registry entry is still removed so the heartbeat prune does not warn forever.
#           TestCloseWorkerPaneForTask_RefusesCurrentMasterSurface guards the behaviour.
set -eu

repo_root=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
cd "$repo_root"

grep -q "func isProtectedMasterSurface" cmd/task_pane_lifecycle.go
grep -q "TestCloseWorkerPaneForTask_RefusesCurrentMasterSurface" cmd/task_pane_lifecycle_test.go
