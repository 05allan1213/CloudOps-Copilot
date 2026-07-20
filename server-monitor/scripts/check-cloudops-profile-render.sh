#!/usr/bin/env bash
set -Eeuo pipefail

phase3_manifest="${1:-}"
phase4_manifest="${2:-}"
phase5_manifest="${3:-}"
phase6_manifest="${4:-}"
chart_dir="${5:-}"

if [[ -z "${phase3_manifest}" || ! -s "${phase3_manifest}" ||
      -z "${phase4_manifest}" || ! -s "${phase4_manifest}" ||
      -z "${phase5_manifest}" || ! -s "${phase5_manifest}" ||
      -z "${phase6_manifest}" || ! -s "${phase6_manifest}" ||
      -z "${chart_dir}" || ! -d "${chart_dir}" ]]; then
  printf 'usage: %s PHASE3_JSON PHASE4_JSON PHASE5_JSON PHASE6_JSON CHART_DIR\n' "$0" >&2
  exit 2
fi

for command_name in jq helm; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    printf 'missing command: %s\n' "${command_name}" >&2
    exit 2
  }
done

check_profile() {
  local manifest="$1"
  local profile="$2"
  local oauth_enabled="$3"
  local worker_enabled="$4"
  local baseline_enabled="$5"

  jq -s -e \
    --arg profile "${profile}" \
    --arg oauth_enabled "${oauth_enabled}" \
    --arg worker_enabled "${worker_enabled}" \
    --arg baseline_enabled "${baseline_enabled}" '
    . as $documents |
    def rs($kind; $name): [$documents[] | select(.kind == $kind and .metadata.name == $name)];
    def count($kind; $name): rs($kind; $name) | length;
    def deployment($name): rs("Deployment"; $name)[0];
    def service($name): rs("Service"; $name)[0];
    def container($deployment; $name):
      [deployment($deployment).spec.template.spec.containers[] | select(.name == $name)][0];
    def env($deployment; $container; $name):
      [container($deployment; $container).env[]? | select(.name == $name)][0];
    def config: rs("ConfigMap"; "cloudops-config")[0];
    def oauth_config: config.data["oauth2-proxy-alpha-config.yaml"];
    def strips($name):
      oauth_config | contains("- name: " + $name + "\n    preserveRequestValue: false");
    def oauth: $oauth_enabled == "true";
    def worker: $worker_enabled == "true";
    def baseline: $baseline_enabled == "true";

    count("ConfigMap"; "cloudops-config") == 1 and
    count("Deployment"; "cloudops-api") == 1 and
    count("Job"; "cloudops-migrate") == 1 and
    count("Service"; "cloudops-api-internal") == 1 and
    count("ServiceAccount"; "cloudops-api") == 1 and
    count("ServiceAccount"; "cloudops-migrate") == 0 and
    count("ServiceMonitor"; "cloudops-api") == 1 and
    count("PrometheusRule"; "cloudops-api") == 1 and
    config.metadata.labels["cloudops.io/profile"] == $profile and
    ([$documents[] | select(.kind == "Secret" or .kind == "Ingress" or
                  .kind == "ClusterRole" or .kind == "ClusterRoleBinding")] | length) == 0 and
    all($documents[] | select(.kind == "Service");
      .spec.type == "ClusterIP" and
      all(.spec.ports[]; .nodePort == null and .targetPort != "user")) and
    deployment("cloudops-api").spec.template.spec.serviceAccountName == "cloudops-api" and
    deployment("cloudops-api").spec.template.spec.automountServiceAccountToken == false and
    deployment("cloudops-api").spec.template.spec.enableServiceLinks == false and
    rs("ServiceAccount"; "cloudops-api")[0].automountServiceAccountToken == false and
    rs("Job"; "cloudops-migrate")[0].spec.template.spec.automountServiceAccountToken == false and
    (rs("Job"; "cloudops-migrate")[0].spec.template.spec | has("serviceAccountName") | not) and
    (rs("Job"; "cloudops-migrate")[0].spec.template.spec.containers[0].env | map(.name) | sort) ==
      ["MYSQL_DATABASE","MYSQL_HOST","MYSQL_PASSWORD","MYSQL_PING_TIMEOUT_SECONDS","MYSQL_PORT","MYSQL_USER"] and
    service("cloudops-api-internal").spec.ports ==
      [{"name":"internal","port":8082,"protocol":"TCP","targetPort":"internal"}] and
    rs("ServiceMonitor"; "cloudops-api")[0].spec.endpoints[0].port == "internal" and
    rs("ServiceMonitor"; "cloudops-api")[0].spec.endpoints[0].path == "/metrics" and
    any(rs("PrometheusRule"; "cloudops-api")[0].spec.groups[].rules[];
      .alert == "CloudOpsAPIAvailability") and

    (if baseline then
      count("Job"; "cloudops-baseline-verifier") == 1 and
      count("ServiceAccount"; "cloudops-baseline-verifier") == 1 and
      count("Role"; "cloudops-baseline-verifier-readonly") == 1 and
      count("RoleBinding"; "cloudops-baseline-verifier-readonly") == 1 and
      rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.serviceAccountName == "cloudops-baseline-verifier" and
      rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.automountServiceAccountToken == true and
      rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.enableServiceLinks == false and
      rs("ServiceAccount"; "cloudops-baseline-verifier")[0].automountServiceAccountToken == true and
      rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].command == ["/app/cloudops-worker"] and
      rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].args == ["baseline-verify"] and
      (rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0] | has("envFrom") | not) and
      (rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].env | map(.name) |
        any(. == "MYSQL_USER")) and
      ([rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].env[] |
        select(.name == "MYSQL_USER")][0].value != config.data.MYSQL_USER) and
      ([rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].env[] |
        select(.name == "MYSQL_PASSWORD")][0].valueFrom.secretKeyRef.name != "cloudops-database") and
      all([
        "LLM_API_KEY","V3_LLM_API_KEY_FILE","GITHUB_APP_ID","GITHUB_INSTALLATION_ID",
        "GITHUB_PRIVATE_KEY_FILE","GITHUB_TOKEN_FILE","GITHUB_WRITE_APP_ID",
        "GITHUB_WRITE_INSTALLATION_ID","GITHUB_WRITE_PRIVATE_KEY_FILE","GITHUB_WRITE_TOKEN_FILE"
      ][]; . as $forbidden |
        (rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.containers[0].env |
          map(.name) | index($forbidden)) == null) and
      (rs("Job"; "cloudops-baseline-verifier")[0].spec.template.spec.volumes |
        [ .[] | select(.name == "baseline-credentials") ][0].secret.items | length) == 4 and
      rs("Role"; "cloudops-baseline-verifier-readonly")[0].metadata.namespace == "cloudops-demo" and
      ([rs("Role"; "cloudops-baseline-verifier-readonly")[0].rules[].resources[]] | sort) ==
        ["deployments","pods"] and
      all(rs("Role"; "cloudops-baseline-verifier-readonly")[0].rules[];
        all(.verbs[]; . == "get" or . == "list")) and
      rs("RoleBinding"; "cloudops-baseline-verifier-readonly")[0].subjects ==
        [{"kind":"ServiceAccount","name":"cloudops-baseline-verifier","namespace":"cloudops-system"}]
    else
      count("Job"; "cloudops-baseline-verifier") == 0 and
      count("ServiceAccount"; "cloudops-baseline-verifier") == 0 and
      count("Role"; "cloudops-baseline-verifier-readonly") == 0 and
      count("RoleBinding"; "cloudops-baseline-verifier-readonly") == 0
    end) and

    (if oauth then
      count("Service"; "cloudops-api-user") == 1 and
      service("cloudops-api-user").spec.ports ==
        [{"name":"oauth","port":4180,"protocol":"TCP","targetPort":"oauth"}] and
      (deployment("cloudops-api").spec.template.spec.containers | map(.name)) ==
        ["cloudops-api","oauth2-proxy"] and
      env("cloudops-api"; "cloudops-api"; "LISTEN_ADDR").value == "127.0.0.1:8080" and
      env("cloudops-api"; "cloudops-api"; "V3_PROXY_AUTH_ENABLED").value == "true" and
      env("cloudops-api"; "cloudops-api"; "V3_CSRF_SECRET_FILE").value ==
        "/var/run/secrets/cloudops/csrf/signing-key" and
      (container("cloudops-api"; "oauth2-proxy").args | index("--alpha-config=/etc/oauth2-proxy/alpha-config.yaml")) != null and
      (container("cloudops-api"; "oauth2-proxy").args | index("--session-cookie-minimal=true")) != null and
      (container("cloudops-api"; "oauth2-proxy").args | index("--cookie-refresh=0s")) != null and
      all(container("cloudops-api"; "oauth2-proxy").args[];
        (startswith("--pass-") or startswith("--set-xauthrequest") or
         startswith("--set-authorization-header") or
         startswith("--skip-auth-strip-headers")) | not) and
      ([container("cloudops-api"; "oauth2-proxy").env[].name] | sort) ==
        ["CLOUDOPS_OAUTH_CLIENT_ID","CLOUDOPS_OAUTH_CLIENT_SECRET","OAUTH2_PROXY_COOKIE_SECRET"] and
      all(container("cloudops-api"; "oauth2-proxy").env[];
        .value == null and
        (.valueFrom.secretKeyRef.name | length) > 0 and
        (.valueFrom.secretKeyRef.key | length) > 0) and
      (oauth_config | contains("uri: http://127.0.0.1:8080")) and
      (oauth_config | test("clientID: .*CLOUDOPS_OAUTH_CLIENT_ID")) and
      (oauth_config | test("clientSecret: .*CLOUDOPS_OAUTH_CLIENT_SECRET")) and
      (oauth_config | contains("- name: X-Auth-Request-User\n    preserveRequestValue: false\n    values:\n      - claimSource:\n          claim: user")) and
      (oauth_config | contains("- name: X-CSRF-Token\n    preserveRequestValue: true")) and
      all([
        "Authorization","Proxy-Authorization","Cookie","X-Forwarded-User",
        "X-Forwarded-Email","X-Forwarded-Groups","X-Forwarded-Preferred-Username",
        "X-Forwarded-Access-Token","X-Auth-Request-Email","X-Auth-Request-Groups",
        "X-Auth-Request-Preferred-Username","X-Auth-Request-Access-Token"
      ][]; strips(.)) and
      (oauth_config | contains("claim: access_token") | not)
    else
      count("Service"; "cloudops-api-user") == 0 and
      (deployment("cloudops-api").spec.template.spec.containers | map(.name)) == ["cloudops-api"] and
      env("cloudops-api"; "cloudops-api"; "LISTEN_ADDR").value == ":8080" and
      env("cloudops-api"; "cloudops-api"; "V3_PROXY_AUTH_ENABLED").value == "false" and
      env("cloudops-api"; "cloudops-api"; "V3_CSRF_SECRET_FILE").value == "" and
      (config.data | has("oauth2-proxy-alpha-config.yaml") | not)
    end) and

    (if worker then
      count("Deployment"; "cloudops-worker") == 1 and
      count("Service"; "cloudops-worker-management") == 1 and
      count("ServiceAccount"; "cloudops-worker") == 1 and
      count("Role"; "cloudops-worker-readonly") == 1 and
      count("RoleBinding"; "cloudops-worker-readonly") == 1 and
      deployment("cloudops-worker").spec.template.spec.serviceAccountName == "cloudops-worker" and
      deployment("cloudops-worker").spec.template.spec.automountServiceAccountToken == true and
      rs("ServiceAccount"; "cloudops-worker")[0].automountServiceAccountToken == true and
      env("cloudops-worker"; "cloudops-worker"; "V3_WORKER_PROVIDERS_ENABLED").value == "true" and
      env("cloudops-worker"; "cloudops-worker"; "K8S_WRITE_ENABLED").value == "false" and
      env("cloudops-worker"; "cloudops-worker"; "ASYNC_WORKER_ID").valueFrom.fieldRef.fieldPath == "metadata.name" and
      (container("cloudops-worker"; "cloudops-worker").envFrom | length) == 1 and
      (container("cloudops-worker"; "cloudops-worker").envFrom[0].secretRef.name | length) > 0 and
      (deployment("cloudops-worker").spec.template.spec.volumes |
        any(.[]; .name == "worker-credentials" and (.secret.secretName | length) > 0)) and
      service("cloudops-worker-management").spec.ports ==
        [{"name":"management","port":8081,"protocol":"TCP","targetPort":"management"}] and
      rs("Role"; "cloudops-worker-readonly")[0].metadata.namespace == "cloudops-demo" and
      ([rs("Role"; "cloudops-worker-readonly")[0].rules[].resources[]] | sort) ==
        ["deployments","endpointslices","events","pods","replicasets","services"] and
      all(rs("Role"; "cloudops-worker-readonly")[0].rules[]; (.verbs | sort) == ["get","list"]) and
      rs("RoleBinding"; "cloudops-worker-readonly")[0].subjects ==
        [{"kind":"ServiceAccount","name":"cloudops-worker","namespace":"cloudops-system"}] and
      count("ServiceMonitor"; "cloudops-worker") == 1 and
      any(rs("PrometheusRule"; "cloudops-api")[0].spec.groups[].rules[];
        .alert == "CloudOpsWorkerAvailability")
    else
      count("Deployment"; "cloudops-worker") == 0 and
      count("Service"; "cloudops-worker-management") == 0 and
      count("ServiceAccount"; "cloudops-worker") == 0 and
      count("Role"; "cloudops-worker-readonly") == 0 and
      count("RoleBinding"; "cloudops-worker-readonly") == 0 and
      count("ServiceMonitor"; "cloudops-worker") == 0 and
      (any(rs("PrometheusRule"; "cloudops-api")[0].spec.groups[].rules[];
        .alert == "CloudOpsWorkerAvailability") | not)
    end)
  ' "${manifest}" >/dev/null || {
    printf 'rendered CloudOps %s profile violates its ownership/auth/RBAC contract\n' "${profile}" >&2
    exit 1
  }
}

