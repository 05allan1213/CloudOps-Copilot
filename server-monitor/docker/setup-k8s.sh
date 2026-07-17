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
    info "Generating kubeconfig for Docker Compose access..."
    local control_plane_container="${CLUSTER_NAME}-control-plane"

    local api_port
    api_port=$(docker inspect "${control_plane_container}" \
        --format '{{(index (index .NetworkSettings.Ports "6443/tcp") 0).HostPort}}' 2>/dev/null || true)

    if [ -z "${api_port}" ]; then
        fail "Cannot determine published API server port for '${control_plane_container}'"
    fi

    info "kind API server published on host.docker.internal:${api_port}"

    kind get kubeconfig --name "${CLUSTER_NAME}" 2>/dev/null | \
        sed -E "s|server: https://[^:]+:[0-9]+|server: https://host.docker.internal:${api_port}|g" | \
        awk '
            /server: https:\/\/host.docker.internal:/ {
                print
                print "    tls-server-name: localhost"
                next
            }
            { print }
        ' > "${KUBECONFIG_FILE}"

    chmod 644 "${KUBECONFIG_FILE}"
    ok "Kubeconfig written to ${KUBECONFIG_FILE} (server: https://host.docker.internal:${api_port})"
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

    echo ""
    echo "=========================================="
    ok "K8s environment setup complete!"
    echo ""
    echo "Next steps:"
    echo "  1. cd to server-monitor directory"
    echo "  2. 启动本地栈：docker compose up -d"
    echo "  3. 如需启用 Incident Agent/Workbench 的受限 K8s 读取，在 .env 中设置 K8S_ENABLED=true"
    echo "     （server-web 将通过 host.docker.internal 访问本机 kind API Server）"
    echo ""
    echo "If server-web is already running, restart it:"
    echo "  docker compose up -d server-web"
    echo "=========================================="
}

main "$@"
