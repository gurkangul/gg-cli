.PHONY: smoke-fresh

# smoke-fresh: run the fresh-machine smoke test inside a clean Ubuntu container.
# Prerequisites: Docker running, host-side Qdrant/Memgraph/Ollama up on standard ports.
# Override gateway: HOST_GATEWAY=172.17.0.1 make smoke-fresh
smoke-fresh:
	@bash scripts/smoke/fresh-install.sh