expect_template_failure() {
  local label="$1"
  local values_file="$2"
  shift 2
  if helm template cloudops-negative "${chart_dir}" \
      --namespace cloudops-system --values "${values_file}" "$@" >/dev/null 2>&1; then
    printf 'negative Helm contract unexpectedly rendered: %s\n' "${label}" >&2
    exit 1
  fi
}

check_profile "${phase3_manifest}" phase3 false false false
check_profile "${phase4_manifest}" phase4 false false false
check_profile "${phase5_manifest}" phase5 true false true
check_profile "${phase6_manifest}" phase6 true true true

expect_template_failure "phase3 oauth enablement" "${chart_dir}/values-phase3.yaml" --set oauth.enabled=true
expect_template_failure "phase4 worker enablement" "${chart_dir}/values-phase4.yaml" --set worker.enabled=true
expect_template_failure "phase5 oauth disablement" "${chart_dir}/values-phase5.yaml" --set oauth.enabled=false
expect_template_failure "phase5 worker enablement" "${chart_dir}/values-phase5.yaml" --set worker.enabled=true
expect_template_failure "phase5 baseline verifier disablement" "${chart_dir}/values-phase5.yaml" --set baselineVerifier.enabled=false
expect_template_failure "phase6 oauth disablement" "${chart_dir}/values-phase6.yaml" --set oauth.enabled=false
expect_template_failure "phase6 worker disablement" "${chart_dir}/values-phase6.yaml" --set worker.enabled=false
expect_template_failure "phase6 Kubernetes writes" "${chart_dir}/values-phase6.yaml" --set-string worker.env.K8S_WRITE_ENABLED=true
expect_template_failure "phase6 namespace drift" "${chart_dir}/values-phase6.yaml" --set-string worker.env.K8S_DEFAULT_NAMESPACE=other
expect_template_failure "baseline Kubernetes writes" "${chart_dir}/values-phase5.yaml" --set-string baselineVerifier.env.K8S_WRITE_ENABLED=true
expect_template_failure "baseline namespace drift" "${chart_dir}/values-phase5.yaml" --set-string baselineVerifier.env.K8S_DEFAULT_NAMESPACE=other
expect_template_failure "baseline runtime database reuse" "${chart_dir}/values-phase5.yaml" --set-string baselineVerifier.database.user=cloudops
expect_template_failure "baseline runtime database Secret reuse" "${chart_dir}/values-phase5.yaml" --set-string baselineVerifier.database.secretName=cloudops-database
expect_template_failure "baseline LLM credential" "${chart_dir}/values-phase5.yaml" --set-string baselineVerifier.credentials.files.V3_LLM_API_KEY_FILE=llm-api-key
expect_template_failure "oauth and csrf Secret reuse" "${chart_dir}/values-phase5.yaml" --set-string oauth.secret.name=cloudops-csrf
expect_template_failure "duplicate OAuth role mapping" "${chart_dir}/values-phase5.yaml" --set-string 'oauth.operatorLogins[0]=cloudops-viewer'
expect_template_failure "oauth2-proxy version drift" "${chart_dir}/values-phase5.yaml" --set-string images.oauth2Proxy.tag=v7.15.2
expect_template_failure "mutable latest image" "${chart_dir}/values-phase3.yaml" --set-string images.api.tag=latest

printf 'PASS: CloudOps phase3-6 rendered ownership, OAuth header stripping, baseline verifier isolation, Secret references, Service exposure, and SA/RBAC boundaries.\n'
