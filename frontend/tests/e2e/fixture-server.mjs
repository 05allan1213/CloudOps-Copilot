import http from "node:http";

const fixtureSchemaVersion = 1;
const port = Number(process.env.CLOUDOPS_E2E_FIXTURE_PORT || 18082);
const appOrigin = process.env.CLOUDOPS_E2E_APP_ORIGIN || "http://127.0.0.1:4173";
const incidentID = "00000000-0000-4000-8000-000000000001";
const planID = "00000050-0000-4000-8000-000000000001";
const deliveryID = "00000060-0000-4000-8000-000000000001";
const defaultFixture = Object.freeze({
  role: "operator",
  command: "202",
  plan: "valid",
  verification: "passed",
  list: "ready",
  detail: "ready",
  sections: "ready",
  sse: "connected",
  decision: null,
});
const fixture = { ...defaultFixture };
const metrics = {
  commands: [],
  eventConnections: 0,
  sseAttempts: 0,
  lastEventID: "",
};
const activeStreams = new Set();

function publicID(group, number) {
  return `${String(group).padStart(8, "0")}-0000-4000-8000-${String(number).padStart(12, "0")}`;
}

function sha256(character) {
  return character.repeat(64).slice(0, 64);
}

function gitSHA(character) {
  return character.repeat(40).slice(0, 40);
}

function at(second) {
  return new Date(Date.UTC(2026, 6, 23, 3, 0, second)).toISOString();
}

function cors(request) {
  return {
    "Access-Control-Allow-Origin": request.headers.origin || appOrigin,
    "Access-Control-Allow-Credentials": "true",
    "Access-Control-Allow-Headers": "Content-Type, X-CSRF-Token, Idempotency-Key, Last-Event-ID",
    "Access-Control-Allow-Methods": "GET,POST,OPTIONS",
    "Access-Control-Expose-Headers": "X-Request-ID, X-Trace-ID, Idempotent-Replay",
    Vary: "Origin",
  };
}

function problem(url, status, code, detail) {
  return {
    type: "about:blank",
    title: status === 403 ? "Forbidden" : status === 409 ? "Command conflict" : status === 422 ? "Invalid transition" : "Service unavailable",
    status,
    detail,
    instance: url.pathname,
    code,
    request_id: `fixture-request-${status}`,
    trace_id: `fixture-trace-${status}`,
  };
}

function json(request, response, status, body, headers = {}) {
  response.writeHead(status, {
    ...cors(request),
    "Content-Type": status >= 400 ? "application/problem+json" : "application/json",
    "X-Request-ID": headers["X-Request-ID"] || `fixture-request-${status}`,
    "X-Trace-ID": headers["X-Trace-ID"] || `fixture-trace-${status}`,
    ...headers,
  });
  response.end(JSON.stringify(body));
}

async function requestBody(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return Buffer.concat(chunks).toString("utf8");
}

function currentIncident() {
  return {
    id: incidentID,
    cycle: 3,
    status: "awaiting_approval",
    severity: "critical",
    summary: "Checkout API recovery requires an exact, hash-bound GitOps remediation decision",
    version: 7,
    needs_attention: true,
    blocking_reason_code: "approval_required",
    migrated_legacy: false,
    migrated_legacy_context: false,
    created_at: at(0),
    updated_at: at(900),
  };
}

function listedIncidents() {
  return [
    currentIncident(),
    {
      ...currentIncident(),
      id: publicID(1, 2),
      cycle: 1,
      status: "investigating",
      severity: "warning",
      summary: "Payments worker retry saturation is under bounded evidence-first investigation",
      version: 4,
      needs_attention: false,
      blocking_reason_code: "",
      created_at: at(-7200),
      updated_at: at(480),
    },
    {
      ...currentIncident(),
      id: publicID(1, 3),
      cycle: 2,
      status: "resolved",
      severity: "info",
      summary: "Inventory API recovery was proved by a complete deterministic verification window",
      version: 12,
      needs_attention: false,
      blocking_reason_code: "",
      created_at: at(-14400),
      updated_at: at(240),
    },
  ];
}

