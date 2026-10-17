#!/usr/bin/env bash
# setup.sh
# Creates the kind cluster, loads local Docker images into it,
# installs the nginx ingress controller, installs metrics-server,
# and applies the Kustomize local overlay.
#
# Prerequisites:
#   - Docker is running
#   - kind   >= 0.23  is on $PATH
#   - kubectl >= 1.30 is on $PATH
#
# Usage:
#   bash kind/setup.sh
#
# To tear everything down:
#   kind delete cluster --name sentinel

set -euo pipefail

CLUSTER_NAME="sentinel"
NAMESPACE="sentinel"

# Pinned versions of external components.
INGRESS_NGINX_VERSION="1.10.1"
METRICS_SERVER_VERSION="0.7.1"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

log() { echo "[setup] $*"; }
die() { echo "[error] $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 1. Verify required tools
# ---------------------------------------------------------------------------
for tool in docker kind kubectl; do
  command -v "${tool}" >/dev/null 2>&1 || die "'${tool}' not found on PATH. Please install it first."
done

log "All required tools found."

# ---------------------------------------------------------------------------
# 2. Create the kind cluster (skip if it already exists)
# ---------------------------------------------------------------------------
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  log "Cluster '${CLUSTER_NAME}' already exists. Skipping creation."
else
  log "Creating kind cluster '${CLUSTER_NAME}'..."
  kind create cluster \
    --name "${CLUSTER_NAME}" \
    --config "${SCRIPT_DIR}/kind-config.yaml" \
    --wait 120s
  log "Cluster created."
fi

# Ensure kubectl is pointing at this cluster.
kubectl config use-context "kind-${CLUSTER_NAME}"

# ---------------------------------------------------------------------------
# 3. Load locally built Docker images into the kind cluster.
#    kind nodes do not share the host Docker daemon image cache, so images
#    must be explicitly loaded. This is faster than pushing to a registry.
# ---------------------------------------------------------------------------
log "Loading Docker images into kind cluster..."

# These image names must match what docker compose built in Step 1.
# docker compose derives the image name from the service name and directory.
BACKEND_IMAGE="devops-project-1-backend:latest"
FRONTEND_IMAGE="devops-project-1-frontend:latest"

for IMAGE in "${BACKEND_IMAGE}" "${FRONTEND_IMAGE}"; do
  if docker image inspect "${IMAGE}" >/dev/null 2>&1; then
    log "Loading image: ${IMAGE}"
    kind load docker-image "${IMAGE}" --name "${CLUSTER_NAME}"
  else
    log "WARNING: Image '${IMAGE}' not found in local Docker daemon."
    log "         Run 'docker compose up --build' from the repo root first."
    log "         Continuing — you can load the image later with:"
    log "         kind load docker-image ${IMAGE} --name ${CLUSTER_NAME}"
  fi
done

# ---------------------------------------------------------------------------
# 4. Install the nginx ingress controller
#    Source: https://kind.sigs.k8s.io/docs/user/ingress/#ingress-nginx
#    Pinned to a specific version. "latest" is never used.
# ---------------------------------------------------------------------------
log "Installing nginx ingress controller v${INGRESS_NGINX_VERSION}..."

kubectl apply -f \
  "https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v${INGRESS_NGINX_VERSION}/deploy/static/provider/kind/deploy.yaml"

log "Waiting for ingress controller to become ready (up to 90s)..."
kubectl wait \
  --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=90s

log "Ingress controller is ready."

# ---------------------------------------------------------------------------
# 5. Install metrics-server (required for HPA to read CPU metrics)
#    kind does not ship metrics-server. We patch the deployment to add
#    --kubelet-insecure-tls because kind nodes use self-signed certs.
# ---------------------------------------------------------------------------
log "Installing metrics-server v${METRICS_SERVER_VERSION}..."

kubectl apply -f \
  "https://github.com/kubernetes-sigs/metrics-server/releases/download/v${METRICS_SERVER_VERSION}/components.yaml"

# Patch metrics-server to disable TLS verification (required in kind).
kubectl patch deployment metrics-server \
  --namespace kube-system \
  --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'

log "Waiting for metrics-server to become ready (up to 90s)..."
kubectl wait \
  --namespace kube-system \
  --for=condition=ready pod \
  --selector=k8s-app=metrics-server \
  --timeout=90s

log "Metrics-server is ready."

# ---------------------------------------------------------------------------
# 6. Apply the Kustomize local overlay
#    This now includes the application stack AND the observability stack.
# ---------------------------------------------------------------------------
log "Applying Kubernetes manifests (local overlay)..."

kubectl apply -k "${REPO_ROOT}/k8s/overlays/local"

# The observability pods (Loki, Tempo, Grafana) take longer to become ready
# than the application pods. Wait in two passes: first for app pods that have
# a fast startup, then for observability pods.
log "Waiting for application pods to be ready (up to 120s)..."
kubectl wait \
  --namespace "${NAMESPACE}" \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/name=backend \
  --timeout=120s || true

kubectl wait \
  --namespace "${NAMESPACE}" \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/name=frontend \
  --timeout=120s || true

log "Waiting for observability pods to be ready (up to 180s)..."
for APP in otelcol prometheus loki tempo grafana; do
  kubectl wait \
    --namespace "${NAMESPACE}" \
    --for=condition=ready pod \
    --selector=app.kubernetes.io/name="${APP}" \
    --timeout=180s || log "WARNING: ${APP} not ready yet — check with: kubectl get pods -n ${NAMESPACE}"
done

# ---------------------------------------------------------------------------
# 7. Summary
# ---------------------------------------------------------------------------
log ""
log "Setup complete. Cluster summary:"
kubectl get pods -n "${NAMESPACE}"
log ""
log "Access the app at:     http://localhost"
log "Access the API at:     http://localhost/api/items"
log "Health endpoint:       http://localhost/health"
log ""
log "Grafana (port-forward required):"
log "  kubectl port-forward -n sentinel svc/grafana 3000:3000"
log "  Then open: http://localhost:3000"
log ""
log "To tear down: kind delete cluster --name ${CLUSTER_NAME}"
