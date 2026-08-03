import { expect, test, type Locator, type Page } from "@playwright/test";

import {
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  type BrowserApiEvidence,
} from "./support";

interface SettingsArtifact {
  pre_run: {
    hash: string;
    scope: { id: string; name: string; cluster_id: string };
  };
  restored: {
    revision_id: string;
    revision_number: number;
    revision_hash: string;
    equivalent_to_pre_run: boolean;
  };
}

interface RetainedArtifacts {
  monitoring: { execution_id: string };
  monitoringCancel: { execution_id: string };
  logsQuery: { query_id: string };
  logsEvidence: { evidence_id: string };
  traceEvidence: { selected_url: string; trace_id: string; evidence_id: string };
  alert: { alert_id: string; scenario_id: string };
  incident: { incident_id: string };
  incidentLifecycle: { verification_id: string; resolution_report_id: string; closed: boolean };
  consultation: { consultation_id: string; quality_snapshot_id: string; cancelled_run_id: string };
  investigation: { investigation_id: string; actual_model: string };
  devops: {
    operation_plan_id: string;
    operation_plan_execution_id: string;
    operation_plan_verification_id: string;
  };
  settings: SettingsArtifact;
}

interface MutationAttempt {
  method: string;
  path: string;
}

const publicRoutes = [
  "/overview",
  "/atlas",
  "/infrastructure",
  "/monitoring",
  "/alerts",
  "/logs",
  "/traces",
  "/agent",
  "/incidents",
  "/devops",
  "/settings",
] as const;

function requiredArtifact<T>(relativePath: string): T {
  const artifact = readRunArtifact<T>(relativePath);
  if (!artifact) throw new Error(`${relativePath} is required for final retained regression`);
  return artifact;
}

function retainedArtifacts(): RetainedArtifacts {
  return {
    monitoring: requiredArtifact("objects/monitoring.json"),
    monitoringCancel: requiredArtifact("objects/monitoring-cancel.json"),
    logsQuery: requiredArtifact("objects/logs-query.json"),
    logsEvidence: requiredArtifact("objects/logs-evidence.json"),
    traceEvidence: requiredArtifact("objects/trace-evidence.json"),
    alert: requiredArtifact("objects/alert.json"),
    incident: requiredArtifact("objects/incident.json"),
    incidentLifecycle: requiredArtifact("objects/incident-lifecycle.json"),
    consultation: requiredArtifact("objects/agent-consultation.json"),
    investigation: requiredArtifact("objects/incident-investigation.json"),
    devops: requiredArtifact("objects/devops-action-cards.json"),
    settings: requiredArtifact("objects/settings.json"),
  };
}

function routeReady(page: Page, route: typeof publicRoutes[number]): Locator {
  switch (route) {
    case "/overview": return page.getByTestId("overview-command-center");
    case "/atlas": return page.getByTestId("atlas-workspace");
    case "/infrastructure": return page.getByTestId("infrastructure-workspace");
    case "/monitoring": return page.getByRole("heading", { name: "监控", level: 1 });
    case "/alerts": return page.getByTestId("alerts-list-route");
    case "/logs": return page.getByRole("heading", { name: "日志分析", level: 1 });
    case "/traces": return page.getByRole("heading", { name: "链路分析", level: 1 });
    case "/agent": return page.getByTestId("agent-workspace");
    case "/incidents": return page.getByRole("heading", { name: "Incident", level: 1 });
    case "/devops": return page.getByTestId("devops-global-queue");
    case "/settings": return page.getByRole("heading", { name: "设置", level: 1 });
  }
}

async function installMutationGuard(page: Page): Promise<MutationAttempt[]> {
  const mutations: MutationAttempt[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const method = request.method();
    if (!["GET", "HEAD", "OPTIONS"].includes(method)) {
      mutations.push({ method, path: new URL(request.url()).pathname });
      await route.abort("blockedbyclient");
      return;
    }
    await route.continue();
  });
  return mutations;
}