const generic = {
  signals: [{
    id: publicID(10, 1), kind: "alertmanager_signal", status: "firing", cycle: 3, version: 1,
    summary: "Checkout API p99 latency exceeded the persisted threshold after an exact GitOps revision.", hash: sha256("1"),
    migrated_legacy: false, migrated_legacy_context: false, created_at: at(1), updated_at: at(1),
  }],
  timeline: [{
    id: publicID(20, 1), kind: "timeline_event", status: "awaiting_approval", cycle: 3, version: 1,
    summary: "The server persisted a bounded remediation Plan and now requires an operator Decision.", hash: sha256("2"),
    migrated_legacy: false, migrated_legacy_context: false, created_at: at(2), updated_at: at(2),
  }],
  evidence: [{
    id: publicID(40, 1), kind: "evidence_item", status: "available", cycle: 3, version: 1,
    summary: "Persisted deployment and configuration evidence bound to the remediation Plan.", hash: sha256("3"),
    migrated_legacy: false, migrated_legacy_context: false, created_at: at(3), updated_at: at(3),
  }],
  investigations: [{
    id: publicID(30, 1), kind: "investigation", status: "diagnosed", cycle: 3, version: 2,
    summary: "Bounded Investigation identified a missing required environment variable without exposing private reasoning.", hash: sha256("4"),
    migrated_legacy: false, migrated_legacy_context: false, created_at: at(4), updated_at: at(4),
  }],
};

function longDiff() {
  const lines = [
    "diff --git a/deploy/checkout-api.yaml b/deploy/checkout-api.yaml",
    "index 1111111..2222222 100644",
    "--- a/deploy/checkout-api.yaml",
    "+++ b/deploy/checkout-api.yaml",
    "@@ -42,6 +42,9 @@ spec:",
    "         env:",
    "+          - name: REQUIRED_CHECKOUT_REGION",
    "+            value: us-east-1",
    "+          # Exact bounded remediation; no other field changes.",
  ];
  for (let index = 1; index <= 180; index += 1) {
    lines.push(` context line ${String(index).padStart(3, "0")}: persisted complete diff remains readable across a deliberately long artifact with exact repository path, immutable revision, policy identity, verification profile, and evidence binding context`);
  }
  return lines.join("\n");
}

function decision(decisionValue, reason) {
  return {
    id: publicID(51, 1),
    decision_schema_version: 1,
    plan_version: 4,
    decision: decisionValue,
    actor: { provider: "github", login: "demo-operator", role: "operator" },
    reason,
    request_id: "fixture-request-202",
    request_authenticated_at: at(20),
    expires_at: "2099-01-01T00:00:00Z",
    approved_hash_schema_version: 1,
    approved_plan_hash: sha256("a"),
    approved_base_sha: gitSHA("b"),
    approved_post_image_hash: sha256("c"),
    approved_tree_hash: gitSHA("d"),
    approved_patch_hash: sha256("e"),
    approved_policy_hash: sha256("f"),
    approved_verification_hash: sha256("7"),
    approved_evidence_set_hash: sha256("8"),
    created_at: at(21),
  };
}

function remediationPlan() {
  const incident = currentIncident();
  const stale = fixture.plan === "stale";
  const expired = fixture.plan === "expired";
  return {
    id: planID,
    kind: "remediation_plan",
    cycle: incident.cycle,
    status: fixture.decision?.decision || "awaiting_approval",
    version: fixture.decision ? 6 : 5,
    plan_version: 4,
    plan_content_schema_version: 3,
    incident_version: stale ? incident.version - 2 : incident.version - 1,
    created_by_agent_run_id: publicID(30, 1),
    operation_type: "restore_required_env",
    risk_level: "medium",
    patch_summary: "Restore REQUIRED_CHECKOUT_REGION for the checkout-api Deployment",
    rollback_plan: "Revert the exact approved commit and wait for Argo to observe the prior immutable revision.",
    validation_plan: "Require CI, exact Argo revision detection, rollout observation, and the complete deterministic verification profile.",
    target: {
      repository: "github.com/example/cloudops-gitops",
      base_branch: "main",
      base_revision: gitSHA("b"),
      last_known_good_revision: gitSHA("9"),
      base_blob_sha: gitSHA("1"),
      file_mode: "100644",
      path: "deploy/environments/demo/checkout-api.yaml",
      field_ref: "spec.template.spec.containers[name=checkout-api].env[name=REQUIRED_CHECKOUT_REGION]",
      resource: {
        api_version: "apps/v1",
        kind: "Deployment",
        namespace: "checkout-system",
        name: "checkout-api",
        container: "checkout-api",
      },
    },
    hash_schema_version: 1,
    diagnosis_hash: sha256("6"),
    canonical_plan_hash: sha256("a"),
    expected_before_hash: sha256("b"),
    expected_post_image_hash: sha256("c"),
    expected_tree_hash: gitSHA("d"),
    proposed_patch_hash: sha256("e"),
    canonical_manifest: { operation: "restore_required_env", path: "deploy/environments/demo/checkout-api.yaml" },
    bounded_diff: longDiff(),
    policy_version: "gitops-remediation/v3.4.1",
    policy_hash: sha256("f"),
    policy_snapshot: { result: "allowed", constraints: ["single_file", "single_env", "draft_pr_only"] },
    verification_plan: { profile: "golden-required-env/v1", common_stability_window_ms: 60000, required_checks: 3 },
    verification_plan_hash: sha256("7"),
    evidence_bindings: [
      { id: publicID(40, 1), content_hash: sha256("3") },
      { id: publicID(40, 2), content_hash: sha256("4") },
    ],
    evidence_set_hash: sha256("8"),
    expires_at: expired ? "2020-01-01T00:00:00Z" : "2099-01-01T00:00:00Z",
    ...(fixture.decision ? { decision: fixture.decision } : {}),
    created_at: at(10),
    updated_at: at(11),
    migrated_legacy: false,
    migrated_legacy_context: false,
  };
}

