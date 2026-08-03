import { expect, test, type Locator, type Page } from "@playwright/test";

import { recordRunArtifact, trackBrowserEvidence, type BrowserApiEvidence } from "./support";

interface RouteObservation {
  route: string;
  final_url: string;
  refreshed: boolean;
  api_evidence: BrowserApiEvidence[];
}

function routeReady(page: Page, route: string): Locator {
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
    default: throw new Error(`No route readiness assertion for ${route}`);
  }
}

function assertHealthyEvidence(records: BrowserApiEvidence[], route: string) {
  const failures = records.filter((record) => (
    (record.failure && record.failure !== "net::ERR_ABORTED")
    || (record.status !== null && record.status >= 500)
  ));
  expect(failures, `${route} emitted failed or 5xx API requests`).toEqual([]);
}

test.describe.serial("real browser public route navigation", () => {
  test("direct-entry and refresh every public workspace", async ({ page }) => {
    const tracker = trackBrowserEvidence(page);
    const observations: RouteObservation[] = [];
    const routes = [
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
    ];

    for (const route of routes) {
      const mark = tracker.mark();
      await page.goto(route);
      await expect(routeReady(page, route)).toBeVisible();
      await page.reload();
      await expect(routeReady(page, route)).toBeVisible();
      const apiEvidence = tracker.since(mark);
      assertHealthyEvidence(apiEvidence, route);
      observations.push({ route, final_url: page.url(), refreshed: true, api_evidence: apiEvidence });
    }

    await page.goto("/");
    await expect(page).toHaveURL(/\/overview$/);
    await expect(routeReady(page, "/overview")).toBeVisible();
    await page.goto("/real-integration-not-found");
    await expect(page.getByRole("heading", { name: "页面不存在", level: 1 })).toBeVisible();

    recordRunArtifact("phase-2-route-observations.json", {
      observed_at: new Date().toISOString(),
      browser: await page.evaluate(() => navigator.userAgent),
      routes: observations,
      root_redirect: "/overview",
      not_found_route: "/real-integration-not-found",
      console_errors: tracker.consoleErrors,
      page_errors: tracker.pageErrors,
    });
  });

  test("Alert and Incident detail survive direct-entry, refresh, back and forward", async ({ page }) => {
    const tracker = trackBrowserEvidence(page);

    await page.goto("/alerts");
    await expect(page.getByTestId("alerts-list-route")).toBeVisible();
    await page.getByTestId("alert-row-summary").first().click();
    await expect(page.getByRole("button", { name: "打开完整详情" })).toBeVisible();
    await page.getByRole("button", { name: "打开完整详情" }).click();
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();
    const alertURL = page.url();
    await page.reload();
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();
    await page.goBack();
    await expect(page.getByTestId("alerts-list-route")).toBeVisible();
    await page.goForward();
    await expect(page.getByTestId("alert-detail-route")).toBeVisible();

    await page.goto("/incidents");
    await expect(page.getByRole("heading", { name: "Incident", level: 1 })).toBeVisible();
    await page.getByTestId("incident-row-summary").first().click();
    await expect(page.getByRole("link", { name: "打开完整 Incident 详情" })).toBeVisible();
    await page.getByRole("link", { name: "打开完整 Incident 详情" }).click();
    await expect(page.getByRole("link", { name: "返回 Incident 列表" })).toBeVisible();
    const incidentURL = page.url();
    await page.reload();
    await expect(page.getByRole("link", { name: "返回 Incident 列表" })).toBeVisible();
    await page.goBack();
    await expect(page.getByRole("heading", { name: "Incident", level: 1 })).toBeVisible();
    await page.goForward();
    await expect(page.getByRole("link", { name: "返回 Incident 列表" })).toBeVisible();

    const apiEvidence = tracker.since(0);
    assertHealthyEvidence(apiEvidence, "detail navigation");
    recordRunArtifact("phase-2-detail-navigation.json", {
      observed_at: new Date().toISOString(),
      alert_url: alertURL,
      incident_url: incidentURL,
      refresh: "PASS",
      back_forward: "PASS",
      api_evidence: apiEvidence,
      console_errors: tracker.consoleErrors,
      page_errors: tracker.pageErrors,
    });
  });
});
