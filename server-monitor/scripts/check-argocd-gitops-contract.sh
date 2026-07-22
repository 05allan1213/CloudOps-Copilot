#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "$#" -ne 6 ]]; then
  printf 'usage: %s APPPROJECT_JSON APPLICATION_JSON VALUES_JSON HEALTHY_DIR REGRESSION_DIR VERSION_LOCK\n' "$0" >&2
  exit 2
fi

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
project_json="$1"
application_json="$2"
values_json="$3"
healthy_dir="$4"
regression_dir="$5"
version_lock="$6"

die() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

for required_file in "${project_json}" "${application_json}" "${values_json}" "${version_lock}"; do
  [[ -s "${required_file}" ]] || die "missing or empty contract input: ${required_file}"
done
for required_dir in "${healthy_dir}" "${regression_dir}"; do
  [[ -d "${required_dir}" ]] || die "missing contract fixture directory: ${required_dir}"
done

repository_url="https://github.com/05allan1213/cloudops-gitops-demo.git"
repository_name="05allan1213/cloudops-gitops-demo"
application_name="cloudops-demo"
project_name="cloudops-demo"
argocd_namespace="cloudops-argocd"
demo_namespace="demo"
destination_server="https://kubernetes.default.svc"
application_path="apps/demo"
deployment_path="apps/demo/deployment.yaml"
target_revision="main"

