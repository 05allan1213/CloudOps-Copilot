import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const scenarioID = process.env.CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID || "";
const alertID = process.env.CLOUDOPS_REAL_INTEGRATION_ALERT_ID || "";
const decision = process.env.CLOUDOPS_REAL_INTEGRATION_REMEDIATION_DECISION as "approved" | "rejected" | undefined;
const shouldCancel = process.env.CLOUDOPS_REAL_INTEGRATION_CANCEL_INVESTIGATION === "true";
const forceNewInvestigation = process.env.CLOUDOPS_REAL_INTEGRATION_FORCE_NEW_INVESTIGATION === "true";
const scenarioReason = "Scenario workload is failing because REQUIRED_ENV is missing";
const artifactPath = `objects/closure-${decision || "undecided"}-${scenarioID || "scenario-required"}.json`;

interface AlertView {
  id: string;
  incident_links: Array<{ incident_id: string; incident_status: string }>;
  investigations: Array<{ id: string; incident_id: string; status: string }>;
}

interface AlertDetail {
  alert: AlertView;
}

interface AgentRun {
  id: string;
  scenario_id?: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  outcome?: string;
  actual_model?: string;
  created_at: string;
  updated_at: string;
}

interface RemediationDecision {
  id: string;
  decision: "approved" | "rejected";
  reason: string;
}

interface RemediationPlan {
  id: string;
  status: string;
  version: number;
  canonical_plan_hash: string;
  operation_type: string;
  created_by_agent_run_id: string;
  decision?: RemediationDecision;
}

interface Collection<T> {
  items: T[];
}

interface ClosureArtifact {
  run_id: string;
  scenario_id: string;
  alert_id: string;
  incident_id?: string;
  cancelled_investigation_id?: string;
  cancelled_status?: string;
  diagnostic_investigation_id?: string;
  diagnostic_status?: string;
  diagnostic_outcome?: string;
  diagnostic_model?: string;
  plan_id?: string;
  plan_hash?: string;
  plan_operation?: string;
  decision?: "approved" | "rejected";
  decision_id?: string;
  decision_reason?: string;
}

function requiredScenarioIdentity() {
  expect(runID, "CLOUDOPS_REAL_INTEGRATION_RUN_ID").not.toBe("");
  expect(scenarioID, "CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID").toMatch(/^scenario-/);
  expect(alertID, "CLOUDOPS_REAL_INTEGRATION_ALERT_ID").toMatch(/^[0-9a-f-]{36}$/);
}

function requiredDecisionIdentity() {
  requiredScenarioIdentity();
  expect(["approved", "rejected"], "CLOUDOPS_REAL_INTEGRATION_REMEDIATION_DECISION").toContain(decision);
}

function currentArtifact(): ClosureArtifact {
  return readRunArtifact<ClosureArtifact>(artifactPath) ?? {
    run_id: runID,
    scenario_id: scenarioID,
    alert_id: alertID,
  };
}

function saveArtifact(patch: Partial<ClosureArtifact>) {
  recordRunArtifact(artifactPath, { ...currentArtifact(), ...patch });
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return body && typeof body === "object" && "data" in body ? body.data : body as T;
}

async function reloadOrGoto(page: Page, target: string) {
  const current = new URL(page.url());
  const next = new URL(target, "http://cloudops.local");
  if (current.pathname === next.pathname && current.search === next.search) {
    if (current.hash !== next.hash) {
      await page.goto("about:blank");
      return page.goto(target);
    }
    return page.reload();
  }
  return page.goto(target);
}

async function readAlert(page: Page): Promise<AlertView> {
  const target = `/alerts/${alertID}?cluster_id=cloudops-local&namespace=demo`;
  const response = await waitForApiResponse(
    page,
    "GET /api/v1/alerts/{id}",
    () => reloadOrGoto(page, target),
  );
  const value = await responseData<AlertView | AlertDetail>(response);
  await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
  return "alert" in value ? value.alert : value;
}

