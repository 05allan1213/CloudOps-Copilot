import { expect, test, type Page } from "@playwright/test";

import { incidentID, monitorBrowser } from "./support";

const fixtureOrigin = process.env.CLOUDOPS_E2E_FIXTURE_ORIGIN || "http://127.0.0.1:18082";
const incidentRunID = "00000100-0000-4000-8000-000000000001";
const globalRunID = "00000100-0000-4000-8000-000000000002";
const incidentPlanID = "00000101-0000-4000-8000-000000000001";
const globalCardID = "00000102-0000-4000-8000-000000000001";
const executionID = "00000103-0000-4000-8000-000000000001";
const deliveryID = "00000104-0000-4000-8000-000000000001";
const revisionID = "00000071-0000-4000-8000-000000000001";
const contentHash = "a".repeat(64);
const globalHash = "b".repeat(64);
const evidenceHash = "c".repeat(64);
const offlineIconFailures = process.env.CLOUDOPS_E2E_OFFLINE === "1"
  ? [/https:\/\/api\.(?:iconify\.design|unisvg\.com|simplesvg\.com)\/lucide\.json/]
  : [];

const workspaceFixture = {
  operation_plans: [
    {
      id: incidentPlanID,
      run_id: incidentRunID,
      configuration_revision_id: revisionID,
      authority: "high_impact",
      operation_type: "kubernetes.deployment.scale",
      target: {
        cluster_id: "fixture-cluster",
        environment: "test",
        namespace: "checkout",
        workload_kind: "Deployment",
        workload_name: "checkout-api",
      },
      parameters: { replicas: 3 },
      intended_state: { replicas: 3 },
      preconditions: [{ type: "deployment.resource_version", expected_resource_version: "42" }],
      risk: "Incident recovery change remains owned by the Incident lifecycle.",
      verification_intent: { type: "kubernetes.deployment.scale", expected_replicas: 3 },
      content_hash: contentHash,
      status: "authorized",
      expires_at: "2026-07-23T05:00:00.000Z",
      authorization: {
        id: "00000105-0000-4000-8000-000000000001",
        subject_type: "operation_plan",
        subject_id: incidentPlanID,
        authorized_content_hash: contentHash,
        authorized_by: "fixture-owner",
        reason: "Exact Incident remediation reviewed",
        expires_at: "2026-07-23T04:45:00.000Z",
        created_at: "2026-07-23T03:05:00.000Z",
      },
      created_at: "2026-07-23T03:00:00.000Z",
    },
  ],
  action_cards: [
    {
      id: globalCardID,
      run_id: globalRunID,
      authority: "reversible",
      action_type: "local.change_freeze.set",
      target: {
        cluster_id: "fixture-cluster",
        environment: "test",
        namespace: "platform",
        workload_kind: "Deployment",
        workload_name: "payments-api",
      },
      parameters: { enabled: true },
      preconditions: [{ type: "local.change_freeze", expected_enabled: false, expected_version: 1 }],
      risk: "Local reversible freeze does not mutate a Provider workload.",
      content_hash: globalHash,
      status: "proposed",
      expires_at: "2026-07-23T05:00:00.000Z",
      created_at: "2026-07-23T03:10:00.000Z",
    },
  ],
  executions: [
    {
      id: executionID,
      subject_type: "operation_plan",
      subject_id: incidentPlanID,
      run_id: incidentRunID,
      incident_id: incidentID,
      configuration_revision_id: revisionID,
      operation_type: "kubernetes.deployment.scale",
      expected_content_hash: contentHash,
      status: "succeeded",
      attempt: 1,
      external_effect_started_at: "2026-07-23T03:12:00.000Z",
      result: { replicas: 3 },
      created_at: "2026-07-23T03:11:00.000Z",
      started_at: "2026-07-23T03:12:00.000Z",
      completed_at: "2026-07-23T03:13:00.000Z",
      events: [
        {
          id: "00000106-0000-4000-8000-000000000001",
          sequence: 1,
          type: "operation.observed",
          payload: { desired_replicas: 3, available_replicas: 3 },
          content_hash: evidenceHash,
          occurred_at: "2026-07-23T03:13:00.000Z",
        },
      ],
      verification: {
        id: "00000107-0000-4000-8000-000000000001",
        source: "kubernetes",
        status: "passed",
        provider_identity: { cluster_id: "fixture-cluster", resource_version: "43" },
        evidence: { desired_replicas: 3, available_replicas: 3 },
        content_hash: evidenceHash,
        summary: "Current post-effect Deployment observation passed the operation verification.",
        observed_at: "2026-07-23T03:13:00.000Z",
      },
      links: [
        { kind: "agent", label: "Agent Investigation", href: `/agent?investigation=${incidentRunID}` },
        { kind: "incident", label: "Legacy Incident link", href: `/incidents/${incidentID}#recovery-zone` },
        { kind: "verification", label: "Legacy verification link", href: `/devops?operation=${executionID}#verification` },
      ],
    },
  ],
  change_freezes: [
    {
      target: {
        cluster_id: "fixture-cluster",
        environment: "test",
        namespace: "platform",
        workload_kind: "Deployment",
        workload_name: "payments-api",
      },
      enabled: false,
      reason: "No active freeze",
      row_version: 1,
      updated_at: "2026-07-23T03:00:00.000Z",
    },
  ],
  change_candidates: [
    {
      id: "00000108-0000-4000-8000-000000000001",
      incident_id: incidentID,
      run_id: incidentRunID,
      cycle: 3,
      change_ref: "checkout-api@fixture",
      source_type: "github.commit",
      repository: "example/cloudops-config",
      commit_sha: "d".repeat(40),
      gitops_revision: "e".repeat(40),
      image_digest: `sha256:${"f".repeat(64)}`,
      target_path: "apps/checkout-api",
      category: "deployment",
      supporting_evidence: { source: "fixture" },
      content_hash: evidenceHash,
      change_time: "2026-07-23T02:50:00.000Z",
      created_at: "2026-07-23T02:51:00.000Z",
    },
  ],
  deployment_baselines: [
    {
      id: "00000109-0000-4000-8000-000000000001",
      target_identity_hash: "1".repeat(64),
      cluster: "fixture-cluster",
      environment: "test",
      namespace: "checkout",
      workload_kind: "Deployment",
      workload_name: "checkout-api",
      container_name: "checkout-api",
      repository: "example/checkout-api",
      base_branch: "main",
      target_path: "apps/checkout-api",
      source_revision: "2".repeat(40),
      image_digest: `sha256:${"3".repeat(64)}`,
      gitops_revision: "4".repeat(40),
      config_hash: "5".repeat(64),
      verification_policy_version: "recovery/v1",
      verification_hash: "6".repeat(64),
      status: "active",
      row_version: 2,
      verified_at: "2026-07-23T02:00:00.000Z",
    },
  ],
  deliveries: [
    {
      id: deliveryID,
      incident_id: incidentID,
      repository: "example/cloudops-config",
      base_revision: "7".repeat(40),
      head_branch: "incident/checkout-api-recovery",
      commit_sha: "8".repeat(40),
      pull_request_number: 42,
      pull_request_url: "https://github.com/example/cloudops-config/pull/42",
      pull_request_state: "merged",
      ci_status: "passed",
      merged_commit_sha: "9".repeat(40),
      target_revision: "9".repeat(40),
      argo_application: "checkout-api",
      argo_sync_status: "Synced",
      argo_operation_phase: "Succeeded",
      argo_health_status: "Healthy",
      rollout_revision: "12",
      desired_replicas: 3,
      updated_replicas: 3,
      available_replicas: 3,
      unavailable_replicas: 0,
      status: "observed",
      last_observed_at: "2026-07-23T03:13:00.000Z",
    },
  ],
  providers: [
    {
      provider: "kubernetes",
      role: "core",
      enabled: true,
      state: "available",
      detail: "Fixture Kubernetes projection is available.",
      configuration_revision_id: revisionID,
      checked_at: "2026-07-23T03:14:00.000Z",
    },
    {
      provider: "github",
      role: "optional",
      enabled: true,
      state: "available",
      detail: "Fixture GitHub delivery projection is available.",
      configuration_revision_id: revisionID,
      checked_at: "2026-07-23T03:14:00.000Z",
    },
    {
      provider: "argocd",
      role: "optional",
      enabled: true,
      state: "available",
      detail: "Fixture Argo observation is available.",
      configuration_revision_id: revisionID,
      checked_at: "2026-07-23T03:14:00.000Z",
    },
  ],
  collected_at: "2026-07-23T03:15:00.000Z",
};

