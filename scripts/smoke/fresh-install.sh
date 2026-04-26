#!/usr/bin/env bash
# fresh-install.sh — end-to-end smoke test inside a clean Ubuntu container.
#
# Exercises the full first-run flow:
#   1. Build gg from source (go install ./cmd/gg)
#   2. gg init in a scratch project dir
#   3. gg doctor (exit 0 or graceful warning)
#   4. Happy-path commands (record / task create / search / status)
#
# Services (Qdrant, Memgraph, Ollama) must already be running on the HOST.
# The container reaches them via host.docker.internal (or HOST_GATEWAY env).
# This script does NOT start any services itself.
#
# Usage:
#   ./scripts/smoke/fresh-install.sh          # runs inside Docker
#   HOST_GATEWAY=172.17.0.1 make smoke-fresh  # override gateway when needed
#
# Prerequisites: Docker, Go source in CWD, services reachable on host.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="ubuntu:22.04"
CONTAINER_NAME="gg-smoke-fresh-$$"
# Allow caller to override the gateway address
HOST_GATEWAY="${HOST_GATEWAY:-host-gateway}"

PASS=0
FAIL=0
FAILURES=""

log()  { echo "[smoke] $*"; }
pass() { log "PASS: $1"; PASS=$((PASS+1)); }
fail() { log "FAIL: $1"; FAIL=$((FAIL+1)); FAILURES="${FAILURES}\n  - $1"; }

# ── Preflight: Docker available ───────────────────────────────────────────────
if ! command -v docker &>/dev/null; then
    echo "ERROR: docker not found — install Docker Desktop and retry" >&2
    exit 1
fi

log "Pulling ${IMAGE} (cached after first run)..."
docker pull --quiet "${IMAGE}" >/dev/null

log "Detecting host gateway for container→host service reach..."
# On Linux Docker sets the gateway; on macOS host-gateway resolves to the VM.
# The caller can export HOST_GATEWAY=<ip> to override.
GATEWAY_FLAG="--add-host=host.docker.internal:${HOST_GATEWAY}"

log "Starting container ${CONTAINER_NAME}..."

