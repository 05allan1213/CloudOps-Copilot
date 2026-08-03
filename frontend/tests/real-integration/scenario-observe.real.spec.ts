import { expect, test, type Page, type Response } from "@playwright/test";

import {
  capabilityPassed,
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
  waitForApiResponseResult,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const scenarioID = process.env.CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID || "";
const scenarioWorkload = "cloudops-scenario-fault";

test.beforeAll(() => {
  expect(scenarioID, "CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID").toMatch(/^scenario-/);
});

async function selectOption(page: Page, combobox: string | RegExp, option: string) {
  await page.getByRole("combobox", { name: combobox }).click();
  await page.getByRole("option", { name: option, exact: true }).click();
}

async function responseID(response: Response): Promise<string> {
  const body = await response.json() as { id?: string; data?: { id?: string } };
  const id = body.id ?? body.data?.id ?? "";
  expect(id, `${response.request().method()} ${new URL(response.url()).pathname} response id`).not.toBe("");
  return id;
}

async function selectScenarioWorkload(page: Page, advanced = false) {
  if (advanced) await page.getByRole("button", { name: "高级时间与采样参数" }).click();
  await selectOption(page, "Namespace", "demo");
  await expect(page.getByRole("combobox", { name: "Workload" })).toContainText(scenarioWorkload);
}

async function recoverLogsWorkspace(page: Page) {
  const main = page.getByTestId("app-main");
  const retry = page.getByRole("alert")
    .filter({ hasText: "REQUEST_FAILED" })
    .getByRole("button", { name: "重试" });
  await expect.poll(async () => (
    (await main.textContent())?.includes(scenarioWorkload) || await retry.isVisible()
  ), {
    message: "Logs workspace should load or expose its browser retry control",
    timeout: 45_000,
  }).toBe(true);
  if (await retry.isVisible()) await retry.click();
  await expect(main).toContainText(scenarioWorkload, { timeout: 45_000 });
}

test("infrastructure.resource-detail-events", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  await proveCapability(tracker, testInfo, {
    capabilityID: "infrastructure.resource-detail-events",
    uiAction: `在基础设施搜索 ${scenarioWorkload}，选择本轮 Scenario Deployment 并刷新 Inspector`,
    uiResult: "本轮 degraded Deployment、真实 workload 状态与 Kubernetes Events 在刷新后保持同一资源 identity",
  }, async () => {
    await page.goto("/infrastructure");
    await expect(page.getByTestId("infrastructure-workspace")).toBeVisible();
    await page.getByRole("searchbox", { name: "搜索资源" }).fill(scenarioWorkload);
    await waitForApiResponse(page, "GET /api/v1/resources", () => page.getByRole("button", { name: "筛选", exact: true }).click());
    await expect(page.getByText(scenarioWorkload, { exact: true }).first()).toBeVisible();
    await page.getByText(scenarioWorkload, { exact: true }).first().click();
    await expect(page.getByTestId("resource-events")).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText("Ready");
    await expect(page).toHaveURL(/resource=/);
    const selectedURL = page.url();
    await page.reload();
    await expect(page.getByTestId("resource-events")).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
    recordRunArtifact("objects/infrastructure.json", {
      run_id: runID,
      scenario_id: scenarioID,
      workload: scenarioWorkload,
      selected_url: selectedURL,
      refreshed_at: new Date().toISOString(),
    });
  });
});

test("infrastructure.topology-sse", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  await proveCapability(tracker, testInfo, {
    capabilityID: "infrastructure.topology-sse",
    uiAction: "打开本轮 Scenario 的基础设施投影，保持真实 topology EventSource 连接并刷新重进",
    uiResult: "SSE 返回当前 content hash，前端去重后仍从 Kubernetes authoritative projection 回显同一 Scenario 资源",
  }, async () => {
    await waitForApiResponse(
      page,
      "GET /api/v1/topology/events",
      () => page.goto("/infrastructure?cluster=cloudops-local&namespace=demo"),
    );
    await expect(page.getByTestId("infrastructure-workspace")).toBeVisible();
    await expect(page.getByText("kubernetes://cloudops-local").first()).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
    await waitForApiResponse(
      page,
      "GET /api/v1/topology/events",
      () => page.reload(),
    );
    await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
    await expect(page.getByText("kubernetes://cloudops-local").first()).toBeVisible();
  });
});

