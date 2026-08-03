import { expect, test } from "@playwright/test";

import { proveCapability, trackBrowserEvidence, waitForApiResponse } from "./support";

test.describe.serial("real browser read integration", () => {
  test("shell.bootstrap-overview", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "shell.bootstrap-overview",
      uiAction: "直接进入 /overview，点击刷新运维态势，再刷新浏览器",
      uiResult: "Overview 与 9/9 Provider 状态在浏览器刷新后仍由后端投影回显",
    }, async () => {
      await page.goto("/overview");
      await expect(page.getByTestId("overview-command-center")).toBeVisible();
      await expect(page.getByRole("link", { name: /Provider 健康：9\/9 可用/ })).toBeVisible();
      await waitForApiResponse(page, "GET /api/v1/overview", () => page.getByRole("button", { name: "刷新运维态势" }).click());
      await page.reload();
      await expect(page.getByTestId("overview-command-center")).toBeVisible();
      await expect(page.getByRole("link", { name: /Provider 健康：9\/9 可用/ })).toBeVisible();
    });
  });

  test("shell.scope-read", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "shell.scope-read",
      uiAction: "从主导航点击设置并读取运行范围",
      uiResult: "Settings 与 Shell 均显示 active cloudops-local Scope",
    }, async () => {
      await page.goto("/overview");
      await expect(page.getByTestId("overview-command-center")).toBeVisible();
      await page.getByRole("link", { name: "设置", exact: true }).click();
      await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
      await waitForApiResponse(page, "GET /api/v1/scopes", () => page.reload());
      await expect(page.getByRole("combobox", { name: /活动运行范围：cloudops-local/ })).toBeVisible();
    });
  });

  test("notifications.list", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "notifications.list",
      uiAction: "打开通知收件箱，点击刷新，关闭后重新打开",
      uiResult: "通知列表、0 条未读与 context links 重新打开后仍由 MySQL 投影回显",
    }, async () => {
      await page.goto("/overview");
      await expect(page.getByTestId("overview-command-center")).toBeVisible();
      await page.getByRole("button", { name: "打开通知收件箱" }).click();
      await expect(page.getByRole("dialog")).toContainText("0 条未读");
      await waitForApiResponse(page, "GET /api/v1/notifications", () => page.getByRole("button", { name: "刷新通知" }).click());
      await page.getByRole("button", { name: "关闭通知收件箱" }).click();
      await page.getByRole("button", { name: "打开通知收件箱" }).click();
      await expect(page.getByRole("dialog")).toContainText("0 条未读");
      await expect(page.getByRole("link", { name: "打开上下文" }).first()).toHaveAttribute("href", /^\/alerts\//);
    });
  });

  test("topology and infrastructure browser projections", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);

    await proveCapability(tracker, testInfo, {
      capabilityID: "infrastructure.topology-read",
      uiAction: "直接进入 Atlas，切换结构化视图，再从导航进入基础设施并刷新",
      uiResult: "Atlas 与 Infrastructure 均展示当前 cloudops-local Kubernetes authoritative projection",
    }, async () => {
      await page.goto("/atlas");
      await expect(page.getByTestId("atlas-workspace")).toBeVisible();
      await expect(page.getByText("kubernetes://cloudops-local").first()).toBeVisible();
      await page.getByRole("button", { name: "切换到结构化资源视图" }).click();
      await expect(page).toHaveURL(/view=structured/);
      await page.getByRole("link", { name: "基础设施", exact: true }).click();
      await expect(page.getByTestId("infrastructure-workspace")).toBeVisible();
      await expect(page.getByText("kubernetes://cloudops-local").first()).toBeVisible();
      await waitForApiResponse(page, "GET /api/v1/topology", () => page.getByRole("button", { name: "刷新基础设施资源" }).click());
      await page.reload();
      await expect(page.getByTestId("infrastructure-workspace")).toBeVisible();
    });

    await proveCapability(tracker, testInfo, {
      capabilityID: "infrastructure.resources-list",
      uiAction: "在基础设施搜索框提交 cloudops-api 筛选并清除筛选",
      uiResult: "URL 和资源列表回显 cloudops-api 筛选，刷新后保持，再从清除按钮恢复",
      expectedOperations: ["GET /api/v1/resources"],
    }, async () => {
      await page.getByRole("searchbox", { name: "搜索资源" }).fill("cloudops-api");
      await waitForApiResponse(page, "GET /api/v1/resources", () => page.getByRole("button", { name: "筛选", exact: true }).click());
      await expect(page).toHaveURL(/search=cloudops-api/);
      await expect(page.getByText("cloudops-api", { exact: true }).first()).toBeVisible();
      await page.reload();
      await expect(page.getByRole("searchbox", { name: "搜索资源" })).toHaveValue("cloudops-api");
      await waitForApiResponse(page, "GET /api/v1/resources", () => page.getByRole("button", { name: "清除资源筛选" }).click());
      await expect(page).not.toHaveURL(/search=cloudops-api/);
    });
  });

  for (const catalog of [
    { capabilityID: "monitoring.catalog", route: "/monitoring", heading: "监控", refresh: "刷新监控工作区", operation: "GET /api/v1/monitoring/catalog" },
    { capabilityID: "logs.catalog", route: "/logs", heading: "日志分析", refresh: "刷新日志工作区", operation: "GET /api/v1/logs/catalog" },
    { capabilityID: "traces.catalog", route: "/traces", heading: "链路分析", refresh: "刷新链路工作区", operation: "GET /api/v1/traces/catalog" },
  ]) {
    test(catalog.capabilityID, async ({ page }, testInfo) => {
      const tracker = trackBrowserEvidence(page);
      await proveCapability(tracker, testInfo, {
        capabilityID: catalog.capabilityID,
        uiAction: `直接进入 ${catalog.route} 并点击页面刷新`,
        uiResult: `${catalog.heading} workspace 从当前 Provider 展示 catalog 和 absolute bounds`,
      }, async () => {
        await page.goto(catalog.route);
        await expect(page.getByRole("heading", { name: catalog.heading, level: 1 })).toBeVisible();
        await waitForApiResponse(page, catalog.operation, () => page.getByRole("button", { name: catalog.refresh }).click());
        await page.reload();
        await expect(page.getByRole("heading", { name: catalog.heading, level: 1 })).toBeVisible();
      });
    });
  }

  test("agent.history-detail", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.history-detail",
      uiAction: "直接进入 Agent，选择持久化 Consultation/Investigation 并刷新",
      uiResult: "Agent 历史与当前 detail 在刷新后保持同一后端 identity",
    }, async () => {
      await page.goto("/agent");
      await expect(page.getByTestId("agent-workspace")).toBeVisible();
      const historyExpand = page.getByTestId("agent-history-expand");
      if (await historyExpand.isVisible()) await historyExpand.click();
      await waitForApiResponse(page, "GET /api/v1/agent/investigations/{id}", () => page.getByRole("tab", { name: /^调查/ }).click());
      await expect(page).toHaveURL(/investigation=/);
      await waitForApiResponse(page, "GET /api/v1/agent/consultations/{id}", () => page.getByRole("tab", { name: /^会话/ }).click());
      await expect(page).toHaveURL(/consultation=/);
      await page.reload();
      await expect(page.getByTestId("agent-workspace")).toBeVisible();
    });
  });

  for (const entry of [
    { capabilityID: "agent.knowledge-read", text: "Owner Knowledge", tab: /^Evidence/, operations: ["GET /api/v1/knowledge-items"] },
    { capabilityID: "agent.runbook-guidance", text: "Runbook Guidance", tab: /^Evidence/, operations: ["GET /api/v1/runbook-guidance"] },
    { capabilityID: "agent.operation-plans-read", text: "Operation Plan", tab: /^权限/, operations: ["GET /api/v1/operation-plans"] },
  ]) {
    test(entry.capabilityID, async ({ page }, testInfo) => {
      const tracker = trackBrowserEvidence(page);
      await proveCapability(tracker, testInfo, {
        capabilityID: entry.capabilityID,
        uiAction: `在 Agent Inspector 查看 ${entry.text}`,
        uiResult: `${entry.text} 从持久化索引读取并显示`,
        expectedOperations: entry.operations,
      }, async () => {
        await page.goto("/agent");
        await expect(page.getByTestId("agent-workspace")).toBeVisible();
        const inspectorExpand = page.getByTestId("agent-inspector-expand");
        if (await inspectorExpand.isVisible()) await inspectorExpand.click();
        await page.getByRole("tab", { name: entry.tab }).click();
        await expect(page.locator("main")).toContainText(entry.text);
      });
    });
  }

  test("devops.workspace-read", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "devops.workspace-read",
      uiAction: "从主导航打开 DevOps，并在浏览器刷新后读取全局队列和 Provider 投影",
      uiResult: "DevOps workspace 的 plans/cards/executions/providers 在刷新后由后端回显",
    }, async () => {
      await page.goto("/devops");
      await expect(page.getByTestId("devops-global-queue")).toBeVisible();
      await page.reload();
      await expect(page.getByTestId("devops-global-queue")).toBeVisible();
    });
  });

  test("settings.read", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "settings.read",
      uiAction: "直接进入 Settings，遍历五个 section、Storage 与 Revision history 后刷新",
      uiResult: "active revision 13、五个 section、Storage 和历史从 MySQL 回显",
    }, async () => {
      await page.goto("/settings");
      await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
      await expect(page.getByTestId("app-main")).toContainText("Revision #13");
      const navigation = page.getByRole("navigation", { name: "Settings 分区" });
      for (const section of ["系统", "运行范围", "升级策略", "Provider", "Secret 引用"]) {
        const button = navigation.getByRole("button", { name: new RegExp(`^${section}`) });
        await button.click();
        await expect(button).toHaveAttribute("aria-current", "page");
      }
      const revisions = navigation.getByRole("button", { name: /^Revision 历史/ });
      await revisions.click();
      await expect(revisions).toHaveAttribute("aria-current", "page");
      await expect(page.getByRole("heading", { name: "Configuration Revisions" })).toBeVisible();
      await expect(page.getByRole("heading", { name: "存储与备份" })).toBeVisible();
      await page.reload();
      await expect(page.getByTestId("app-main")).toContainText("Revision #13");
      await expect(page.getByRole("heading", { name: "存储与备份" })).toBeVisible();
    });
  });
});
