#!/usr/bin/env bash
# Spin up a kind cluster, build local images, helm-install the chart,
# wait for everything to reach Ready, hit /healthz, and print summary.
# Tear down with `kind delete cluster --name csw-smoke`.
#
# Usage:   deploy/scripts/kind-smoke.sh [--no-cleanup]
#
# Prereqs: kind, kubectl, helm, docker, openssl in PATH.

set -euo pipefail

CLUSTER="${KIND_CLUSTER:-csw-smoke}"
NAMESPACE="${KIND_NAMESPACE:-csw}"
RELEASE="csw"
CHART_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../helm/chain-sync-watch" && pwd)"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Image refs match the chart defaults so we only override .tag below.
BACKEND_IMG="ghcr.io/seokheejang/chain-sync-watch:smoke"
WEB_IMG="ghcr.io/seokheejang/chain-sync-watch-web:smoke"

step() { printf '\n\033[1;36m▶ %s\033[0m\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# ----- 1. cluster --------------------------------------------------
step "Ensuring kind cluster '${CLUSTER}'"
if ! kind get clusters | grep -qx "${CLUSTER}"; then
  kind create cluster --name "${CLUSTER}" --wait 60s
else
  note "cluster already exists; reusing"
fi

# ----- 2. images ---------------------------------------------------
step "Building backend image"
docker build -t "${BACKEND_IMG}" -f "${REPO_ROOT}/Dockerfile" "${REPO_ROOT}"

step "Building web image"
docker build -t "${WEB_IMG}" -f "${REPO_ROOT}/web/Dockerfile" "${REPO_ROOT}/web"

step "Loading images into kind"
kind load docker-image --name "${CLUSTER}" "${BACKEND_IMG}" "${WEB_IMG}"

# ----- 3. helm install ---------------------------------------------
step "helm dependency update"
helm dependency update "${CHART_DIR}" >/dev/null

step "helm upgrade --install ${RELEASE}"
helm upgrade --install "${RELEASE}" "${CHART_DIR}" \
  -n "${NAMESPACE}" --create-namespace \
  -f "${CHART_DIR}/environments/values.dev.yaml" \
  --set image.tag=smoke \
  --set imageWeb.tag=smoke \
  --set image.pullPolicy=IfNotPresent \
  --set imageWeb.pullPolicy=IfNotPresent \
  --set secrets.CSW_SECRET_KEY="$(openssl rand -base64 32)" \
  --wait --timeout 5m

# ----- 4. smoke probes ---------------------------------------------
step "Probing /healthz"
SVC="${RELEASE}-chain-sync-watch-server"
kubectl -n "${NAMESPACE}" wait pod \
  -l app.kubernetes.io/instance="${RELEASE}",app.kubernetes.io/component=server \
  --for=condition=Ready --timeout=120s

# Background port-forward; pick an unused local port.
LOCAL_PORT="${LOCAL_PORT:-18080}"
kubectl -n "${NAMESPACE}" port-forward "svc/${SVC}" "${LOCAL_PORT}:8080" >/dev/null 2>&1 &
PF_PID=$!
trap 'kill ${PF_PID} 2>/dev/null || true' EXIT

# Give port-forward a moment to bind.
for i in 1 2 3 4 5; do
  if curl -fsS "http://localhost:${LOCAL_PORT}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if curl -fsS "http://localhost:${LOCAL_PORT}/healthz"; then
  echo
  note "✓ /healthz returned 200"
else
  echo
  echo "✗ /healthz failed" >&2
  kubectl -n "${NAMESPACE}" get pods -o wide
  exit 1
fi

# ----- 5. summary --------------------------------------------------
step "Summary"
kubectl -n "${NAMESPACE}" get pods -o wide
echo
note "Web UI:  kubectl -n ${NAMESPACE} port-forward svc/${RELEASE}-chain-sync-watch-web 3000:3000"
note "API:     kubectl -n ${NAMESPACE} port-forward svc/${SVC} 8080:8080"
note "Tear down: kind delete cluster --name ${CLUSTER}"

if [[ "${1:-}" == "--no-cleanup" ]]; then
  exit 0
fi