function assertHealthyReads(records: BrowserApiEvidence[], context: string) {
  const failures = records.filter((record) => (
    (record.failure && record.failure !== "net::ERR_ABORTED")
    || (record.status !== null && record.status >= 400)
  ));
  expect(failures, `${context} emitted failed API reads`).toEqual([]);
}

async function assertLiveScope(page: Page, settings: SettingsArtifact) {
  await expect(page.locator(".live-state")).toHaveText("Live");
  const selector = page.getByRole("combobox", { name: /活动运行范围：cloudops-local · local/ });
  await expect(selector).toBeVisible();
  await expect(selector).toBeDisabled();
  await expect(selector).toContainText(settings.pre_run.scope.cluster_id);
}

test.describe.serial("final cleanup retained real-browser regression", () => {
  test("all public routes reload in Live Mode with equivalent Scope and Settings", async ({ page }) => {
    const artifacts = retainedArtifacts();
    const tracker = trackBrowserEvidence(page);
    const mutations = await installMutationGuard(page);
    const observations: Array<{ route: string; api_evidence: BrowserApiEvidence[] }> = [];

    for (const route of publicRoutes) {
      const mark = tracker.mark();
      await page.goto(route);
      await expect(routeReady(page, route)).toBeVisible();
      await assertLiveScope(page, artifacts.settings);
      await page.reload();
      await expect(routeReady(page, route)).toBeVisible();
      await assertLiveScope(page, artifacts.settings);
      const apiEvidence = tracker.since(mark);
      assertHealthyReads(apiEvidence, route);
      observations.push({ route, api_evidence: apiEvidence });
    }

    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
    await expect(page.locator(".settings-context-bar")).toContainText(`Revision #${artifacts.settings.restored.revision_number}`);
    await page.getByRole("button", { name: /Revision 历史 20 个只读 Revision/ }).click();
    const restoredRevision = page.getByRole("row").filter({ hasText: `#${artifacts.settings.restored.revision_number}` });
    await expect(restoredRevision).toContainText("Active");
    await expect(restoredRevision).toContainText(artifacts.settings.restored.revision_hash);
    expect(artifacts.settings.restored.revision_hash).toBe(artifacts.settings.pre_run.hash);
    expect(artifacts.settings.restored.equivalent_to_pre_run).toBe(true);
    await page.getByRole("button", { name: /运行范围 1 个 Cluster Scope/ }).click();
    await expect(page.getByTestId("app-main")).toContainText(artifacts.settings.pre_run.scope.name);

    expect(mutations, "final public-route regression attempted an API mutation").toEqual([]);
    recordRunArtifact("final-retained/routes-settings.json", {
      observed_at: new Date().toISOString(),
      live_mode: true,
      active_scope_id: artifacts.settings.pre_run.scope.id,
      active_scope_name: artifacts.settings.pre_run.scope.name,
      active_configuration_revision_id: artifacts.settings.restored.revision_id,
      active_configuration_hash: artifacts.settings.restored.revision_hash,
      equivalent_to_pre_run: artifacts.settings.restored.equivalent_to_pre_run,
      routes: observations,
      mutations,
      console_errors: tracker.consoleErrors,
      page_errors: tracker.pageErrors,
    });
  });

  test("run-scoped durable results survive cleanup without mutations", async ({ page }) => {
    const artifacts = retainedArtifacts();
    const tracker = trackBrowserEvidence(page);
    const mutations = await installMutationGuard(page);
    const retained: Record<string, string> = {};

    await page.goto(`/monitoring?execution=${artifacts.monitoringCancel.execution_id}`);
    await expect(page.getByRole("heading", { name: "监控", level: 1 })).toBeVisible();
    await expect(page.getByText("已取消", { exact: true }).first()).toBeVisible();
    await page.reload();
    await expect(page.getByText("已取消", { exact: true }).first()).toBeVisible();
    retained.monitoring_cancel = artifacts.monitoringCancel.execution_id;

    await page.goto(`/monitoring?execution=${artifacts.monitoring.execution_id}`);
    await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible();
    await page.reload();
    await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible();
    retained.monitoring = artifacts.monitoring.execution_id;

    await page.goto(`/logs?query=${artifacts.logsQuery.query_id}`);
    await expect(page.getByRole("heading", { name: "日志分析", level: 1 })).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(artifacts.logsEvidence.evidence_id);
    retained.logs_evidence = artifacts.logsEvidence.evidence_id;

    await page.goto(artifacts.traceEvidence.selected_url);
    await expect(page.getByRole("heading", { name: "链路分析", level: 1 })).toBeVisible();
    await expect(page.getByTestId("trace-waterfall")).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(artifacts.traceEvidence.trace_id);
    await expect(page.getByTestId("app-main")).toContainText(artifacts.traceEvidence.evidence_id);
    retained.trace_evidence = artifacts.traceEvidence.evidence_id;

    await page.goto(`/alerts/${artifacts.alert.alert_id}`);
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();
    await expect(page.getByText("已恢复 · 严重", { exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(artifacts.alert.scenario_id);
    retained.alert = artifacts.alert.alert_id;

    await page.goto(`/incidents/${artifacts.incident.incident_id}`);
    await expect(page.getByRole("button", { name: "Incident 已关闭" })).toBeDisabled();
    await expect(page.getByText(`VerificationRun ${artifacts.incidentLifecycle.verification_id}`, { exact: true })).toBeVisible();
    await expect(page.getByText(`ResolutionReport ${artifacts.incidentLifecycle.resolution_report_id}`, { exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByText("Incident 已关闭，最终恢复证明与关闭历史保持可审计。")).toBeVisible();
    retained.incident = artifacts.incident.incident_id;

    await page.goto(`/agent?consultation=${artifacts.consultation.consultation_id}`);
    await expect(page.getByTestId("agent-workspace")).toBeVisible();
    await expect(page.getByTestId("agent-workspace")).toContainText(artifacts.consultation.quality_snapshot_id);
    await page.reload();
    await expect(page.locator(".run-status-copy")).toContainText("调查已取消");
    retained.consultation = artifacts.consultation.consultation_id;

    await page.goto(`/agent?investigation=${artifacts.investigation.investigation_id}`);
    await expect(page.getByTestId("agent-workspace")).toBeVisible();
    await expect(page.getByTestId("agent-workspace")).toContainText(artifacts.investigation.investigation_id);
    await expect(page.getByTestId("agent-workspace")).toContainText("调查已完成 · 结论：已诊断");
    await page.reload();
    await expect(page.getByTestId("agent-workspace")).toContainText(artifacts.investigation.investigation_id);
    retained.investigation = artifacts.investigation.investigation_id;

    await page.goto(`/devops?view=operations&subject=${artifacts.devops.operation_plan_id}&operation=${artifacts.devops.operation_plan_execution_id}`);
    await expect(page.getByTestId("devops-full-detail")).toBeVisible();
    const matrix = page.getByTestId("devops-verification-matrix");
    await expect(matrix).toContainText(artifacts.devops.operation_plan_verification_id);
    await expect(matrix).toContainText("验证通过");
    await page.reload();
    await expect(page.getByTestId("devops-full-detail")).toContainText(artifacts.devops.operation_plan_execution_id);
    retained.devops_execution = artifacts.devops.operation_plan_execution_id;

    await assertLiveScope(page, artifacts.settings);
    const reads = tracker.since(0);
    assertHealthyReads(reads, "retained durable results");
    expect(mutations, "final retained regression attempted an API mutation").toEqual([]);
    recordRunArtifact("final-retained/durable-results.json", {
      observed_at: new Date().toISOString(),
      retained,
      api_evidence: reads,
      mutations,
      console_errors: tracker.consoleErrors,
      page_errors: tracker.pageErrors,
    });
  });
});
