#!/usr/bin/env bash
# validate-manifests.sh
#
# Validates all Kubernetes manifests in k8s/ using two complementary methods:
#
#   1. kubeconform  - schema validation against the Kubernetes OpenAPI spec.
#                    Catches field typos, wrong types, and missing required fields
#                    WITHOUT needing a running cluster.
#
#   2. kubectl diff --dry-run=server - server-side dry run against a live cluster.
#                    Catches admission webhook rejections and real API mismatches.
#                    Only runs when KUBECONFIG points at a live cluster.
#
# Usage:
#   bash scripts/validate-manifests.sh              # kubeconform only
#   LIVE=true bash scripts/validate-manifests.sh    # kubeconform + server dry-run
#
# Prerequisites:
#   - kubectl >= 1.30 on $PATH
#   - kubeconform is auto-installed if missing (into a local ./bin directory)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# kubeconform release to pin. Check https://github.com/yannh/kubeconform/releases
KUBECONFORM_VERSION="0.6.7"
# Install location for the kubeconform binary if not already on PATH.
LOCAL_BIN="${REPO_ROOT}/bin"
KUBECONFORM_BIN="${LOCAL_BIN}/kubeconform"

OVERLAY="${REPO_ROOT}/k8s/overlays/local"

log()  { echo "[validate] $*"; }
pass() { echo "[PASS] $*"; }
fail() { echo "[FAIL] $*" >&2; }

ERRORS=0

# ---------------------------------------------------------------------------
# Helper: increment error counter without exiting immediately.
# This lets the script report ALL failures, not just the first one.
# ---------------------------------------------------------------------------
record_failure() {
  ERRORS=$(( ERRORS + 1 ))
}

# ---------------------------------------------------------------------------
# 1. Ensure kubeconform is available
# ---------------------------------------------------------------------------
if command -v kubeconform >/dev/null 2>&1; then
  KUBECONFORM="kubeconform"
  log "Using kubeconform from PATH: $(command -v kubeconform)"
elif [[ -x "${KUBECONFORM_BIN}" ]]; then
  KUBECONFORM="${KUBECONFORM_BIN}"
  log "Using kubeconform from ${KUBECONFORM_BIN}"
else
  log "kubeconform not found. Installing v${KUBECONFORM_VERSION} into ${LOCAL_BIN}..."
  mkdir -p "${LOCAL_BIN}"

  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  # Normalise architecture names to match kubeconform release asset names.
  case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
      log "Unsupported architecture: ${ARCH}. Install kubeconform manually."
      log "https://github.com/yannh/kubeconform/releases"
      exit 1
      ;;
  esac

  DOWNLOAD_URL="https://github.com/yannh/kubeconform/releases/download/v${KUBECONFORM_VERSION}/kubeconform-${OS}-${ARCH}.tar.gz"
  log "Downloading from ${DOWNLOAD_URL}"
  curl -sSL "${DOWNLOAD_URL}" | tar -xz -C "${LOCAL_BIN}" kubeconform
  chmod +x "${KUBECONFORM_BIN}"
  KUBECONFORM="${KUBECONFORM_BIN}"
  log "kubeconform installed at ${KUBECONFORM_BIN}"
fi

# ---------------------------------------------------------------------------
# 2. Render the Kustomize overlay to a single YAML stream
# ---------------------------------------------------------------------------
log "Rendering Kustomize overlay: ${OVERLAY}"
RENDERED="$(kubectl kustomize "${OVERLAY}")"

if [[ -z "${RENDERED}" ]]; then
  fail "kubectl kustomize produced no output. Check your kustomization.yaml files."
  exit 1
fi

pass "kubectl kustomize rendered successfully."

# ---------------------------------------------------------------------------
# 3. kubeconform schema validation
# ---------------------------------------------------------------------------
log "Running kubeconform schema validation..."

# Flags:
#   -strict              fail on unknown fields (catches typos)
#   -summary             print a one-line result summary
#   -output pretty       human-readable output
#   -kubernetes-version  pin to a known version to avoid false positives
if echo "${RENDERED}" | "${KUBECONFORM}" \
    -strict \
    -summary \
    -output pretty \
    -kubernetes-version "1.30.0" \
    -schema-location default \
    -schema-location "https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json"; then
  pass "kubeconform: all resources are valid."
else
  fail "kubeconform: one or more resources failed validation."
  record_failure
fi

# ---------------------------------------------------------------------------
# 4. Optional: server-side dry run against a live cluster
# Only runs when LIVE=true is set in the environment.
# Requires KUBECONFIG to point at a running cluster.
# ---------------------------------------------------------------------------
if [[ "${LIVE:-false}" == "true" ]]; then
  log "Running server-side dry run (LIVE=true)..."

  if ! kubectl cluster-info >/dev/null 2>&1; then
    fail "kubectl cannot reach the cluster. Check your KUBECONFIG."
    record_failure
  else
    if echo "${RENDERED}" | kubectl apply --dry-run=server -f -; then
      pass "kubectl dry-run=server: all resources accepted by the API server."
    else
      fail "kubectl dry-run=server: one or more resources were rejected."
      record_failure
    fi
  fi
else
  log "Skipping server-side dry run. Set LIVE=true to enable it."
fi

# ---------------------------------------------------------------------------
# 5. Final result
# ---------------------------------------------------------------------------
if [[ "${ERRORS}" -gt 0 ]]; then
  fail "Validation finished with ${ERRORS} error(s)."
  exit 1
else
  pass "All validation checks passed."
  exit 0
fi