const investigationsFixture = [
  {
    id: incidentRunID,
    subject_type: "incident",
    incident_id: incidentID,
    status: "completed",
    evidence_count: 4,
    action_cards: [],
    operation_plans: [],
  },
  {
    id: globalRunID,
    subject_type: "consultation",
    consultation_id: "00000110-0000-4000-8000-000000000001",
    status: "completed",
    evidence_count: 1,
    action_cards: [],
    operation_plans: [],
  },
];

async function routeDevOpsFixture(page: Page) {
  await page.route("**/api/v1/devops?**", async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(workspaceFixture) });
  });
  await page.route("**/api/v1/resources?**", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        scope: {
          id: "00000111-0000-4000-8000-000000000001",
          name: "Fixture CloudOps",
          cluster_id: "fixture-cluster",
          environment: "test",
          namespaces: ["demo"],
          active: true,
        },
        provider_state: "available",
        source: {
          provider: "kubernetes",
          cluster_id: "fixture-cluster",
          identity: "kubernetes://fixture-cluster",
          collected_at: "2026-07-23T03:15:00.000Z",
        },
        freshness: { state: "fresh", fresh_until: "2026-07-23T03:16:00.000Z", age_seconds: 0 },
        items: [],
        partial: false,
        truncated: false,
        collected_at: "2026-07-23T03:15:00.000Z",
      }),
    });
  });
  await page.route("**/api/v1/agent/investigations?**", async (route) => {
    await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ items: investigationsFixture }) });
  });
}