test("monitoring.query-cancel", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  const existingArtifact = readRunArtifact<{ execution_id?: string }>("objects/monitoring-cancel.json");
  const existingExecutionID = existingArtifact?.execution_id ?? "";

  await page.goto(existingExecutionID ? `/monitoring?execution=${existingExecutionID}` : "/monitoring");
  await expect(page.getByRole("heading", { name: "监控", level: 1 })).toBeVisible();
  if (!existingExecutionID) {
    await selectScenarioWorkload(page, true);
    await page.getByRole("button", { name: "6h", exact: true }).click();
    await selectOption(page, "查询 Step", "30s");
    await page.getByRole("button", { name: "PromQL", exact: true }).click();
    await page.getByRole("textbox", { name: "PromQL" }).fill('{__name__=~".+"}');
  }

  await proveCapability(tracker, testInfo, {
    capabilityID: "monitoring.query-cancel",
    uiAction: existingExecutionID
      ? `从 Monitoring deep link 重新进入并展开技术详情，读取当前测试运行已取消的 execution ${existingExecutionID}`
      : `选择 demo/${scenarioWorkload} 的最大合法 6h/30s 高基数 scoped PromQL，在执行前预等待原查询工具栏取消控件并立即点击`,
    expectedOperations: existingExecutionID
      ? ["GET /api/v1/monitoring/queries/{id}"]
      : ["POST /api/v1/monitoring/queries", "POST /api/v1/monitoring/queries/{id}/cancel"],
    uiResult: "同一 Query Execution 在刷新与 deep link 重进后保持 cancelled durable terminal state",
  }, async () => {
    let executionID = existingExecutionID;
    if (!executionID) {
      const cancel = page.getByRole("button", { name: "取消", exact: true });
      const cancelClick = cancel.click({ timeout: 45_000 });
      const cancelResponse = page.waitForResponse((response) => (
        response.request().method() === "POST"
        && /^\/api\/v1\/monitoring\/queries\/[^/]+\/cancel$/.test(new URL(response.url()).pathname)
      ));
      const startResponse = await waitForApiResponse(
        page,
        "POST /api/v1/monitoring/queries",
        () => page.getByRole("button", { name: "执行查询" }).click(),
      );
      executionID = await responseID(startResponse);
      await cancelClick;
      const cancelled = await cancelResponse;
      expect(cancelled.status(), "POST /api/v1/monitoring/queries/{id}/cancel").toBeGreaterThanOrEqual(200);
      expect(cancelled.status(), "POST /api/v1/monitoring/queries/{id}/cancel").toBeLessThan(300);
    }
    await expect(page.getByText("已取消", { exact: true }).first()).toBeVisible();
    await expect(page).toHaveURL(new RegExp(`execution=${executionID}`));
    await waitForApiResponse(
      page,
      "GET /api/v1/monitoring/queries/{id}",
      () => page.reload(),
    );
    await expect(page.getByText("已取消", { exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "展开查询技术详情" }).click();
    await expect(page.getByTestId("app-main")).toContainText(executionID);
    recordRunArtifact("objects/monitoring-cancel.json", {
      run_id: runID,
      scenario_id: scenarioID,
      execution_id: executionID,
      status: "cancelled",
    });
  });
});

