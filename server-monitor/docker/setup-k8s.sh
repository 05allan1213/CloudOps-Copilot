#!/usr/bin/env bash
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-cloudops-test}"
KUBECONFIG_DIR="$(cd "$(dirname "$0")" && pwd)"
KUBECONFIG_FILE="${KUBECONFIG_DIR}/kubeconfig"
NAMESPACE_LIST="${K8S_ALLOWED_NAMESPACES:-default,cloudops-test}"
NGINX_REPLICAS="${NGINX_REPLICAS:-2}"

info()  { echo -e "\033[1;34m[INFO]\033[0m  $*"; }
ok()    { echo -e "\033[1;32m[OK]\033[0m    $*"; }
warn()  { echo -e "\033[1;33m[WARN]\033[0m  $*"; }
fail()  { echo -e "\033[1;31m[FAIL]\033[0m  $*"; exit 1; }

check_prerequisites() {
    info "Checking prerequisites..."
    command -v docker >/dev/null 2>&1 || fail "docker is required"
    command -v kind >/dev/null 2>&1    || fail "kind is required"
    command -v kubectl >/dev/null 2>&1 || fail "kubectl is required"
    ok "All prerequisites met"
}

ensure_kind_cluster() {
    if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
        ok "Kind cluster '${CLUSTER_NAME}' already exists"
    else
        info "Creating kind cluster '${CLUSTER_NAME}'..."
        kind create cluster --name "${CLUSTER_NAME}" --wait 60s
        ok "Kind cluster '${CLUSTER_NAME}' created"
    fi
}

ensure_namespaces() {
    info "Ensuring namespaces exist..."
    for ns in ${NAMESPACE_LIST//,/ }; do
        if kubectl get namespace "${ns}" >/dev/null 2>&1; then
            ok "Namespace '${ns}' exists"
        else
            info "Creating namespace '${ns}'..."
            kubectl create namespace "${ns}"
            ok "Namespace '${ns}' created"
        fi
    done
}

ensure_nginx() {
    info "Ensuring nginx deployment in 'default' namespace..."
    if kubectl get deployment nginx -n default >/dev/null 2>&1; then
        ok "nginx deployment already exists"
    else
        info "Deploying nginx (${NGINX_REPLICAS} replicas)..."
        kubectl create deployment nginx --image=nginx:alpine --replicas="${NGINX_REPLICAS}"
        ok "nginx deployment created"
    fi
    kubectl rollout status deployment nginx --timeout=60s >/dev/null 2>&1 || warn "nginx rollout not complete yet"
    ok "nginx is ready"
}

generate_kubeconfig() {
    info "Generating kubeconfig for Docker internal access..."
    local control_plane_container="${CLUSTER_NAME}-control-plane"

    local kind_ip
    kind_ip=$(docker inspect "${control_plane_container}" \
        --format '{{(index .NetworkSettings.Networks "kind").IPAddress}}' 2>/dev/null || true)

    if [ -z "${kind_ip}" ]; then
        kind_ip=$(docker inspect "${control_plane_container}" | \
            python3 -c "import sys,json; d=json.load(sys.stdin); print(d[0]['NetworkSettings']['Networks']['kind']['IPAddress'])" 2>/dev/null || true)
    fi

    if [ -z "${kind_ip}" ]; then
        fail "Cannot determine kind control plane IP on 'kind' network"
    fi

    info "Control plane IP on kind network: ${kind_ip}"

    kind get kubeconfig --name "${CLUSTER_NAME}" --internal 2>/dev/null | \
        sed "s|server: https://[0-9.]*:[0-9]*|server: https://${kind_ip}:6443|g" \
        > "${KUBECONFIG_FILE}"

    chmod 644 "${KUBECONFIG_FILE}"
    ok "Kubeconfig written to ${KUBECONFIG_FILE} (server: https://${kind_ip}:6443)"
}

ensure_docker_network() {
    info "Ensuring kind Docker network exists..."
    if docker network ls --format '{{.Name}}' | grep -q '^kind$'; then
        ok "Docker network 'kind' exists"
    else
        fail "Docker network 'kind' not found. Is kind cluster running?"
    fi
}

main() {
    echo "=========================================="
    echo " CloudOps K8s Environment Setup"
    echo "=========================================="
    echo ""

    check_prerequisites
    ensure_kind_cluster
    ensure_namespaces
    ensure_nginx
    generate_kubeconfig
    ensure_docker_network

    echo ""
    echo "=========================================="
    ok "K8s environment setup complete!"
    echo ""
    echo "Next steps:"
    echo "  1. cd to server-monitor directory"
    echo "  2. docker compose up -d"
    echo "     (server-web will auto-connect to 'kind' network via docker-compose.yml)"
    echo ""
    echo "If server-web is already running, restart it:"
    echo "  docker compose up -d server-web"
    echo "=========================================="
}

main "$@"