function delivery() {
  return {
    id: deliveryID,
    kind: "delivery",
    cycle: 3,
    status: "delivered",
    version: 8,
    remediation_plan_id: planID,
    repository: "github.com/example/cloudops-gitops",
    base_revision: gitSHA("b"),
    head_branch: "cloudops/restore-checkout-region",
    commit_sha: gitSHA("2"),
    pr_number: 418,
    pr_url: "https://github.com/example/cloudops-gitops/pull/418",
    pr_state: "closed",
    ci_status: "passing",
    merged_commit_sha: gitSHA("3"),
    target_revision: gitSHA("3"),
    detected_revision: gitSHA("3"),
    argocd_application: "checkout-demo",
    argocd_project: "cloudops-demo",
    argocd_sync_status: "Synced",
    argocd_operation_phase: "Succeeded",
    argocd_health_status: "Healthy",
    resource_health: [{ kind: "Deployment", name: "checkout-api", health: "Healthy" }],
    cluster: "kind-cloudops",
    environment: "demo",
    namespace: "checkout-system",
    workload_kind: "Deployment",
    workload_name: "checkout-api",
    deployment_generation: 42,
    observed_generation: 42,
    rollout_revision: gitSHA("3"),
    desired_replicas: 6,
    updated_replicas: 6,
    available_replicas: 6,
    unavailable_replicas: 0,
    sync_started_at: at(30),
    sync_completed_at: at(40),
    delivery_started_at: at(22),
    delivery_deadline_at: at(600),
    delivery_completed_at: at(50),
    last_observed_at: at(55),
    created_at: at(22),
    updated_at: at(55),
    migrated_legacy: false,
    migrated_legacy_context: false,
  };
}

function samples(checkNumber, status) {
  return Array.from({ length: 8 }, (_, index) => ({
    id: publicID(80 + checkNumber, index + 1),
    schema_version: 1,
    sequence: index + 1,
    status,
    observed: { value: status === "passed" ? 0.04 + index / 1000 : 1.4, unit: "ratio", source: "persisted-fixture" },
    source_reference: "https://grafana.example.test/d/checkout-recovery",
    reason_code: status === "passed" ? "threshold_satisfied" : "threshold_not_satisfied",
    window_start_at: at(60 + index * 5),
    window_end_at: at(65 + index * 5),
    sampled_at: at(65 + index * 5),
    content_hash: sha256(String((checkNumber + index) % 10)),
    created_at: at(65 + index * 5),
    migrated_legacy: false,
    migrated_legacy_context: false,
  }));
}

