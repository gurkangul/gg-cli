.PHONY: install docs test smoke-fresh

# install: rebuild the local gg binary from this checkout and replace the active PATH gg.
# This is the developer path for dogfood patches that are not released yet.
install:
	@go run ./cmd/gg update --from-source --skip-sync

docs:
	@go run ./tools/docs-gen

test:
	@go test ./... -count=1 -race -timeout=120s

# smoke-fresh: run the fresh-machine smoke test inside a clean Ubuntu container.
# This exercises the OPT-IN Qdrant/Memgraph server backends specifically (the
# default no-Docker embedded path is covered by the unit/integration suite).
# Prerequisites: Docker running, host-side Qdrant/Memgraph/Ollama up on standard ports.
# Override gateway: HOST_GATEWAY=172.17.0.1 make smoke-fresh
smoke-fresh:
	@bash scripts/smoke/fresh-install.sh
