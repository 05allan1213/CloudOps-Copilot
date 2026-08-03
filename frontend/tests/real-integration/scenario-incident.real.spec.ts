import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const scenarioReason = "Scenario workload is failing because REQUIRED_ENV is missing";

interface IncidentArtifact {
  incident_id: string;
}

interface IncidentLifecycleArtifact {
  run_id: string;
  incident_id: string;
  recovery_decision_id?: string;
  recovery_reason?: string;
  verification_id?: string;
  resolution_report_id?: string;
  closed?: boolean;
}

interface IncidentInvestigation {
  id: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  objective: string;
  outcome?: string;
  failure_code?: string;
  failure_summary?: string;
  model_provider?: string;
  actual_model?: string;
  prompt_version: string;
  used_steps: number;
  max_steps: number;
  created_at: string;
  updated_at: string;
}

interface CollectionResponse<T> {
  items: T[];
}

function requiredIncidentID(): string {
  const artifact = readRunArtifact<IncidentArtifact>("objects/incident.json");
  if (!artifact?.incident_id) throw new Error("objects/incident.json with the test-owned incident_id is required");
  return artifact.incident_id;
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return "data" in (body as { data?: T }) ? (body as { data: T }).data : body as T;
}

async function openIncident(page: Page, incidentID: string) {
  await page.goto(`/incidents/${incidentID}`);
  await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
  await expect(page).toHaveURL(new RegExp(`/incidents/${incidentID}`));
}

async function reloadInvestigations(page: Page): Promise<IncidentInvestigation[]> {
  const response = await waitForApiResponse(
    page,
    "GET /api/v1/incidents/{id}/investigations",
    () => page.reload(),
  );
  return (await responseData<CollectionResponse<IncidentInvestigation>>(response)).items;
}

function lifecycleArtifact(): IncidentLifecycleArtifact {
  return readRunArtifact<IncidentLifecycleArtifact>("objects/incident-lifecycle.json") ?? {
    run_id: runID,
    incident_id: requiredIncidentID(),
  };
}

function saveLifecycleArtifact(update: Partial<IncidentLifecycleArtifact>) {
  recordRunArtifact("objects/incident-lifecycle.json", { ...lifecycleArtifact(), ...update });
}

async function reloadIncidentUI(page: Page) {
  await waitForApiResponse(
    page,
    "GET /api/v1/incidents/{id}",
    () => page.reload(),
  );
  await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
}

async function waitForRecoveryUI(page: Page, attempts = 150): Promise<{ verificationID: string; reportID: string }> {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    await reloadIncidentUI(page);
    const closeButton = page.getByRole("button", { name: "关闭 Incident", exact: true });
    if (await closeButton.isEnabled().catch(() => false)) {
      const verificationText = await page.locator("#verifications").getByText(/^VerificationRun [0-9a-f-]+$/).first().textContent();
      const reportText = await page.locator("#resolution-report").getByText(/^ResolutionReport [0-9a-f-]+$/).first().textContent();
      const verificationID = verificationText?.replace("VerificationRun ", "") ?? "";
      const reportID = reportText?.replace("ResolutionReport ", "") ?? "";
      expect(verificationID).toMatch(/^[0-9a-f-]{36}$/);
      expect(reportID).toMatch(/^[0-9a-f-]{36}$/);
      return { verificationID, reportID };
    }
    if (await page.getByText("返回 Investigate", { exact: true }).isVisible().catch(() => false)) {
      throw new Error("Recovery Verification returned to investigating");
    }
    await page.waitForTimeout(2_000);
  }
  throw new Error("Incident did not expose a passed Verification and ResolutionReport");
}

async function waitForClosedUI(page: Page, attempts = 30) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    await reloadIncidentUI(page);
    if (await page.getByRole("button", { name: "Incident 已关闭" }).isVisible().catch(() => false)) return;
    await page.waitForTimeout(1_000);
  }
  throw new Error("Incident did not reach closed UI state");
}

