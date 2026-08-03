import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const existingInvestigationID = process.env.CLOUDOPS_REAL_INTEGRATION_EXISTING_INVESTIGATION_ID || "";
const scenarioReason = "Scenario workload is failing because REQUIRED_ENV is missing";

interface AlertArtifact {
  alert_id: string;
  scenario_id: string;
}

interface RunArtifact {
  pre_run: { unread_notifications: number };
}

interface AlertViewResponse {
  id: string;
  incident_links: Array<{ incident_id: string; incident_status: string }>;
  investigations: Array<{ id: string; incident_id: string; status: string }>;
}

interface InvestigationRunResponse {
  id: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  outcome?: "diagnosed" | "insufficient" | "cancelled" | "failed";
  answer?: string;
  model_provider?: string;
  actual_model?: string;
  failure_code?: string;
  failure_summary?: string;
  evidence_citations: Array<{ evidence_id: string; source: string }>;
  steps: Array<{ tool: string; status: string; evidence_id?: string }>;
}

interface SilenceResponse {
  id: string;
  provider_silence_id?: string;
  status: string;
}

interface PriorAttemptLedger {
  capabilities: Array<{
    capability_id: string;
    attempts: Array<{
      api_evidence: Array<{ method: string; path: string; status: number | null }>;
    }>;
  }>;
}

function requiredAlert(): AlertArtifact {
  const artifact = readRunArtifact<AlertArtifact>("objects/alert.json");
  if (!artifact?.alert_id) throw new Error("objects/alert.json with the test-owned alert_id is required");
  return artifact;
}

function priorReadNotificationID(): string {
  const ledger = readRunArtifact<PriorAttemptLedger>("capability-ledger.json");
  const attempts = ledger?.capabilities.find((item) => item.capability_id === "notifications.mark-read")?.attempts ?? [];
  const record = attempts.flatMap((attempt) => attempt.api_evidence).find((item) => (
    item.method === "POST"
    && item.status !== null
    && item.status >= 200
    && item.status < 300
    && /^\/api\/v1\/notifications\/[^/]+\/read$/.test(item.path)
  ));
  return record?.path.match(/^\/api\/v1\/notifications\/([^/]+)\/read$/)?.[1] ?? "";
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return "data" in (body as { data?: T }) ? (body as { data: T }).data : body as T;
}

async function openNotifications(page: Page) {
  await page.getByRole("button", { name: /打开通知收件箱/ }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "通知收件箱" })).toBeVisible();
  await expect(dialog.getByRole("status").filter({ hasText: "实时连接正常" })).toBeVisible();
  return dialog;
}