function check(number, status, required = true) {
  return {
    id: publicID(70, number),
    spec_schema_version: 1,
    type: number === 1 ? "prometheus_threshold" : number === 2 ? "kubernetes_rollout" : "alert_absence",
    status,
    required,
    profile_id: "golden-required-env/v1",
    template_id: number === 1 ? "checkout-error-rate" : number === 2 ? "checkout-rollout-ready" : "checkout-alert-absent",
    template_version: "v3.1.0",
    subject: {
      revision: gitSHA("3"),
      cluster: "kind-cloudops",
      environment: "demo",
      namespace: "checkout-system",
      service: "checkout-api",
      workload_kind: "Deployment",
      workload_name: "checkout-api",
    },
    expected: { comparison: number === 3 ? "absent" : "lte", threshold: number === 1 ? 0.1 : 0 },
    observed: { value: status === "passed" ? 0.04 : 1.4, status },
    comparison: number === 3 ? "absent" : "lte",
    threshold: number === 3 ? undefined : number === 1 ? 0.1 : 0,
    source_reference: number === 2 ? "https://argo.example.test/applications/checkout-demo" : "https://grafana.example.test/d/checkout-recovery",
    source_identity: number === 2 ? "argocd:checkout-demo" : "prometheus:checkout-api",
    lookback_ms: 300000,
    initial_delay_ms: 0,
    stability_window_ms: 60000,
    timeout_ms: 300000,
    poll_interval_ms: 5000,
    min_samples: 6,
    sample_unit: "observations",
    failure_mode: "resets",
    first_checked_at: at(60),
    last_checked_at: at(120),
    ...(status === "passed" ? { passed_at: at(120), consecutive_success_since: at(60) } : {}),
    attempt_count: 12,
    ...(status === "passed" ? {} : { failure_reason: `Persisted ${status} result for this required check.` }),
    samples: status === "unavailable" ? [] : samples(number, status === "passed" ? "passed" : "failed"),
    created_at: at(60),
    updated_at: at(120),
    migrated_legacy: false,
    migrated_legacy_context: false,
  };
}

function verificationRun(attempt, status, trigger = "post_delivery") {
  const passed = status === "passed";
  const checkStatus = passed ? "passed" : status === "timed_out" ? "timed_out" : status === "inconclusive" ? "unavailable" : status;
  return {
    id: publicID(70, 100 + attempt),
    kind: "verification",
    cycle: 3,
    status,
    version: attempt + 2,
    trigger_type: trigger,
    ...(trigger === "post_delivery" ? { remediation_plan_id: planID, change_request_id: deliveryID } : { trigger_signal_id: publicID(10, 1) }),
    attempt,
    profile: {
      id: trigger === "post_delivery" ? "golden-required-env/v1" : "no-change/v1",
      version: 1,
      hash: sha256(String(attempt)),
      contract_version: 3,
    },
    revisions: {
      target_revision: gitSHA("3"),
      source_revision: gitSHA("4"),
      image_digest: `sha256:${sha256("5")}`,
      gitops_revision: gitSHA("3"),
    },
    started_at: at(60),
    deadline_at: at(360),
    ...(passed || ["failed", "timed_out", "inconclusive", "cancelled"].includes(status) ? { completed_at: at(130) } : {}),
    common_window: {
      stability_window_ms: 60000,
      ...(passed ? { success_since: at(60), completed_at: at(120) } : status === "running" ? { success_since: at(110) } : {}),
    },
    result_summary: passed ? "All required checks passed across the complete common stable window." : `Recovery result is ${status}; the Workbench must not present this as resolved.`,
    ...(passed ? {} : { failure_reason: `${status}_recovery_not_proved` }),
    checks: [check(1, checkStatus, true), check(2, checkStatus, true), check(3, passed ? "passed" : "pending", false)],
    created_at: at(60),
    updated_at: at(130 + attempt),
    migrated_legacy: false,
    migrated_legacy_context: false,
  };
}

function verifications() {
  if (fixture.verification === "not_run") return [];
  return [
    verificationRun(1, "timed_out"),
    verificationRun(2, "inconclusive", "no_change_signal"),
    verificationRun(3, fixture.verification),
  ];
}