async function ensureIncident(page: Page): Promise<string> {
  requiredScenarioIdentity();
  const artifact = currentArtifact();
  await readAlert(page);
  if (artifact.incident_id) {
    await expect(page.getByRole("link", { name: new RegExp(`^Incident ${artifact.incident_id.slice(0, 8)}`) })).toBeVisible();
    return artifact.incident_id;
  }

  const existingLink = page.getByRole("link", { name: /^Incident [0-9a-f]{8}/ }).first();
  if (await existingLink.isVisible().catch(() => false)) {
    const href = await existingLink.getAttribute("href");
    const existingID = href?.match(/\/incidents\/([0-9a-f-]{36})/)?.[1] ?? "";
    expect(existingID).toMatch(/^[0-9a-f-]{36}$/);
    saveArtifact({ incident_id: existingID });
    return existingID;
  }

  await page.getByRole("button", { name: "创建 Incident", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/alerts/{id}/incident-links",
    () => dialog.getByRole("button", { name: "创建 Incident", exact: true }).click(),
  );
  const alert = await responseData<AlertView>(response);
  const link = alert.incident_links.find((item) => item.incident_status !== "closed");
  const incidentID = link?.incident_id ?? "";
  expect(incidentID).toMatch(/^[0-9a-f-]{36}$/);
  saveArtifact({ incident_id: incidentID });
  await page.reload();
  await expect(page.getByRole("link", { name: new RegExp(`^Incident ${incidentID.slice(0, 8)}`) })).toBeVisible();
  return incidentID;
}

async function openAgentInvestigation(page: Page, investigationID: string) {
  await waitForApiResponse(
    page,
    "GET /api/v1/agent/investigations/{id}",
    () => reloadOrGoto(page, `/agent?investigation=${investigationID}`),
  );
  await expect(page.getByTestId("agent-workspace")).toBeVisible();
  await expect(page.getByText(investigationID, { exact: true })).toBeVisible();
}

async function readIncidentInvestigations(page: Page, incidentID: string): Promise<AgentRun[]> {
  const response = await waitForApiResponse(
    page,
    "GET /api/v1/incidents/{id}/investigations",
    () => reloadOrGoto(page, `/incidents/${incidentID}#investigations`),
  );
  const collection = await responseData<Collection<AgentRun>>(response);
  await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
  return collection.items;
}

async function ensureDiagnosticInvestigation(page: Page, incidentID: string): Promise<string> {
  const artifact = currentArtifact();
  if (
    !forceNewInvestigation
    && artifact.diagnostic_investigation_id
    && (
      ["pending", "running"].includes(artifact.diagnostic_status ?? "")
      || (artifact.diagnostic_status === "completed" && artifact.diagnostic_outcome === "diagnosed")
    )
  ) return artifact.diagnostic_investigation_id;

  const runs = await readIncidentInvestigations(page, incidentID);
  const existing = forceNewInvestigation
    ? undefined
    : runs
      .filter((run) => run.id !== artifact.cancelled_investigation_id)
      .filter((run) => !run.scenario_id || run.scenario_id === scenarioID)
      .filter((run) => (
        ["pending", "running"].includes(run.status)
        || (run.status === "completed" && run.outcome === "diagnosed")
      ))
      .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];
  if (existing) {
    saveArtifact({
      diagnostic_investigation_id: existing.id,
      diagnostic_status: undefined,
      diagnostic_outcome: undefined,
      diagnostic_model: undefined,
    });
    return existing.id;
  }

  await page.getByRole("button", { name: "发起调查", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "发起有界 Agent 调查" })).toBeVisible();
  await dialog.getByRole("textbox", { name: "命令原因" }).fill(
    `${runID} ${scenarioID} retry after reviewing prior bounded Investigation evidence`,
  );
  await waitForApiResponse(
    page,
    "POST /api/v1/incidents/{id}/investigations",
    () => dialog.getByRole("button", { name: "发起调查", exact: true }).click(),
  );

  for (let attempt = 0; attempt < 30; attempt += 1) {
    const created = (await readIncidentInvestigations(page, incidentID))
      .filter((run) => run.id !== artifact.cancelled_investigation_id)
      .filter((run) => !run.scenario_id || run.scenario_id === scenarioID)
      .filter((run) => !["failed", "cancelled"].includes(run.status))
      .sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];
    if (created) {
      saveArtifact({
        diagnostic_investigation_id: created.id,
        diagnostic_status: undefined,
        diagnostic_outcome: undefined,
        diagnostic_model: undefined,
      });
      return created.id;
    }
    await page.waitForTimeout(2_000);
  }
  throw new Error("Incident Investigation was accepted but no durable run appeared");
}