test("monitoring query, definition and authorization lifecycle", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  const priorExecution = readRunArtifact<{ execution_id: string }>("objects/monitoring.json");
  await page.goto(priorExecution?.execution_id ? `/monitoring?execution=${priorExecution.execution_id}` : "/monitoring");
  await expect(page.getByRole("heading", { name: "监控", level: 1 })).toBeVisible();

  let executionID = priorExecution?.execution_id ?? "";
  if (!capabilityPassed("monitoring.query-run-history")) {
    await selectScenarioWorkload(page, true);
    await page.getByRole("button", { name: "15m", exact: true }).click();
    await proveCapability(tracker, testInfo, {
      capabilityID: "monitoring.query-run-history",
      uiAction: `选择 demo/${scenarioWorkload} 与最近 15m，从指标查询控件执行并刷新重进历史`,
      uiResult: "Prometheus Query Execution 完成并在刷新后按同一 durable execution identity 回显",
    }, async () => {
      const response = await waitForApiResponse(page, "POST /api/v1/monitoring/queries", () => page.getByRole("button", { name: "执行查询" }).click());
      executionID = await responseID(response);
      await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible({ timeout: 120_000 });
      await expect(page).toHaveURL(new RegExp(`execution=${executionID}`));
      await waitForApiResponse(page, "GET /api/v1/monitoring/queries/{id}", () => page.reload());
      await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible();
      await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
      recordRunArtifact("objects/monitoring.json", { run_id: runID, scenario_id: scenarioID, execution_id: executionID });
    });
  } else {
    await expect(page.getByText("已完成", { exact: true }).first()).toBeVisible();
  }

  const definitionTitle = `${runID} scenario CPU`;
  let definitionID = "";
  const definitionExists = await page.getByText(definitionTitle, { exact: true }).isVisible();
  await proveCapability(tracker, testInfo, {
    capabilityID: "monitoring.definition-save",
    uiAction: "从本轮成功 Metrics execution 打开保存对话框，填写 run_id 标题并保存",
    uiResult: "Query Definition 在刷新后仍从 MySQL 查询资产回显",
    ...(definitionExists ? { expectedOperations: ["GET /api/v1/monitoring/query-definitions"] } : {}),
  }, async () => {
    if (definitionExists) {
      await page.reload();
      await expect(page.getByTestId("app-main")).toContainText(definitionTitle);
      return;
    }
    await page.getByRole("button", { name: "保存定义" }).click();
    await page.locator('input[name="query-definition-title"]').fill(definitionTitle);
    await page.locator('textarea[name="query-definition-description"]').fill(`real integration ${runID}`);
    const response = await waitForApiResponse(page, "POST /api/v1/monitoring/query-definitions", () => page.getByRole("button", { name: "保存定义" }).last().click());
    definitionID = await responseID(response);
    await expect(page.getByRole("status").filter({ hasText: "Query Definition 已保存" })).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(definitionTitle);
    recordRunArtifact("objects/monitoring-definition.json", { run_id: runID, execution_id: executionID, definition_id: definitionID, title: definitionTitle });
  });

  let authorizationID = "";
  await proveCapability(tracker, testInfo, {
    capabilityID: "monitoring.authorization-lifecycle",
    uiAction: "从本轮 execution 创建一次性精确 Agent 查询授权，再从查询资产撤销",
    uiResult: "授权精确 identity 与已撤销终态在刷新后由 MySQL 回显",
  }, async () => {
    await page.getByRole("button", { name: "授权一次" }).click();
    const createResponse = await waitForApiResponse(page, "POST /api/v1/monitoring/query-authorizations", () => page.getByRole("button", { name: "创建一次性授权" }).click());
    authorizationID = await responseID(createResponse);
    await page.getByRole("tab", { name: "Agent 授权" }).click();
    const authorization = page.locator("article").filter({ hasText: "一次性精确查询" }).first();
    await expect(authorization).toContainText("有效");
    await authorization.getByRole("button", { name: "撤销" }).click();
    await waitForApiResponse(page, "POST /api/v1/monitoring/query-authorizations/{id}/revoke", () => page.getByRole("button", { name: "确认撤销" }).click());
    await expect(authorization).toContainText("已撤销");
    await page.reload();
    await page.getByRole("tab", { name: "Agent 授权" }).click();
    await expect(page.locator("article").filter({ hasText: "一次性精确查询" }).first()).toContainText("已撤销");
    recordRunArtifact("objects/monitoring-authorization.json", { run_id: runID, execution_id: executionID, authorization_id: authorizationID, status: "revoked" });
  });
});