test.describe.serial("Scenario Incident lifecycle", () => {
  test("incident list and durable relation projections", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();

    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.read-relations",
      uiAction: "在 Incident 列表筛选本轮 Scenario，打开 Inspector 与完整详情，读取 Alert relation 和 persisted Signals",
      uiResult: "刷新后同一 Incident、Scenario resource、关联 Alert 与持久化 Signal 保持权威回显",
    }, async () => {
      await page.goto("/incidents");
      await expect(page.getByRole("heading", { name: "Incident", level: 1 })).toBeVisible();
      await page.getByRole("textbox", { name: "搜索服务" }).fill("cloudops-scenario-fault");
      await waitForApiResponse(
        page,
        "GET /api/v1/incidents",
        () => page.getByRole("button", { name: "应用", exact: true }).click(),
      );
      const rowTitle = page.getByTestId("incident-row-summary").filter({ hasText: scenarioReason }).first();
      await expect(rowTitle).toBeVisible();
      await rowTitle.locator("xpath=ancestor::button[1]").click();
      const detailLink = page.getByRole("link", { name: "打开完整 Incident 详情" });
      await expect(detailLink).toHaveAttribute("href", new RegExp(`/incidents/${incidentID}`));
      await detailLink.click();
      await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
      await expect(page.getByRole("navigation", { name: "同一调查的相关资源" })).toBeVisible();
      await expect(page.getByRole("heading", { name: "关联 Alert", level: 3 })).toBeVisible();
      await expect(page.getByRole("link", { name: scenarioReason, exact: true })).toHaveAttribute(
        "href",
        /\/alerts\/[0-9a-f-]+/,
      );
      await expect(page.getByRole("heading", { name: "Persisted Signals", level: 3 })).toBeVisible();
      await expect(page.getByRole("region", { name: "Persisted Signals" })).toContainText(scenarioReason);
    });
  });

  test("incident Timeline and realtime stream", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.timeline-sse",
      uiAction: "重新进入本轮 Incident，建立 EventStream 并读取当前 Cycle Timeline",
      uiResult: "Incident realtime 状态变为已连接，Timeline 从后端投影展示并在刷新后保持",
    }, async () => {
      await openIncident(page, incidentID);
      await expect(page.getByRole("status").filter({ hasText: /实时连接已建立|Realtime connection established|Live/ }))
        .toBeVisible({ timeout: 30_000 });
      const timeline = page.getByRole("region", { name: "活动时间线" });
      await expect(timeline).toBeVisible();
      await expect(timeline.locator("li").first()).toBeVisible();
    });
  });

  test("incident Evidence and Investigation projections", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.evidence-investigations",
      uiAction: "重新进入本轮 Incident，读取当前 Cycle Evidence 与 Investigation 集合",
      uiResult: "Evidence 和 Investigation 空/有数据状态均由后端投影，并在直接重进后稳定展示",
    }, async () => {
      await openIncident(page, incidentID);
      await expect(page.getByRole("heading", { name: "Evidence 证据", level: 3 })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Investigation 记录", level: 3 })).toBeVisible();
    });
  });

  test("incident lifecycle projections", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.lifecycle-projections",
      uiAction: "重新进入本轮 Incident，读取 Decision、Remediation、Delivery、Verification 与 Resolution 投影",
      uiResult: "全部生命周期分区由对应后端合同返回，并在刷新后保持与 Incident 当前阶段一致",
    }, async () => {
      await openIncident(page, incidentID);
      await expect(page.getByRole("heading", { name: "Recovery Decision", level: 3 })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Remediation Plan & Approval", level: 3 })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Delivery", level: 2 })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Verification", level: 2 })).toBeVisible();
      await expect(page.getByRole("heading", { name: "Resolution", level: 2 })).toBeVisible();
    });
  });

  test("incident Investigation starts from the original UI control", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    const prior = readRunArtifact<{ investigation_id: string }>("objects/incident-investigation.json");
    await openIncident(page, incidentID);

    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.investigation-start",
      uiAction: "在本轮 Incident Agent 调查区点击发起调查，并在精确版本确认窗口提交",
      uiResult: "Incident Investigation durable ID、DeepSeek 终态与 Agent 入口在页面刷新和重进后回显",
      ...(prior?.investigation_id ? {
        expectedOperations: ["GET /api/v1/incidents/{id}/investigations"],
      } : {}),
    }, async () => {
      let investigationID = prior?.investigation_id ?? "";
      let investigation: IncidentInvestigation | undefined;

      if (!investigationID) {
        await page.getByRole("button", { name: "发起调查", exact: true }).click();
        const dialog = page.getByRole("dialog");
        await expect(dialog.getByRole("heading", { name: "发起有界 Agent 调查" })).toBeVisible();
        await waitForApiResponse(
          page,
          "POST /api/v1/incidents/{id}/investigations",
          () => dialog.getByRole("button", { name: "发起调查", exact: true }).click(),
        );
        await expect(page.getByRole("status").filter({ hasText: "命令已持久化并进入异步执行" })).toBeVisible();
      }

      for (let attempt = 0; attempt < 90; attempt += 1) {
        const investigations = await reloadInvestigations(page);
        investigation = investigationID
          ? investigations.find((item) => item.id === investigationID)
          : [...investigations].sort((left, right) => Date.parse(right.created_at) - Date.parse(left.created_at))[0];
        if (investigation && !investigationID) investigationID = investigation.id;
        if (investigation && !["pending", "running"].includes(investigation.status)) break;
        await page.waitForTimeout(2_000);
      }

      expect(investigationID).not.toBe("");
      expect(investigation).toBeTruthy();
      expect(investigation?.status).toBe("completed");
      expect(investigation?.actual_model).toBe("deepseek-v4-flash");
      expect(investigation?.prompt_version).toBe("agent-workspace/v2");
      const investigationRegion = page.getByRole("region", { name: "Investigation 记录" });
      await expect(investigationRegion).toContainText("deepseek-v4-flash");
      await expect(investigationRegion).toContainText(investigation?.outcome ?? "diagnosed");
      await expect(investigationRegion.getByRole("link", { name: investigation?.objective || "Incident 调查" })).toHaveAttribute(
        "href",
        new RegExp(`investigation=${investigationID}`),
      );

      recordRunArtifact("objects/incident-investigation.json", {
        run_id: runID,
        incident_id: incidentID,
        investigation_id: investigationID,
        status: investigation?.status,
        outcome: investigation?.outcome ?? "",
        model_provider: investigation?.model_provider ?? "",
        actual_model: investigation?.actual_model ?? "",
        prompt_version: investigation?.prompt_version ?? "",
      });
    });
  });

  test("no-change Recovery Decision produces durable Verification and ResolutionReport", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    const prior = lifecycleArtifact();
    const recoveryReason = prior.recovery_reason || `${runID} verified Scenario provider recovery without claiming a remediation delivery`;
    await openIncident(page, incidentID);
    const alreadyRecovered = await page.locator("#resolution-report").getByText(/^ResolutionReport [0-9a-f-]+$/).count() > 0;

    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.recovery-decision",
      uiAction: "在本轮 Incident 的 Recovery Decision 区点击进入恢复验证，并确认精确 Incident 版本与 no-change 原因",
      uiResult: "刷新后 Recovery Decision、passed Verification、共同稳定窗口与 immutable ResolutionReport 使用同一 durable provenance",
      ...(prior.verification_id || alreadyRecovered ? {
        expectedOperations: ["GET /api/v1/incidents/{id}"],
      } : {}),
    }, async () => {
      if (!alreadyRecovered) {
        await page.getByRole("button", { name: "进入恢复验证", exact: true }).click();
        const dialog = page.getByRole("dialog");
        await expect(dialog.getByRole("heading", { name: "进入恢复验证" })).toBeVisible();
        await dialog.getByRole("textbox", { name: "命令原因" }).fill(recoveryReason);
        await waitForApiResponse(
          page,
          "POST /api/v1/incidents/{id}/decision",
          () => dialog.getByRole("button", { name: "进入 Verification" }).click(),
        );
        await expect(page.getByRole("status").filter({ hasText: "命令已持久化并进入异步执行" })).toBeVisible();
      }

      const { verificationID, reportID } = await waitForRecoveryUI(page);
      await expect(page.getByRole("region", { name: "Recovery Decision" }).getByText(recoveryReason, { exact: true }).first()).toBeVisible();
      await expect(page.getByText(`VerificationRun ${verificationID}`, { exact: true })).toBeVisible();
      await expect(page.getByText(`ResolutionReport ${reportID}`, { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "关闭 Incident" })).toBeEnabled();

      saveLifecycleArtifact({
        recovery_reason: recoveryReason,
        verification_id: verificationID,
        resolution_report_id: reportID,
      });
    });
  });

  test("resolved Incident closes and survives refresh, re-entry, and history navigation", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const incidentID = requiredIncidentID();
    const prior = lifecycleArtifact();
    const closeReason = `${runID} close after persisted recovery Verification and ResolutionReport`;
    await openIncident(page, incidentID);
    const alreadyClosed = await page.getByRole("button", { name: "Incident 已关闭" }).isVisible().catch(() => false);

    await proveCapability(tracker, testInfo, {
      capabilityID: "incidents.close",
      uiAction: "在 Resolution 区点击关闭本轮 Incident，并确认精确 Incident 版本与恢复证明",
      uiResult: "刷新、重新进入和浏览器 Back/Forward 后仍展示 closed 终态与 retained ResolutionReport",
      ...(prior.closed || alreadyClosed ? {
        expectedOperations: ["GET /api/v1/incidents/{id}"],
      } : {}),
    }, async () => {
      if (!alreadyClosed) {
        await expect(page.getByRole("button", { name: "关闭 Incident", exact: true })).toBeEnabled();
        await page.getByRole("button", { name: "关闭 Incident", exact: true }).click();
        const dialog = page.getByRole("dialog");
        await expect(dialog.getByRole("heading", { name: "关闭 Incident" })).toBeVisible();
        await dialog.getByRole("textbox", { name: "命令原因" }).fill(closeReason);
        await waitForApiResponse(
          page,
          "POST /api/v1/incidents/{id}/close",
          () => dialog.getByRole("button", { name: "关闭 Incident", exact: true }).click(),
        );
        await waitForClosedUI(page);
      }

      await expect(page.getByRole("button", { name: "Incident 已关闭" })).toBeDisabled();
      await expect(page.getByText("Incident 已关闭，最终恢复证明与关闭历史保持可审计。")).toBeVisible();
      await expect(page.getByText(`ResolutionReport ${prior.resolution_report_id}`, { exact: true })).toBeVisible();

      await page.goto("/incidents");
      await expect(page.getByRole("heading", { name: "Incident", level: 1 })).toBeVisible();
      await page.goBack();
      await expect(page.getByRole("heading", { name: scenarioReason, level: 1 })).toBeVisible();
      await expect(page.getByRole("button", { name: "Incident 已关闭" })).toBeDisabled();
      await page.goForward();
      await expect(page.getByRole("heading", { name: "Incident", level: 1 })).toBeVisible();
      await page.goBack();
      await expect(page.getByText(`ResolutionReport ${prior.resolution_report_id}`, { exact: true })).toBeVisible();

      saveLifecycleArtifact({ closed: true });
    });
  });
});