function resolutionReport() {
  return {
    id: publicID(90, 1),
    kind: "resolution_report",
    status: "resolved",
    cycle: 3,
    trigger_type: "post_delivery",
    resolution_reason: "verification_passed",
    service: "checkout-api",
    workload: "Deployment/checkout-api",
    environment: "demo",
    impact_summary: "Checkout error rate returned below the persisted threshold and remained stable for the complete common window.",
    summary: "Checkout API recovered after the exact approved GitOps remediation was delivered and deterministically verified.",
    hash: sha256("9"),
    cycle_started_at: at(0),
    resolved_at: at(130),
    measured_duration_ms: 130000,
    generated_at: at(131),
    revisions: {
      bad_gitops_revision: gitSHA("b"),
      fix_gitops_revision: gitSHA("3"),
      source_revision: gitSHA("4"),
      image_digest: `sha256:${sha256("5")}`,
      gitops_revision: gitSHA("3"),
    },
    verification_profile: { id: "golden-required-env/v1", hash: sha256("3") },
    stability: { common_window_started_at: at(60), common_window_completed_at: at(120) },
    trigger_signal: { id: publicID(10, 1), status: "firing" },
    diagnosis: { summary: "Required environment variable was absent from the GitOps manifest." },
    evidence: { ids: [publicID(40, 1), publicID(40, 2)] },
    remediation_plan: { id: planID, hash: sha256("a") },
    remediation_decision: { decision: "approved", actor: "demo-operator" },
    delivery: { id: deliveryID, target_revision: gitSHA("3") },
    verification: { run_id: publicID(70, 103), status: "passed" },
    timeline: { terminal_event: "incident_resolved" },
    agent_usage: { runs: 1, private_reasoning_exposed: false },
    migrated_legacy_context: false,
  };
}

