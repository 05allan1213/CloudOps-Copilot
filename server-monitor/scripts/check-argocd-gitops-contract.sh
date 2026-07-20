#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "$#" -ne 6 ]]; then
  printf 'usage: %s APPPROJECT_JSON APPLICATION_JSON VALUES_JSON HEALTHY_JSON REGRESSION_JSON VERSION_LOCK\n' "$0" >&2
  exit 2
fi

project_json="$1"
application_json="$2"
values_json="$3"
healthy_json="$4"
regression_json="$5"
version_lock="$6"

die() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

for required_file in \
  "${project_json}" "${application_json}" "${values_json}" \
  "${healthy_json}" "${regression_json}" "${version_lock}"; do
  [[ -s "${required_file}" ]] || die "missing or empty contract input: ${required_file}"
done

repository_url="https://github.com/05allan1213/cloudops-gitops-demo.git"
repository_name="05allan1213/cloudops-gitops-demo"
application_name="cloudops-demo"
project_name="cloudops-demo"
argocd_namespace="cloudops-argocd"
demo_namespace="cloudops-demo"
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
  --arg destination "${destination_server}" '
  .baselineVerifier.env.V3_TARGET_REPOSITORY == $repository and
  .baselineVerifier.env.V3_TARGET_BASE_BRANCH == "main" and
  .baselineVerifier.env.V3_TARGET_GITOPS_PATH == $deployment_path and
  .baselineVerifier.env.V3_TARGET_ARGO_PATH == $application_path and
  .baselineVerifier.env.V3_TARGET_ARGO_APPLICATION == $application and
  .baselineVerifier.env.V3_TARGET_ARGO_PROJECT == $project and
  .baselineVerifier.env.V3_TARGET_ARGO_DESTINATION_SERVER == $destination and
  .baselineVerifier.env.ARGOCD_ALLOWED_APPLICATIONS == $application and
  .baselineVerifier.env.ARGOCD_ALLOWED_PROJECTS == $project
' "${values_json}" >/dev/null || die "Argo assets no longer match the checked CloudOps target configuration"

jq -e --arg namespace "${demo_namespace}" '
  length == 5 and
  ([.[] | select(.apiVersion == "apps/v1" and .kind == "Deployment" and .metadata.name == "cloudops-demo-workload")] | length == 1) and
  ([.[] | select(.apiVersion == "v1" and .kind == "Service") | .metadata.name] | sort) == ["cloudops-demo-workload", "demo-diagnostics"] and
  ([.[] | select(.apiVersion == "monitoring.coreos.com/v1" and .kind == "PodMonitor" and .metadata.name == "cloudops-demo-workload")] | length == 1) and
  ([.[] | select(.apiVersion == "monitoring.coreos.com/v1" and .kind == "PrometheusRule" and .metadata.name == "cloudops-demo-workload")] | length == 1) and
  all(.[]; .metadata.namespace == $namespace) and
  all(.[].kind; . == "Deployment" or . == "Service" or . == "PodMonitor" or . == "PrometheusRule") and
  ([.[] | select(.kind == "Job" or .kind == "Secret" or .kind == "ServiceAccount" or .kind == "Role" or .kind == "RoleBinding" or .kind == "ClusterRole" or .kind == "ClusterRoleBinding" or .kind == "Ingress")] | length == 0) and
  ([.. | objects | select(has("secretKeyRef") or has("secretRef") or has("envFrom"))] | length == 0)
' "${healthy_json}" >/dev/null || die "healthy GitOps fixture inventory exceeds the AppProject or contains secret/RBAC/load-generator state"

jq -e '
  [
    .[] |
    select(.kind == "Deployment" and .metadata.name == "cloudops-demo-workload") |
    .spec.template.spec.containers[] |
    select(.name == "cloudops-demo") |
    .env[] |
    select(.name == "REQUIRED_ENV")
  ] == [{"name": "REQUIRED_ENV", "value": "baseline-present"}]
' "${healthy_json}" >/dev/null || die "healthy fixture must contain exactly one literal non-secret REQUIRED_ENV"

jq -e '
  length == 1 and
  .[0].apiVersion == "apps/v1" and
  .[0].kind == "Deployment" and
  .[0].metadata.name == "cloudops-demo-workload" and
  .[0].metadata.namespace == "cloudops-demo" and
  ([.[0].spec.template.spec.containers[] | select(.name == "cloudops-demo") | .env[] | select(.name == "REQUIRED_ENV")] | length == 0)
' "${regression_json}" >/dev/null || die "regression fixture is not the fixed Demo Deployment without REQUIRED_ENV"

jq -e --slurpfile regression "${regression_json}" '
  def remove_required_env:
    .spec.template.spec.containers |= map(
      if .name == "cloudops-demo"
      then .env |= map(select(.name != "REQUIRED_ENV"))
      else .
      end
    );
  ([.[] | select(.kind == "Deployment" and .metadata.name == "cloudops-demo-workload")][0] | remove_required_env) == $regression[0][0]
' "${healthy_json}" >/dev/null || die "regression fixture changes more than removal of REQUIRED_ENV"

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

printf 'PASS: canonical AppProject/Application and healthy/regression GitOps contracts are bounded and aligned\n'