async function openAlert(page: Page, alertID: string) {
  await page.goto(`/alerts/${alertID}?cluster_id=cloudops-local&namespace=demo`);
  await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/alerts/${alertID}`));
}

async function waitForInvestigationTerminal(
  page: Page,
  investigationID: string,
  initial: InvestigationRunResponse,
): Promise<InvestigationRunResponse> {
  let run = initial;
  for (let attempt = 0; attempt < 75 && ["pending", "running"].includes(run.status); attempt += 1) {
    await page.waitForTimeout(2_000);
    const response = await waitForApiResponse(
      page,
      "GET /api/v1/agent/investigations/{id}",
      () => page.reload(),
    );
    run = await responseData<InvestigationRunResponse>(response);
  }
  expect(run.id).toBe(investigationID);
  expect(["pending", "running"], `Investigation remained ${run.status}`).not.toContain(run.status);
  return run;
}

test.describe.serial("Scenario notifications and Alert lifecycle", () => {
  test("notifications.sse", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "notifications.sse",
      uiAction: "进入 Overview，打开通知收件箱并保持真实 Notification EventSource 连接",
      uiResult: "SSE 连接正常且本轮 Scenario Alert 通知由 Shell 收件箱展示",
    }, async () => {
      await page.goto("/overview");
      await expect(page.getByTestId("overview-command-center")).toBeVisible();
      const dialog = await openNotifications(page);
      await expect(dialog).toContainText(scenarioReason);
      await expect(dialog.locator("li.is-unread").first()).toContainText("alert · firing:");
    });
  });

  test("notifications.mark-read", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const alert = requiredAlert();
    const priorNotificationID = priorReadNotificationID();
    await page.goto("/overview");
    await expect(page.getByTestId("overview-command-center")).toBeVisible();
    const dialog = await openNotifications(page);

    await proveCapability(tracker, testInfo, {
      capabilityID: "notifications.mark-read",
      uiAction: "在通知收件箱将最新一条本轮 Scenario Alert 通知标记为已读",
      uiResult: "刷新并重新打开收件箱后，同一通知仍保持已读且未读计数减少",
      ...(priorNotificationID ? { expectedOperations: ["GET /api/v1/notifications"] } : {}),
    }, async () => {
      if (priorNotificationID) {
        await waitForApiResponse(page, "GET /api/v1/notifications", () => dialog.getByRole("button", { name: "刷新通知" }).click());
        const durableItem = dialog.locator("li:not(.is-unread)")
          .filter({ has: page.locator(`a[href^="/alerts/${alert.alert_id}"]`) })
          .first();
        await expect(durableItem).toBeVisible();
        await expect(durableItem).toContainText(scenarioReason);
        await expect(durableItem.getByRole("button", { name: "标记为已读" })).toHaveCount(0);
        const sourceState = (await durableItem.locator(".source").textContent())?.trim() ?? "";
        recordRunArtifact("objects/notification.json", {
          run_id: runID,
          alert_id: alert.alert_id,
          notification_id: priorNotificationID,
          source_state: sourceState,
        });
        return;
      }
      const item = dialog.locator("li.is-unread").filter({ hasText: scenarioReason }).first();
      await expect(item).toBeVisible();
      const sourceState = (await item.locator(".source").textContent())?.trim() ?? "";
      expect(sourceState).toMatch(/^alert · firing:/);
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/notifications/{id}/read",
        () => item.getByRole("button", { name: "标记为已读" }).click(),
      );
      const match = new URL(response.url()).pathname.match(/\/notifications\/([^/]+)\/read$/);
      const notificationID = match?.[1] ?? "";
      expect(notificationID).not.toBe("");
      const stableItem = dialog.locator("li")
        .filter({ hasText: sourceState })
        .filter({ has: page.locator(`a[href^="/alerts/${alert.alert_id}"]`) })
        .first();
      await expect(stableItem.getByRole("button", { name: "标记为已读" })).toHaveCount(0);

      await page.getByRole("button", { name: "关闭通知收件箱" }).click();
      await page.reload();
      const refreshed = await openNotifications(page);
      const durableItem = refreshed.locator("li").filter({ hasText: sourceState }).first();
      await expect(durableItem).toBeVisible();
      await expect(durableItem.getByRole("button", { name: "标记为已读" })).toHaveCount(0);
      recordRunArtifact("objects/notification.json", {
        run_id: runID,
        alert_id: alert.alert_id,
        notification_id: notificationID,
        source_state: sourceState,
      });
    });
  });

  test("notifications.mark-all-read", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const run = readRunArtifact<RunArtifact>("run.json");
    expect(run?.pre_run.unread_notifications, "pre-run unread notification boundary").toBe(0);
    await page.goto("/overview");
    await expect(page.getByTestId("overview-command-center")).toBeVisible();
    const dialog = await openNotifications(page);

    await proveCapability(tracker, testInfo, {
      capabilityID: "notifications.mark-all-read",
      uiAction: "在运行前未读为 0 且现有未读全属本轮 Scenario 的边界内点击全部已读",
      uiResult: "刷新、重载并重新打开后，MySQL 权威未读数保持为 0",
    }, async () => {
      const unread = dialog.locator("li.is-unread");
      const unreadTexts = await unread.allTextContents();
      expect(unreadTexts.length).toBeGreaterThan(0);
      expect(unreadTexts.every((text) => text.includes(scenarioReason))).toBe(true);
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/notifications/read-all",
        () => dialog.getByRole("button", { name: "全部已读" }).click(),
      );
      const result = await responseData<{ updated: number }>(response);
      expect(result.updated).toBeGreaterThan(0);
      await expect(dialog).toContainText("0 条未读");
      await waitForApiResponse(page, "GET /api/v1/notifications", () => dialog.getByRole("button", { name: "刷新通知" }).click());
      await expect(dialog).toContainText("0 条未读");

      await page.getByRole("button", { name: "关闭通知收件箱" }).click();
      await page.reload();
      const refreshed = await openNotifications(page);
      await expect(refreshed).toContainText("0 条未读");
      recordRunArtifact("objects/notifications-read-all.json", {
        run_id: runID,
        updated: result.updated,
        verified_unread_count: 0,
      });
    });
  });

  test("alerts.acknowledge", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const { alert_id: alertID } = requiredAlert();
    await openAlert(page, alertID);
    await proveCapability(tracker, testInfo, {
      capabilityID: "alerts.acknowledge",
      uiAction: "从本轮 firing Alert 详情点击 Acknowledge 并确认默认审计原因",
      uiResult: "刷新后 Alert lifecycle 显示 durable acknowledgement recurrence 与原因",
    }, async () => {
      await page.getByRole("button", { name: "Acknowledge", exact: true }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog).toContainText("确认已知悉此 Alert");
      await waitForApiResponse(
        page,
        "POST /api/v1/alerts/{id}/acknowledgements",
        () => dialog.getByRole("button", { name: "记录 Acknowledge" }).click(),
      );
      await expect(page.getByText("记录 Acknowledge 已返回", { exact: true })).toBeVisible();
      await page.reload();
      const facets = page.getByLabel("Alert lifecycle facets");
      await expect(facets).toContainText(/recurrence \d+/);
      await expect(facets).toContainText("Owner 已看到并开始 triage");
    });
  });

  test("alerts.silence-create", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const { alert_id: alertID } = requiredAlert();
    await openAlert(page, alertID);
    await proveCapability(tracker, testInfo, {
      capabilityID: "alerts.silence-create",
      uiAction: "从本轮 firing Alert 创建 30 分钟 Provider-backed Silence",
      uiResult: "刷新后 Alertmanager Silence public identity 与 active 状态保持",
    }, async () => {
      await page.getByRole("button", { name: "创建 Silence", exact: true }).click();
      const dialog = page.getByRole("dialog");
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/alerts/{id}/silences",
        () => dialog.getByRole("button", { name: "创建 Silence" }).click(),
      );
      const silence = await responseData<SilenceResponse>(response);
      expect(silence.id).not.toBe("");
      await expect(page.getByText("创建 Silence 已返回", { exact: true })).toBeVisible();
      await page.reload();
      const facets = page.getByLabel("Alert lifecycle facets");
      await expect(facets).toContainText("active");
      await expect(page.getByTestId("app-main")).toContainText(silence.id);
      recordRunArtifact("objects/alert-silence.json", {
        run_id: runID,
        alert_id: alertID,
        silence_id: silence.id,
        provider_silence_id: silence.provider_silence_id ?? "",
        status: "active",
      });
    });
  });

  test("alerts.silence-expire", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const { alert_id: alertID } = requiredAlert();
    const prior = readRunArtifact<{ silence_id: string }>("objects/alert-silence.json");
    expect(prior?.silence_id).toBeTruthy();
    await openAlert(page, alertID);
    await proveCapability(tracker, testInfo, {
      capabilityID: "alerts.silence-expire",
      uiAction: "从本轮 Alert 点击结束当前 Silence 并确认",
      uiResult: "刷新后同一 Silence 在 MySQL 与 Alert detail 中显示 expired 终态",
    }, async () => {
      await page.getByRole("button", { name: "结束 Silence", exact: true }).click();
      const dialog = page.getByRole("dialog");
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/silences/{id}/expire",
        () => dialog.getByRole("button", { name: "结束 Silence" }).click(),
      );
      const silence = await responseData<SilenceResponse>(response);
      expect(silence.id).toBe(prior?.silence_id);
      await page.reload();
      await expect(page.getByLabel("Alert lifecycle facets")).toContainText("expired");
      await expect(page.getByTestId("app-main")).toContainText(prior?.silence_id ?? "");
      recordRunArtifact("objects/alert-silence.json", {
        run_id: runID,
        alert_id: alertID,
        silence_id: silence.id,
        provider_silence_id: silence.provider_silence_id ?? "",
        status: "expired",
      });
    });
  });

  test("alerts.incident-link", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const alert = requiredAlert();
    await openAlert(page, alert.alert_id);
    await proveCapability(tracker, testInfo, {
      capabilityID: "alerts.incident-link",
      uiAction: "从本轮 firing Alert 点击创建 Incident",
      uiResult: "刷新后 Alert 与新建 Incident reciprocal relation 显示同一 public identity",
    }, async () => {
      await page.getByRole("button", { name: "创建 Incident", exact: true }).click();
      const dialog = page.getByRole("dialog");
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/alerts/{id}/incident-links",
        () => dialog.getByRole("button", { name: "创建 Incident" }).click(),
      );
      const updated = await responseData<AlertViewResponse>(response);
      const incident = updated.incident_links.find((link) => link.incident_status !== "closed");
      expect(incident?.incident_id).toBeTruthy();
      const incidentID = incident?.incident_id ?? "";
      await page.reload();
      const link = page.getByRole("link", { name: new RegExp(`^Incident ${incidentID.slice(0, 8)}`) });
      await expect(link).toBeVisible();
      await link.click();
      await expect(page).toHaveURL(new RegExp(`/incidents/${incidentID}`));
      await expect(page.getByTestId("app-main")).toContainText(incidentID);
      await page.goBack();
      await expect(page).toHaveURL(new RegExp(`/alerts/${alert.alert_id}`));
      recordRunArtifact("objects/incident.json", {
        run_id: runID,
        scenario_id: alert.scenario_id,
        alert_id: alert.alert_id,
        incident_id: incidentID,
      });
    });
  });

  test("alerts.investigation-start", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const alert = requiredAlert();
    const incident = readRunArtifact<{ incident_id: string }>("objects/incident.json");
    const previous = readRunArtifact<{ investigation_id: string }>("objects/investigation.json");
    expect(incident?.incident_id).toBeTruthy();
    await openAlert(page, alert.alert_id);
    await proveCapability(tracker, testInfo, {
      capabilityID: "alerts.investigation-start",
      uiAction: existingInvestigationID
        ? "从本轮 Alert 点击进入已 durable 完成的 Investigation，恢复中断后的终态回显验证"
        : "从已关联本轮 Incident 的 Alert 点击启动真实 Investigation",
      expectedOperations: existingInvestigationID
        ? ["GET /api/v1/agent/investigations/{id}"]
        : ["POST /api/v1/alerts/{id}/investigations", "GET /api/v1/agent/investigations/{id}"],
      uiResult: "刷新后 Alert 与 Agent 链接显示 durable run identity 和后端终态",
    }, async () => {
      let investigationID = existingInvestigationID;
      let acceptedStatus = "durable-recovery";
      if (!investigationID) {
        await page.getByRole("button", { name: "启动 Investigation", exact: true }).click();
        const dialog = page.getByRole("dialog");
        const response = await waitForApiResponse(
          page,
          "POST /api/v1/alerts/{id}/investigations",
          () => dialog.getByRole("button", { name: "启动 Investigation" }).click(),
        );
        const updated = await responseData<AlertViewResponse>(response);
        const investigation = updated.investigations.find((item) => (
          item.incident_id === incident?.incident_id && item.id !== previous?.investigation_id
        )) ?? updated.investigations.find((item) => item.id !== previous?.investigation_id);
        expect(investigation?.id).toBeTruthy();
        investigationID = investigation?.id ?? "";
        acceptedStatus = investigation?.status ?? "";
      }
      await page.reload();
      const link = page.getByRole("link", { name: new RegExp(`^Investigation ${investigationID.slice(0, 8)}`) });
      await expect(link).toBeVisible();
      const detailResponse = await waitForApiResponse(
        page,
        "GET /api/v1/agent/investigations/{id}",
        () => link.click(),
      );
      await expect(page).toHaveURL(new RegExp(`investigation=${investigationID}`));
      await expect(page.getByTestId("app-main")).toContainText(investigationID);
      const terminal = await waitForInvestigationTerminal(
        page,
        investigationID,
        await responseData<InvestigationRunResponse>(detailResponse),
      );
      expect(terminal.status, terminal.failure_summary || terminal.failure_code).toBe("completed");
      expect(terminal.outcome).toBe("diagnosed");
      expect(terminal.failure_code ?? "").toBe("");
      expect(terminal.model_provider).toBe("llm");
      expect(terminal.actual_model).toBe("deepseek-v4-flash");
      expect(terminal.answer?.length ?? 0).toBeGreaterThan(80);
      expect(terminal.answer).toContain("cloudops-scenario-fault");
      const evidenceSources = new Set(terminal.evidence_citations.map((item) => item.source));
      expect(terminal.evidence_citations.length).toBeGreaterThanOrEqual(2);
      expect(evidenceSources.size).toBeGreaterThanOrEqual(2);
      for (const citation of terminal.evidence_citations.slice(0, 2)) {
        expect(terminal.answer).toContain(citation.evidence_id);
      }
      expect(terminal.steps.filter((step) => step.status === "completed" && step.evidence_id).length).toBeGreaterThanOrEqual(2);
      await page.reload();
      await expect(page.getByRole("status", { name: "当前调查状态与诊断结论" })).toContainText("调查已完成");
      await expect(page.getByRole("status", { name: "当前调查状态与诊断结论" })).toContainText("结论：已定位根因");
      await expect(page.getByTestId("app-main")).toContainText("cloudops-scenario-fault");
      await expect(page.getByTestId("app-main")).toContainText("REQUIRED_ENV");
      for (const citation of terminal.evidence_citations.slice(0, 2)) {
        await expect(page.getByTestId("app-main")).toContainText(`Evidence ${citation.evidence_id}`);
      }
      recordRunArtifact("objects/investigation.json", {
        run_id: runID,
        scenario_id: alert.scenario_id,
        alert_id: alert.alert_id,
        incident_id: incident?.incident_id,
        investigation_id: investigationID,
        accepted_status: acceptedStatus,
        terminal_status: terminal.status,
        outcome: terminal.outcome,
        model_provider: terminal.model_provider,
        actual_model: terminal.actual_model,
        evidence_ids: terminal.evidence_citations.map((item) => item.evidence_id),
      });
    });
  });
});