const server = http.createServer(async (request, response) => {
  const url = new URL(request.url, `http://127.0.0.1:${port}`);
  if (request.method === "OPTIONS") {
    response.writeHead(204, cors(request));
    response.end();
    return;
  }

  if (url.pathname === "/fixture/config") {
    if (url.searchParams.get("reset") === "1") {
      Object.assign(fixture, defaultFixture);
      metrics.commands = [];
      metrics.eventConnections = 0;
      metrics.sseAttempts = 0;
      metrics.lastEventID = "";
    }
    for (const key of ["role", "command", "plan", "verification", "list", "detail", "sections", "sse"]) {
      const value = url.searchParams.get(key);
      if (value) fixture[key] = value;
    }
    json(request, response, 200, { fixture_schema_version: fixtureSchemaVersion, fixture, metrics });
    return;
  }

  if (url.pathname === "/fixture/state") {
    json(request, response, 200, { fixture_schema_version: fixtureSchemaVersion, fixture, metrics });
    return;
  }

  if (url.pathname === "/fixture/metrics") {
    json(request, response, 200, metrics);
    return;
  }

  if (url.pathname === "/api/v3/session/csrf") {
    json(request, response, 200, {
      token: "fixture-csrf-token",
      expires_at: "2099-01-01T00:00:00Z",
      actor: { provider: "github", login: `demo-${fixture.role}`, role: fixture.role },
    });
    return;
  }

  if (url.pathname === "/api/v3/incidents") {
    if (fixture.list === "loading") await new Promise((resolve) => setTimeout(resolve, 1200));
    if (fixture.list === "forbidden") {
      json(request, response, 403, problem(url, 403, "LIST_FORBIDDEN", "The current GitHub identity cannot read Incident projections."));
      return;
    }
    if (fixture.list === "error") {
      json(request, response, 503, problem(url, 503, "LIST_UNAVAILABLE", "The Incident list projection is temporarily unavailable."));
      return;
    }
    json(request, response, 200, { items: fixture.list === "empty" ? [] : listedIncidents() });
    return;
  }

  const decisionMatch = url.pathname.match(/^\/api\/v3\/remediation-plans\/([^/]+)\/decisions$/);
  if (request.method === "POST" && decisionMatch) {
    const rawBody = await requestBody(request);
    let body = {};
    try { body = JSON.parse(rawBody); } catch { /* fixture returns the configured response below */ }
    const record = {
      idempotencyKey: String(request.headers["idempotency-key"] || ""),
      csrfToken: String(request.headers["x-csrf-token"] || ""),
      origin: String(request.headers.origin || ""),
      body,
      mode: fixture.command,
    };
    metrics.commands.push(record);
    if (fixture.command === "timeout") {
      const timer = setTimeout(() => {
        if (!response.destroyed) json(request, response, 504, problem(url, 504, "COMMAND_TIMEOUT", "The fixture intentionally exceeded the client timeout."));
      }, 12000);
      request.on("close", () => clearTimeout(timer));
      return;
    }
    if (fixture.command === "403") {
      json(request, response, 403, problem(url, 403, "COMMAND_FORBIDDEN", "The authenticated identity is not allowed to execute this command."));
      return;
    }
    if (fixture.command === "409") {
      json(request, response, 409, problem(url, 409, "STALE_EXPECTATION", "The expected plan version or canonical hash is stale."));
      return;
    }
    if (fixture.command === "422") {
      json(request, response, 422, problem(url, 422, "INVALID_TRANSITION", "The business transition is not allowed."));
      return;
    }
    if (fixture.command === "501") {
      json(request, response, 501, problem(url, 501, "NOT_IMPLEMENTED", "Domain command remains the Phase 2 skeleton."));
      return;
    }
    if (fixture.command === "503") {
      json(request, response, 503, problem(url, 503, "COMMAND_UNAVAILABLE", "The command service is temporarily unavailable."));
      return;
    }
    fixture.decision = decision(body.decision || "approved", body.reason || "Fixture decision");
    const replay = metrics.commands.filter((item) => item.idempotencyKey === record.idempotencyKey).length > 1;
    json(request, response, 202, {
      id: planID,
      command: "remediation_plan.decide",
      status: "accepted",
      version: 6,
      cycle: 3,
    }, {
      "X-Request-ID": "fixture-request-202",
      "X-Trace-ID": "fixture-trace-202",
      ...(replay ? { "Idempotent-Replay": "true" } : {}),
    });
    return;
  }

  const incidentMatch = url.pathname.match(/^\/api\/v3\/incidents\/([^/]+)(?:\/(.*))?$/);
  if (incidentMatch && incidentMatch[1] === incidentID) {
    const resource = incidentMatch[2];
    if (!resource) {
      if (fixture.detail === "loading") await new Promise((resolve) => setTimeout(resolve, 1200));
      if (fixture.detail === "forbidden") {
        json(request, response, 403, problem(url, 403, "INCIDENT_FORBIDDEN", "The current GitHub identity cannot read this Incident projection."));
        return;
      }
      if (fixture.detail === "error") {
        json(request, response, 503, problem(url, 503, "INCIDENT_UNAVAILABLE", "The Incident projection is temporarily unavailable."));
        return;
      }
      json(request, response, 200, { incident: currentIncident() });
      return;
    }
    if (resource === "events") {
      metrics.eventConnections += 1;
      metrics.sseAttempts += 1;
      metrics.lastEventID = String(request.headers["last-event-id"] || "");
      if (fixture.sse === "offline" || (fixture.sse === "reconnect" && metrics.sseAttempts === 1)) {
        json(request, response, 503, problem(url, 503, "REALTIME_UNAVAILABLE", "The fixture is exercising the reconnect state."));
        return;
      }
      response.writeHead(200, {
        ...cors(request),
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
      });
      response.write(": connected\n\n");
      activeStreams.add(response);
      request.on("close", () => {
        activeStreams.delete(response);
        response.end();
      });
      return;
    }
    if (fixture.sections === "error" && resource === "evidence") {
      json(request, response, 503, problem(url, 503, "EVIDENCE_UNAVAILABLE", "The Evidence projection is temporarily unavailable."));
      return;
    }
    if (resource in generic) {
      json(request, response, 200, { items: fixture.sections === "empty" ? [] : generic[resource] });
      return;
    }
    if (resource === "remediation-plans") {
      json(request, response, 200, { items: fixture.sections === "empty" ? [] : [remediationPlan()] });
      return;
    }
    if (resource === "delivery") {
      if (fixture.sections === "empty") {
        json(request, response, 404, problem(url, 404, "DELIVERY_NOT_FOUND", "No Delivery projection exists for this cycle."));
        return;
      }
      json(request, response, 200, { resource: delivery() });
      return;
    }
    if (resource === "verifications") {
      json(request, response, 200, { items: fixture.sections === "empty" ? [] : verifications() });
      return;
    }
    if (resource === "resolution-report") {
      if (fixture.sections === "empty") {
        json(request, response, 404, problem(url, 404, "RESOLUTION_REPORT_NOT_FOUND", "No ResolutionReport exists for this cycle."));
        return;
      }
      json(request, response, 200, { resource: resolutionReport() });
      return;
    }
  }

  json(request, response, 404, problem(url, 404, "RESOURCE_NOT_FOUND", "Fixture route not found."));
});

server.listen(port, "127.0.0.1", () => {
  process.stdout.write(`CloudOps browser fixture v${fixtureSchemaVersion} listening on 127.0.0.1:${port}\n`);
});

function shutdown() {
  for (const stream of activeStreams) stream.end();
  activeStreams.clear();
  server.close(() => process.exit(0));
}

process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