async function waitForDiagnosticTerminal(page: Page, incidentID: string, investigationID: string): Promise<AgentRun> {
  for (let attempt = 0; attempt < 180; attempt += 1) {
    const runs = await readIncidentInvestigations(page, incidentID);
    const run = runs.find((item) => item.id === investigationID);
    if (run && !["pending", "running"].includes(run.status)) {
      saveArtifact({
        diagnostic_status: run.status,
        diagnostic_outcome: run.outcome ?? "",
        diagnostic_model: run.actual_model ?? "",
      });
      return run;
    }
    await page.waitForTimeout(2_000);
  }
  throw new Error(`Investigation ${investigationID} did not reach a terminal state within six minutes`);
}

async function readPlans(page: Page, incidentID: string): Promise<RemediationPlan[]> {
  const response = await waitForApiResponse(
    page,
    "GET /api/v1/incidents/{id}/remediation-plans",
    () => reloadOrGoto(page, `/incidents/${incidentID}#approval`),
  );
  const collection = await responseData<Collection<RemediationPlan>>(response);
  await expect(page.getByRole("heading", { name: "Remediation Plan & Approval" })).toBeVisible();
  return collection.items;
}

async function waitForPlan(page: Page, incidentID: string, investigationID: string): Promise<RemediationPlan> {
  const artifact = currentArtifact();
  for (let attempt = 0; attempt < 180; attempt += 1) {
    const plans = await readPlans(page, incidentID);
    const plan = plans.find((item) => item.id === artifact.plan_id)
      ?? plans.find((item) => item.created_by_agent_run_id === investigationID);
    if (plan) {
      expect(plan.operation_type).toBe("restore_required_env");
      saveArtifact({
        plan_id: plan.id,
        plan_hash: plan.canonical_plan_hash,
        plan_operation: plan.operation_type,
      });
      return plan;
    }
    await page.waitForTimeout(2_000);
  }
  throw new Error(`Investigation ${investigationID} did not produce an immutable Plan within six minutes`);
}

test("cancel a current-Scenario Investigation from the Agent UI", async ({ page }, testInfo) => {
  test.skip(!shouldCancel, "This Scenario is dedicated to an immutable Plan decision");
  const tracker = trackBrowserEvidence(page);
  await ensureIncident(page);
  const prior = currentArtifact();

  await proveCapability(tracker, testInfo, {
    capabilityID: "agent.investigation-cancel",
    uiAction: prior.cancelled_status === "cancelled"
      ? "直接重新进入本轮 cancelled Investigation，读取 durable Agent 终态"
      : "从本轮 Alert 点击启动 Investigation，立即进入 Agent 工作台并点击取消当前 Agent 运行",
    expectedOperations: prior.cancelled_status === "cancelled"
      ? ["GET /api/v1/agent/investigations/{id}"]
      : ["POST /api/v1/agent/investigations/{id}/cancel"],
    uiResult: "刷新并直接重新进入后，同一 run 保持 cancelled，已持久化对象未被删除",
  }, async () => {
    let investigationID = prior.cancelled_investigation_id ?? "";
    if (!investigationID) {
      const before = await readAlert(page);
      const existingIDs = new Set(before.investigations.map((item) => item.id));
      await page.getByRole("button", { name: "启动 Investigation", exact: true }).click();
      const dialog = page.getByRole("dialog");
      const startResponse = await waitForApiResponse(
        page,
        "POST /api/v1/alerts/{id}/investigations",
        () => dialog.getByRole("button", { name: "启动 Investigation", exact: true }).click(),
      );
      const startValue = await responseData<AlertView | AlertDetail>(startResponse);
      const startedAlert = "alert" in startValue ? startValue.alert : startValue;
      const responseCandidates = startedAlert.investigations.filter((item) => !existingIDs.has(item.id));
      if (responseCandidates.length === 1) investigationID = responseCandidates[0].id;
      for (let attempt = 0; attempt < 20; attempt += 1) {
        if (investigationID) break;
        const projected = await readAlert(page);
        const candidates = projected.investigations.filter((item) => !existingIDs.has(item.id));
        if (candidates.length === 1) {
          investigationID = candidates[0].id;
          break;
        }
        await page.waitForTimeout(100);
      }
      expect(investigationID).toMatch(/^[0-9a-f-]{36}$/);
      saveArtifact({ cancelled_investigation_id: investigationID });
    }

    await openAgentInvestigation(page, investigationID);
    if (prior.cancelled_status !== "cancelled") {
      const cancelButton = page.getByRole("button", { name: "取消当前 Agent 运行" });
      await expect(cancelButton).toBeVisible({ timeout: 30_000 });
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/agent/investigations/{id}/cancel",
        () => cancelButton.click(),
      );
      const cancelled = await responseData<AgentRun>(response);
      expect(cancelled.id).toBe(investigationID);
      expect(cancelled.status).toBe("cancelled");
      saveArtifact({ cancelled_investigation_id: investigationID, cancelled_status: cancelled.status });
    }

    await openAgentInvestigation(page, investigationID);
    await expect(page.locator(".run-status-copy")).toContainText("调查已取消");
    await page.reload();
    await expect(page.locator(".run-status-copy")).toContainText("调查已取消");
  });
});