test("logs query history and durable Evidence echo", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  const priorQuery = readRunArtifact<{ query_id: string }>("objects/logs-query.json");
  const priorEvidence = readRunArtifact<{ evidence_id: string }>("objects/logs-evidence.json");
  const resourceID = new URL(
    readRunArtifact<{ selected_url: string }>("objects/infrastructure.json")?.selected_url ?? "http://localhost",
  ).searchParams.get("resource") ?? "";
  const queryRoute = priorQuery?.query_id
    ? `/logs?cluster=cloudops-local&namespace=demo&resource=${encodeURIComponent(resourceID)}&query=${priorQuery.query_id}`
    : "/logs";
  await page.goto(queryRoute);
  await expect(page.getByRole("heading", { name: "日志分析", level: 1 })).toBeVisible();
  await recoverLogsWorkspace(page);

  let queryID = priorQuery?.query_id ?? "";
  if (!capabilityPassed("logs.query-run-history")) {
    await selectScenarioWorkload(page);
    await page.getByRole("button", { name: "15m", exact: true }).click();
    await proveCapability(tracker, testInfo, {
      capabilityID: "logs.query-run-history",
      uiAction: `选择 demo/${scenarioWorkload}，从 Logs 控件运行最近 15m 查询并刷新重进`,
      uiResult: "Elasticsearch 返回本轮真实日志，Query Execution 与结果在刷新后保持",
    }, async () => {
      const response = await waitForApiResponse(page, "POST /api/v1/logs/queries", () => page.getByRole("button", { name: "搜索日志" }).click());
      queryID = await responseID(response);
      await expect(page).toHaveURL(new RegExp(`query=${queryID}`));
      await expect(page.getByRole("checkbox", { name: /作为 Evidence/ }).first()).toBeVisible();
      await waitForApiResponse(page, "GET /api/v1/logs/queries/{id}", () => page.reload());
      await expect(page.getByRole("checkbox", { name: /作为 Evidence/ }).first()).toBeVisible();
      await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
      recordRunArtifact("objects/logs-query.json", { run_id: runID, scenario_id: scenarioID, query_id: queryID });
    });
  } else {
    await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
    await expect(page.getByTestId("app-main")).toContainText(/日志结果已陈旧|Provider 日志结果已过期/);
  }

  await proveCapability(tracker, testInfo, {
    capabilityID: "logs.evidence-save",
    uiAction: "选择本轮真实日志行并点击保存 Evidence，然后刷新原 owning query",
    uiResult: "Evidence durable identity 与计数在刷新后的 owning query UI 回显",
    ...(priorEvidence?.evidence_id ? {
      expectedOperations: ["GET /api/v1/logs/queries/{id}", "GET /api/v1/logs/queries/{id}/evidence"],
    } : {}),
  }, async () => {
    if (priorEvidence?.evidence_id) {
      await page.reload();
      await recoverLogsWorkspace(page);
      await expect(page.getByTestId("app-main")).toContainText(priorEvidence.evidence_id);
      return;
    }
    await page.getByRole("checkbox", { name: /作为 Evidence/ }).first().click();
    const response = await waitForApiResponse(page, "POST /api/v1/logs/queries/{id}/evidence", () => page.getByRole("button", { name: "保存 1 条" }).click());
    const evidenceID = await responseID(response);
    await expect(page.getByTestId("app-main").getByRole("status").filter({ hasText: "已保留 1 条日志 Evidence" })).toBeVisible();
    recordRunArtifact("objects/logs-evidence.json", { run_id: runID, query_id: queryID, evidence_id: evidenceID });
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(evidenceID);
  });
});