jq -e \
  --arg repository "${repository_url}" \
  --arg project "${project_name}" \
  --arg namespace "${argocd_namespace}" \
  --arg demo_namespace "${demo_namespace}" \
  --arg destination "${destination_server}" '
  .apiVersion == "argoproj.io/v1alpha1" and
  .kind == "AppProject" and
  .metadata.name == $project and
  .metadata.namespace == $namespace and
  .spec.sourceRepos == [$repository] and
  .spec.destinations == [{"server": $destination, "namespace": $demo_namespace}] and
  .spec.clusterResourceWhitelist == [] and
  ((.spec.namespaceResourceWhitelist | sort_by(.group, .kind)) == [
    {"group": "", "kind": "Service"},
    {"group": "apps", "kind": "Deployment"},
    {"group": "monitoring.coreos.com", "kind": "PodMonitor"},
    {"group": "monitoring.coreos.com", "kind": "PrometheusRule"}
  ]) and
  ((.spec.roles // []) | length == 0)
' "${project_json}" >/dev/null || die "AppProject repository, destination, or resource allowlist drifted"

jq -e \
  --arg repository "${repository_url}" \
  --arg application "${application_name}" \
  --arg project "${project_name}" \
  --arg namespace "${argocd_namespace}" \
  --arg demo_namespace "${demo_namespace}" \
  --arg destination "${destination_server}" \
  --arg path "${application_path}" \
  --arg revision "${target_revision}" '
  .apiVersion == "argoproj.io/v1alpha1" and
  .kind == "Application" and
  .metadata.name == $application and
  .metadata.namespace == $namespace and
  .spec.project == $project and
  (.spec.sources == null) and
  .spec.source.repoURL == $repository and
  .spec.source.targetRevision == $revision and
  .spec.source.path == $path and
  .spec.source.directory.recurse == false and
  (.spec.source.helm == null) and
  (.spec.source.kustomize == null) and
  .spec.destination.server == $destination and
  .spec.destination.namespace == $demo_namespace and
  .spec.syncPolicy.automated.enabled == true and
  .spec.syncPolicy.automated.selfHeal == true and
  .spec.syncPolicy.automated.prune == false and
  .spec.syncPolicy.automated.allowEmpty == false and
  .spec.syncPolicy.retry.limit == 5 and
  .spec.syncPolicy.retry.backoff.duration == "5s" and
  .spec.syncPolicy.retry.backoff.factor == 2 and
  .spec.syncPolicy.retry.backoff.maxDuration == "3m"
' "${application_json}" >/dev/null || die "single-source Application or automated retry policy drifted"

jq -e \
  --arg repository "${repository_name}" \
  --arg deployment_path "${deployment_path}" \
  --arg application_path "${application_path}" \
  --arg application "${application_name}" \
  --arg project "${project_name}" \
  --arg destination "${destination_server}" \
  --arg demo_namespace "${demo_namespace}" '
  (.api.env.SIGNAL_TARGET_ALLOWLIST_JSON | fromjson) == [{
    "cluster_id": "kind-cloudops-v3",
    "environment": "local-demo",
    "namespace": $demo_namespace,
    "workload_kind": "Deployment",
    "workload_name": "demo",
    "service_name": "demo",
    "match_labels": {
      "cluster": "kind-cloudops-v3",
      "environment": "local-demo",
      "namespace": $demo_namespace,
      "deployment": "demo"
    }
  }] and
  .worker.demoNamespace == $demo_namespace and
  .worker.env.K8S_ALLOWED_NAMESPACES == $demo_namespace and
  .worker.env.K8S_DEFAULT_NAMESPACE == $demo_namespace and
  .worker.env.V3_TARGET_NAMESPACE == $demo_namespace and
  .baselineVerifier.demoNamespace == $demo_namespace and
  .baselineVerifier.env.K8S_ALLOWED_NAMESPACES == $demo_namespace and
  .baselineVerifier.env.K8S_DEFAULT_NAMESPACE == $demo_namespace and
  .baselineVerifier.env.V3_TARGET_NAMESPACE == $demo_namespace and
  .baselineVerifier.env.V3_TARGET_SERVICE == "demo" and
  .baselineVerifier.env.V3_TARGET_WORKLOAD == "demo" and
  .baselineVerifier.env.V3_TARGET_CONTAINER == "demo" and
  .baselineVerifier.env.V3_TARGET_REPOSITORY == $repository and
  .baselineVerifier.env.V3_TARGET_BASE_BRANCH == "main" and
  .baselineVerifier.env.V3_TARGET_GITOPS_PATH == $deployment_path and
  .baselineVerifier.env.V3_TARGET_ARGO_PATH == $application_path and
  .baselineVerifier.env.V3_TARGET_ARGO_APPLICATION == $application and
  .baselineVerifier.env.V3_TARGET_ARGO_PROJECT == $project and
  .baselineVerifier.env.V3_TARGET_ARGO_DESTINATION_SERVER == $destination and
  .baselineVerifier.env.V3_TARGET_READY_URL == "http://demo-diagnostics.demo.svc:8080/readyz" and
  .baselineVerifier.env.V3_TARGET_REQUIRED_ENV_KEY == "REQUIRED_ENV" and
  .baselineVerifier.env.V3_REQUIRED_CHECK_NAME == "gitops-required-check" and
  .baselineVerifier.env.V3_REQUIRED_WORKFLOW_PATH == ".github/workflows/gitops-required-check.yml" and
  .baselineVerifier.env.ARGOCD_ALLOWED_APPLICATIONS == $application and
  .baselineVerifier.env.ARGOCD_ALLOWED_PROJECTS == $project
' "${values_json}" >/dev/null || die "Argo assets no longer match the checked CloudOps target configuration"

go -C "${root_dir}" run ./cmd/gitops-demo-contract healthy "${healthy_dir}" >/dev/null || die "healthy GitOps fixture violates the external required-check contract"
go -C "${root_dir}" run ./cmd/gitops-demo-contract regression "${healthy_dir}" "${regression_dir}" >/dev/null || die "regression fixture changes more than removal of REQUIRED_ENV"

lock_value() {
  local key="$1"
  awk -F= -v key="${key}" '$1 == key {print substr($0, index($0, "=") + 1)}' "${version_lock}"
}

chart_version="$(lock_value ARGOCD_CHART_VERSION)"
app_version="$(lock_value ARGOCD_APP_VERSION)"
chart_url="$(lock_value ARGOCD_CHART_URL)"
chart_sha="$(lock_value ARGOCD_CHART_SHA256)"

[[ "${chart_version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "Argo CD chart version lock is invalid"
[[ "${app_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "Argo CD app version lock is invalid"
[[ "${chart_url}" == "https://github.com/argoproj/argo-helm/releases/download/argo-cd-${chart_version}/argo-cd-${chart_version}.tgz" ]] || die "Argo CD chart URL does not match its version lock"
[[ "${chart_sha}" =~ ^[0-9a-f]{64}$ ]] || die "Argo CD chart checksum lock is invalid"

printf 'PASS: canonical AppProject/Application and fixed five-file healthy/regression GitOps contracts are bounded and aligned\n'