test("make an exact durable decision on a current-Scenario immutable Plan", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  requiredDecisionIdentity();
  const incidentID = await ensureIncident(page);
  const capabilityID = decision === "approved" ? "incidents.remediation-approve" : "incidents.remediation-reject";
  const prior = currentArtifact();

  await proveCapability(tracker, testInfo, {
    capabilityID,
    uiAction: prior.decision === decision
      ? `直接重新进入本轮 Incident，读取 immutable Plan 的 durable ${decision} Decision`
      : `从本轮 Incident 发起完整 Investigation，等待服务端生成 immutable Plan 并点击 ${decision === "approved" ? "Approve Exact Plan" : "Reject Plan"}`,
    expectedOperations: prior.decision === decision
      ? ["GET /api/v1/incidents/{id}/remediation-plans"]
      : ["POST /api/v1/remediation-plans/{id}/decisions"],
    uiResult: `刷新并直接重新进入后，同一 Plan、canonical hash、Decision ID、reason 与 ${decision} 终态保持权威回显`,
  }, async () => {
    const investigationID = await ensureDiagnosticInvestigation(page, incidentID);
    const terminal = await waitForDiagnosticTerminal(page, incidentID, investigationID);
    expect(terminal.status).toBe("completed");
    expect(terminal.outcome).toBe("diagnosed");
    expect(terminal.actual_model).toBe("deepseek-v4-flash");

    let plan = await waitForPlan(page, incidentID, investigationID);
    const reason = prior.decision_reason
      ?? `${runID} ${scenarioID} exact ${decision} decision for confirmed REQUIRED_ENV regression`;
    if (prior.decision !== decision) {
      const panel = page.locator("#remediation-plans article.approval-panel").filter({ hasText: plan.id });
      await expect(panel).toBeVisible();
      const openButton = panel.getByRole("button", {
        name: decision === "approved" ? "Approve Exact Plan" : "Reject Plan",
        exact: true,
      });
      await expect(openButton).toBeEnabled();
      await openButton.click();
      const dialog = page.getByRole("dialog");
      await dialog.getByRole("textbox", { name: "Decision Reason" }).fill(reason);
      await waitForApiResponse(
        page,
        "POST /api/v1/remediation-plans/{id}/decisions",
        () => dialog.getByRole("button", {
          name: decision === "approved" ? "Submit Approval" : "Submit Rejection",
          exact: true,
        }).click(),
      );
    }

    for (let attempt = 0; attempt < 60; attempt += 1) {
      const plans = await readPlans(page, incidentID);
      const refreshed = plans.find((item) => item.id === plan.id);
      if (refreshed && refreshed.decision?.decision === decision) {
        plan = refreshed;
        break;
      }
      await page.waitForTimeout(2_000);
    }
    expect(plan.decision?.decision).toBe(decision);
    expect(plan.decision?.reason).toBe(reason);
    expect(plan.canonical_plan_hash).toBe(currentArtifact().plan_hash);
    saveArtifact({
      decision,
      decision_id: plan.decision?.id,
      decision_reason: reason,
    });

    const panel = page.locator("#remediation-plans article.approval-panel").filter({ hasText: plan.id });
    await expect(panel).toContainText(reason);
    await expect(panel.getByRole("heading", { name: decision, exact: true })).toBeVisible();
    await page.reload();
    await expect(page.locator("#remediation-plans article.approval-panel").filter({ hasText: plan.id })).toContainText(reason);
  });
});