test("Trace search, detail history and durable Evidence echo", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  const priorSearch = readRunArtifact<{ search_id: string; selected_url?: string }>("objects/trace-search.json");
  const priorEvidence = readRunArtifact<{ evidence_id: string; search_id: string; trace_id: string; selected_url?: string }>("objects/trace-evidence.json");
  const resourceID = new URL(
    readRunArtifact<{ selected_url: string }>("objects/infrastructure.json")?.selected_url ?? "http://localhost",
  ).searchParams.get("resource") ?? "";
  const searchRoute = priorSearch?.search_id
    ? (priorEvidence?.selected_url ?? priorSearch.selected_url ?? `/traces?cluster=cloudops-local&namespace=demo&resource=${encodeURIComponent(resourceID)}&search=${priorSearch.search_id}`)
    : "/traces";
  await page.goto(searchRoute);
  await expect(page.getByRole("heading", { name: "链路分析", level: 1 })).toBeVisible();

  let searchID = priorSearch?.search_id ?? "";
  let traceID = priorEvidence?.trace_id ?? "";
  await proveCapability(tracker, testInfo, {
    capabilityID: "traces.search-detail-history",
    uiAction: `选择 demo/${scenarioWorkload}，从 Trace 控件搜索最近 15m，打开真实 Trace/Waterfall 并刷新`,
    uiResult: "Tempo Search、Trace detail 与 Span Waterfall 在刷新后保持同一 durable identities",
    ...(priorSearch?.search_id ? {
      expectedOperations: ["GET /api/v1/traces/searches/{id}", "GET /api/v1/traces/{trace_id}"],
    } : {}),
  }, async () => {
    if (priorEvidence?.evidence_id && priorSearch?.search_id) {
      await waitForApiResponse(page, "GET /api/v1/traces/searches/{id}", () => page.reload());
      await expect(page.getByTestId("trace-waterfall")).toBeVisible();
      await expect(page.getByTestId("app-main")).toContainText(traceID);
      recordRunArtifact("objects/trace-search.json", {
        run_id: runID,
        scenario_id: scenarioID,
        search_id: searchID,
        trace_id: traceID,
        selected_url: page.url(),
      });
      return;
    }
    if (priorSearch?.search_id) {
      await waitForApiResponse(page, "GET /api/v1/traces/searches/{id}", () => page.reload());
    } else {
      await selectScenarioWorkload(page);
      await page.getByRole("button", { name: "15m", exact: true }).click();
      const response = await waitForApiResponse(page, "POST /api/v1/traces/searches", () => page.getByRole("button", { name: "发现 Trace" }).click());
      searchID = await responseID(response);
      await expect(page).toHaveURL(new RegExp(`search=${searchID}`));
    }
    const completeTraceButton = () => page.getByTestId("trace-search-results")
      .locator("article")
      .filter({ has: page.getByText(scenarioWorkload, { exact: true }) })
      .getByRole("button", { name: /^打开 Trace / })
      .first();
    let traceButton = completeTraceButton();
    if (!(await traceButton.isVisible())) {
      await selectScenarioWorkload(page);
      await page.getByRole("button", { name: "15m", exact: true }).click();
      const response = await waitForApiResponse(page, "POST /api/v1/traces/searches", () => page.getByRole("button", { name: "发现 Trace" }).click());
      searchID = await responseID(response);
      await expect(page).toHaveURL(new RegExp(`search=${searchID}`));
      traceButton = completeTraceButton();
    }
    await expect(traceButton).toBeVisible();
    traceID = (await traceButton.getAttribute("aria-label"))?.replace(/^打开 Trace /, "") ?? "";
    expect(traceID).not.toBe("");
    const detailOperation = "GET /api/v1/traces/{trace_id}";
    const detailResponse = await waitForApiResponseResult(page, detailOperation, () => traceButton.click());
    if (detailResponse.status() >= 500) {
      const retryDetail = page.getByRole("status")
        .filter({ hasText: "Trace detail 当前不可用" })
        .getByRole("button", { name: "重试" });
      await expect(retryDetail).toBeVisible();
      await waitForApiResponse(page, detailOperation, () => retryDetail.click());
    } else {
      expect(detailResponse.status(), detailOperation).toBeGreaterThanOrEqual(200);
      expect(detailResponse.status(), detailOperation).toBeLessThan(300);
    }
    await expect(page.getByTestId("trace-waterfall")).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("trace-waterfall")).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText(traceID);
    recordRunArtifact("objects/trace-search.json", {
      run_id: runID,
      scenario_id: scenarioID,
      search_id: searchID,
      trace_id: traceID,
      selected_url: page.url(),
    });
  });

  await proveCapability(tracker, testInfo, {
    capabilityID: "traces.evidence-save",
    uiAction: "选择本轮真实 Trace 的一个 Span 并点击保存 Evidence，然后刷新 owning Trace",
    uiResult: "Trace Evidence durable identity 与计数在刷新后的 owning Trace UI 回显",
    ...(priorEvidence?.evidence_id ? {
      expectedOperations: ["GET /api/v1/traces/searches/{id}/evidence"],
    } : {}),
  }, async () => {
    if (priorEvidence?.evidence_id) {
      await waitForApiResponse(page, "GET /api/v1/traces/searches/{id}/evidence", () => page.reload());
      await expect(page.getByTestId("app-main")).toContainText(priorEvidence.evidence_id);
      return;
    }
    await page.getByRole("checkbox", { name: /选择 span .*作为 Evidence/ }).first().click();
    const response = await waitForApiResponse(page, "POST /api/v1/traces/searches/{id}/traces/{trace_id}/evidence", () => page.getByRole("button", { name: "保存 Evidence" }).click());
    const evidenceID = await responseID(response);
    await expect(page.getByTestId("app-main").getByRole("status")).toContainText("已保留 1 个 Span Evidence");
    recordRunArtifact("objects/trace-evidence.json", {
      run_id: runID,
      search_id: searchID,
      trace_id: traceID,
      evidence_id: evidenceID,
      selected_url: page.url(),
    });
    await page.reload();
    await expect(page.getByTestId("app-main")).toContainText(evidenceID);
  });
});