test.beforeEach(async ({ page, request }) => {
  await page.clock.setFixedTime(new Date("2026-07-23T04:00:00.000Z"));
  await page.emulateMedia({ colorScheme: "light" });
  await page.setViewportSize({ width: 1440, height: 900 });
  const response = await request.get(`${fixtureOrigin}/fixture/config?reset=1&sections=ready&verification=passed`);
  expect(response.ok()).toBeTruthy();
  await routeDevOpsFixture(page);
});

test("DevOps converges Incident ownership and preserves non-incident commands", async ({ page }) => {
  const browser = monitorBrowser(page, { allowedFailures: offlineIconFailures });
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) mutations.push(`${request.method()} ${request.url()}`);
  });

  await page.goto("/devops", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { level: 1, name: "DevOps Workspace" })).toBeVisible();
  await expect(page.getByTestId("devops-global-queue")).toBeVisible();

  await page.getByTestId("devops-global-queue").getByRole("button", { name: /调整 Deployment 副本/ }).click();
  await expect(page).toHaveURL(new RegExp(`selected=${incidentPlanID}`));
  await expect(page.getByTestId("devops-inspector")).toBeVisible();
  await expect(page.getByTestId("devops-inspector").getByText("Incident 所有", { exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "审批" })).toHaveAttribute("href", `/incidents/${incidentID}#approval`);
  await expect(page.getByRole("link", { name: "交付" })).toHaveAttribute("href", `/incidents/${incidentID}#delivery`);
  await expect(page.getByRole("link", { name: "验证" })).toHaveAttribute("href", `/incidents/${incidentID}#verification`);

  await page.getByRole("button", { name: "打开完整技术详情" }).click();
  await expect(page).toHaveURL(new RegExp(`view=operations.*subject=${incidentPlanID}.*operation=${executionID}`));
  await expect(page.getByTestId("incident-ownership-boundary")).toBeVisible();
  await expect(page.getByTestId("non-incident-actions")).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Delivery Rail" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Verification Matrix" })).toBeVisible();
  await expect(page.getByTestId("devops-verification-matrix")).toContainText("passed");

  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`selected=${incidentPlanID}`));
  await page.goBack();
  await expect(page).not.toHaveURL(/selected=/);

  await page.getByTestId("devops-global-queue").getByRole("button", { name: /设置变更冻结/ }).click();
  await page.getByRole("button", { name: "打开完整技术详情" }).click();
  await expect(page.getByTestId("non-incident-actions")).toBeVisible();
  await expect(page.getByRole("button", { name: "授权精确 Hash" })).toBeVisible();

  expect(mutations).toEqual([]);
  browser.expectClean();
});

test("DevOps restores operation-only Query and renders Delivery Identity", async ({ page }) => {
  const browser = monitorBrowser(page, { allowedFailures: offlineIconFailures });
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (!['GET', 'HEAD', 'OPTIONS'].includes(request.method())) mutations.push(`${request.method()} ${request.url()}`);
  });

  await page.goto(`/devops?operation=${executionID}#verification`, { waitUntil: "domcontentloaded" });
  await expect(page.getByTestId("devops-full-detail")).toBeVisible();
  await expect(page.getByText(incidentPlanID, { exact: true })).toBeVisible();
  await expect(page.getByTestId("devops-verification-matrix")).toBeVisible();

  await page.getByRole("tab", { name: "交付身份" }).click();
  await expect(page).toHaveURL(/view=identity/);
  await expect(page.getByTestId("devops-identity-view")).toBeVisible();
  await expect(page.getByRole("heading", { name: "ChangeCandidate" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "DeploymentBaseline" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Delivery 投影" })).toBeVisible();

  await page.getByRole("tab", { name: "操作与授权" }).click();
  await expect(page.getByTestId("devops-full-detail")).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`operation=${executionID}`));

  expect(mutations).toEqual([]);
  browser.expectClean();
});