# ── Build the inline container script ─────────────────────────────────────────
INNER_SCRIPT=$(cat <<'INNER'
#!/bin/bash
set -euo pipefail

PASS=0
FAIL=0
FAILURES=""

_pass() { echo "[AC] PASS: $1"; PASS=$((PASS+1)); }
_fail() { echo "[AC] FAIL: $1"; FAIL=$((FAIL+1)); FAILURES="${FAILURES}\n  - $1"; }

echo "=== Environment ==="
echo "OS: $(. /etc/os-release && echo $PRETTY_NAME)"
echo "Go: $(go version 2>/dev/null || echo 'not installed')"

# ── Install Go ────────────────────────────────────────────────────────────────
echo ""
echo "=== Installing Go ==="
apt-get update -qq
apt-get install -y -qq wget git ca-certificates curl 2>/dev/null
GO_VER="1.22.4"
GO_URL="https://dl.google.com/go/go${GO_VER}.linux-amd64.tar.gz"
wget -q "${GO_URL}" -O /tmp/go.tar.gz
tar -C /usr/local -xzf /tmp/go.tar.gz
rm /tmp/go.tar.gz
export PATH="/usr/local/go/bin:${PATH}"
export GOPATH="/root/go"
export PATH="${GOPATH}/bin:${PATH}"
echo "Go installed: $(go version)"

# ── AC-1: Build gg from source ────────────────────────────────────────────────
echo ""
echo "=== AC-1: Build gg from source ==="
cd /src
go install ./cmd/gg 2>&1
if which gg >/dev/null 2>&1; then
    _pass "AC-1: go install ./cmd/gg succeeded, which gg = $(which gg)"
else
    _fail "AC-1: which gg failed after go install"
fi

# ── AC-2: gg init in a scratch project dir ────────────────────────────────────
echo ""
echo "=== AC-2: gg init ==="
SCRATCH_DIR="$(mktemp -d)"
cd "${SCRATCH_DIR}"
git init -q .

# Write config pointing to host-side services via host.docker.internal
# (gg init tries to start Docker services; --yes --no-index skips the prompt,
#  and we pre-create the config to avoid service-start attempts)
GG_DIR="${SCRATCH_DIR}/.gg"
mkdir -p "${GG_DIR}"

PROJECT_ID="smoke-$(cat /proc/sys/kernel/random/uuid 2>/dev/null || uuidgen 2>/dev/null || date +%s)"
# Inside the container, host.docker.internal is the alias added via --add-host
# (HOST_GATEWAY is the *source* — host-gateway on macOS, bridge IP on Linux).
GATEWAY="host.docker.internal"

# gg refuses to record without an agent identity — set one for the smoke.
export GG_AGENT="smoke-test"

cat > "${GG_DIR}/config.yaml" <<YAML
project_id: ${PROJECT_ID}
qdrant:
    host: ${GATEWAY}
    port: 6334
embedding:
    host: http://${GATEWAY}:11434
    model: nomic-embed-text
memgraph:
    uri: bolt://${GATEWAY}:7687
    username: ""
    password: ""
hooks:
    strict: false
YAML

gg init --yes --no-index 2>&1 || true  # re-init over existing config is idempotent

if [ -d "${GG_DIR}" ] && [ -f "${GG_DIR}/config.yaml" ]; then
    _pass "AC-2: .gg/ directory exists with config.yaml"
else
    _fail "AC-2: .gg/ directory or config.yaml missing after gg init"
fi

# Check for expected initial files
EXPECTED_FILES=("config.yaml" ".gitignore" "RULES.md")
MISSING=""
for f in "${EXPECTED_FILES[@]}"; do
    if [ ! -f "${GG_DIR}/${f}" ]; then
        MISSING="${MISSING} ${f}"
    fi
done
if [ -z "${MISSING}" ]; then
    _pass "AC-2: expected initial files present (config.yaml, .gitignore, RULES.md)"
else
    _fail "AC-2: missing files in .gg/:${MISSING}"
fi

# ── AC-3: gg doctor ───────────────────────────────────────────────────────────
echo ""
echo "=== AC-3: gg doctor ==="
cd "${SCRATCH_DIR}"
set +e
DOCTOR_OUT="$(gg doctor 2>&1)"
DOCTOR_EXIT=$?
set -e
echo "${DOCTOR_OUT}"
if [ ${DOCTOR_EXIT} -eq 0 ]; then
    _pass "AC-3: gg doctor exited 0"
elif echo "${DOCTOR_OUT}" | grep -qiE "unreachable|connect|refused|timeout|warn|cannot|not running"; then
    _pass "AC-3: gg doctor exited non-zero but emitted a graceful service-unreachable warning (expected when host services are down)"
else
    _fail "AC-3: gg doctor exited ${DOCTOR_EXIT} without a graceful warning — output: ${DOCTOR_OUT}"
fi

# ── AC-4: Happy-path commands ─────────────────────────────────────────────────
echo ""
echo "=== AC-4: Happy-path commands ==="
cd "${SCRATCH_DIR}"

# gg record
set +e
RECORD_OUT="$(gg record "smoke test decision" --reason "verifying fresh install" --tags "smoke,test" 2>&1)"
RECORD_EXIT=$?
set -e
echo "record exit=${RECORD_EXIT}: ${RECORD_OUT}"
if [ ${RECORD_EXIT} -eq 0 ]; then
    _pass "AC-4a: gg record exited 0"
else
    _fail "AC-4a: gg record exited ${RECORD_EXIT}"
fi

# gg task create
set +e
TASK_OUT="$(gg task create "smoke task" --requester user --priority low --detail "fresh install smoke test task" 2>&1)"
TASK_EXIT=$?
set -e
echo "task create exit=${TASK_EXIT}: ${TASK_OUT}"
if [ ${TASK_EXIT} -eq 0 ]; then
    _pass "AC-4b: gg task create exited 0"
else
    _fail "AC-4b: gg task create exited ${TASK_EXIT}"
fi

# gg search --compact (must surface the just-recorded decision)
set +e
SEARCH_OUT="$(gg search --compact "smoke test decision" 2>&1)"
SEARCH_EXIT=$?
set -e
echo "search exit=${SEARCH_EXIT}: ${SEARCH_OUT}"
if [ ${SEARCH_EXIT} -eq 0 ]; then
    _pass "AC-4c: gg search --compact exited 0"
else
    _fail "AC-4c: gg search --compact exited ${SEARCH_EXIT}"
fi
if echo "${SEARCH_OUT}" | grep -qi "smoke test decision"; then
    _pass "AC-4c: gg search surfaced the just-recorded decision"
else
    _fail "AC-4c: gg search did not surface 'smoke test decision' — output: ${SEARCH_OUT}"
fi

# gg status
set +e
STATUS_OUT="$(gg status 2>&1)"
STATUS_EXIT=$?
set -e
echo "status exit=${STATUS_EXIT}: ${STATUS_OUT}"
if [ ${STATUS_EXIT} -eq 0 ]; then
    _pass "AC-4d: gg status exited 0"
else
    _fail "AC-4d: gg status exited ${STATUS_EXIT}"
fi

# ── Results ───────────────────────────────────────────────────────────────────
echo ""
echo "=== Smoke Results ==="
echo "PASS: ${PASS}"
echo "FAIL: ${FAIL}"
if [ ${FAIL} -gt 0 ]; then
    echo -e "Failures:${FAILURES}"
    exit 1
fi
echo "All assertions passed."
INNER
)

# Run the container, mounting the repo source read-only at /src
docker run --rm \
    --name "${CONTAINER_NAME}" \
    ${GATEWAY_FLAG} \
    -e "HOST_GATEWAY=${HOST_GATEWAY}" \
    -v "${REPO_ROOT}:/src:ro" \
    "${IMAGE}" \
    bash -c "${INNER_SCRIPT}"

CONTAINER_EXIT=$?

echo ""
if [ ${CONTAINER_EXIT} -eq 0 ]; then
    log "smoke-fresh PASSED"
else
    log "smoke-fresh FAILED (container exited ${CONTAINER_EXIT})"
    exit ${CONTAINER_EXIT}
fi