test("alerts.read", async ({ page }, testInfo) => {
  const tracker = trackBrowserEvidence(page);
  await proveCapability(tracker, testInfo, {
    capabilityID: "alerts.read",
    uiAction: `在 Alerts 搜索 ${scenarioWorkload}，打开 Inspector、完整详情并刷新`,
    uiResult: "本轮 firing Alert 的 Scenario identity、Provider facts、Signals 与 Timeline 在刷新后保持",
  }, async () => {
    await page.goto("/alerts");
    await expect(page.getByTestId("alerts-list-route")).toBeVisible();
    await page.getByRole("textbox", { name: "搜索告警或目标" }).fill(scenarioWorkload);
    await waitForApiResponse(page, "GET /api/v1/alerts", () => page.getByRole("button", { name: "应用" }).click());
    await page.getByTestId("alert-row-summary").first().click();
    await expect(page.getByRole("button", { name: "打开完整详情" })).toBeVisible();
    await page.getByRole("button", { name: "打开完整详情" }).click();
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText(scenarioID);
    const alertID = new URL(page.url()).pathname.split("/").at(-1) ?? "";
    await page.reload();
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();
    await expect(page.getByTestId("app-main")).toContainText(scenarioID);
    recordRunArtifact("objects/alert.json", { run_id: runID, scenario_id: scenarioID, alert_id: alertID, status: "firing" });
  });
});
