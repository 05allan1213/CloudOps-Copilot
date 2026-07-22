import { expect, test, type Page } from "@playwright/test";

import {
  configureFixture,
  expectHeadingOrder,
  expectMinimumTarget,
  expectNoLayoutOverflow,
  incidentID,
  monitorBrowser,
} from "./support";

test.describe.configure({ mode: "serial" });

test.beforeEach(async ({ page }) => {
  await page.clock.setFixedTime(new Date("2026-07-23T04:00:00.000Z"));
});

async function openList(page: Page) {
  await page.goto("/incidents?e2e=list", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#incident-list-title")).toBeVisible();
}

async function openDetail(page: Page) {
  await page.goto(`/incidents/${incidentID}?e2e=detail`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".incident-detail-view")).toBeVisible();
  await expect(page.locator(".incident-detail-view h1")).toBeVisible();
}

test("Incident List is keyboard navigable and URL-synced", async ({ page, request }) => {
  const browser = monitorBrowser(page);
  await configureFixture(request, { list: "ready" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openList(page);

  await page.keyboard.press("Tab");
  await expect(page.locator(".skip-link")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("#incident-content")).toBeFocused();
  await expectHeadingOrder(page);
  await expectNoLayoutOverflow(page);

  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(3);
  await page.getByLabel("Exact service").fill("checkout-api");
  await page.getByRole("button", { name: "Apply Filters" }).click();
  await expect(page).toHaveURL(/service=checkout-api/);
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(3);

  const sortButton = page.getByRole("button", { name: "Severity" });
  await sortButton.focus();
  await page.keyboard.press("Enter");
  await expect(sortButton).toBeFocused();

  const firstIncident = page.locator(".desktop-table-wrap tbody a").first();
  await firstIncident.click();
  await expect(page.locator(".incident-detail-view h1")).toBeFocused();
  browser.expectClean();
});

test("Incident List exposes loading, empty, forbidden, and unavailable states", async ({ page, request }) => {
  await configureFixture(request, { list: "loading" });
  await page.goto("/incidents?e2e=list-loading", { waitUntil: "domcontentloaded" });
  await expect(page.locator(".incident-skeleton")).toBeVisible();
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(3, { timeout: 5_000 });

  await configureFixture(request, { list: "empty" });
  await openList(page);
  await expect(page.getByRole("heading", { name: "No incidents found" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry Incidents" })).toBeVisible();

  await configureFixture(request, { list: "forbidden" });
  await openList(page);
  await expect(page.getByRole("heading", { name: "Viewer access is required" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry Access Check" })).toBeVisible();

  await configureFixture(request, { list: "error" });
  await openList(page);
  await expect(page.getByRole("heading", { name: "Incident projection unavailable" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry Incidents" })).toBeVisible();
});

test("Incident Detail keeps the four-zone chain, dialog focus, and responsive layout", async ({ page, request }) => {
  const browser = monitorBrowser(page);
  await configureFixture(request, { role: "operator", sections: "ready", sse: "connected" });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await openDetail(page);

  await expect(page.locator(".desktop-zone-list a")).toHaveCount(4);
  await expect(page.locator(".report")).toHaveCount(1);
  await expectHeadingOrder(page);
  await expectNoLayoutOverflow(page);
  await expectMinimumTarget(page, ".approve-button, .reject-button");
  await expectMinimumTarget(page, ".copy-button");

  const inspectEvidence = page.locator(".evidence-desktop .inspect-button").first();
  await inspectEvidence.click();
  await expect(page).toHaveURL(/evidence=00000040-0000-4000-8000-000000000001/);
  const evidenceDrawer = page.locator("dialog.evidence-drawer");
  await expect(evidenceDrawer).toHaveAttribute("open", "");
  await expect(evidenceDrawer.locator(".drawer-close")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(evidenceDrawer).not.toHaveAttribute("open", "");
  await expect(inspectEvidence).toBeFocused();

  const approve = page.locator(".approve-button");
  await approve.click();
  const decisionDialog = page.locator("dialog.decision-dialog");
  await expect(decisionDialog).toHaveAttribute("open", "");
  await expect(decisionDialog.locator("textarea[name=decision_reason]")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(decisionDialog).not.toHaveAttribute("open", "");
  await expect(approve).toBeFocused();

  for (const [width, height] of [[320, 812], [375, 812], [667, 375], [720, 900], [768, 900], [1024, 768], [1440, 1000]]) {
    await page.setViewportSize({ width, height });
    await page.waitForTimeout(250);
    await expectNoLayoutOverflow(page);
  }

  await page.setViewportSize({ width: 375, height: 812 });
  await expectMinimumTarget(page, ".mobile-navigation-trigger, .icon-button, #incident-zone-select, .approve-button, .reject-button");
  const navigationTrigger = page.locator(".mobile-navigation-trigger");
  await navigationTrigger.click();
  await expect(page.locator(".mobile-navigation.el-drawer")).toBeVisible();
  await page.locator(".mobile-navigation .el-drawer__close-btn").click();
  await expect(navigationTrigger).toBeFocused();
  browser.expectClean();
});

test("viewer and projection states fail closed without hiding recovery truth", async ({ page, request }) => {
  await configureFixture(request, { role: "viewer", sections: "ready", verification: "passed", sse: "connected" });
  await openDetail(page);
  await expect(page.locator(".approve-button")).toBeDisabled();
  await expect(page.locator(".reject-button")).toBeDisabled();
  await expect(page.locator(".decision-actions p")).toContainText("Viewer");

  await configureFixture(request, { role: "operator", sections: "empty", verification: "not_run", sse: "connected" });
  await openDetail(page);
  await expect(page.locator("#evidence .section-message")).toContainText("No persisted Evidence");
  await expect(page.locator("#verifications .section-message")).toContainText("NOT RUN");
  await expect(page.locator("#resolution-report .section-message")).toContainText("No immutable ResolutionReport");
  await expect(page.locator(".report")).toHaveCount(0);

  await configureFixture(request, { role: "operator", sections: "error", verification: "passed", sse: "connected" });
  await openDetail(page);
  await expect(page.locator("#evidence .section-message")).toContainText("Projection unavailable");
  await expect(page.locator("#evidence .request-identity")).toContainText("fixture-request-503");
});

test("realtime reconnect keeps the projection visible and restores Live state", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/503.*\/events/] });
  await configureFixture(request, { sse: "reconnect", sections: "ready" });
  await openDetail(page);
  const realtimeChip = page.locator(".realtime-chip");
  await expect(realtimeChip).toContainText("Reconnecting", { timeout: 4_000 });
  await expect(realtimeChip).toContainText("Live", { timeout: 6_000 });
  const metricsResponse = await request.get("http://127.0.0.1:18082/fixture/metrics");
  const metrics = await metricsResponse.json();
  expect(metrics.eventConnections).toBeGreaterThanOrEqual(2);
  expect(metrics.sseAttempts).toBeGreaterThanOrEqual(2);
  browser.expectClean();
});

test("command feedback preserves disabled and retryable states", async ({ page, request }) => {
  await configureFixture(request, { command: "503", role: "operator", sse: "connected" });
  await openDetail(page);
  await page.locator(".approve-button").click();
  const dialog = page.locator("dialog.decision-dialog");
  await dialog.locator("textarea[name=decision_reason]").fill("Bounded fixture reason for retry validation");
  await dialog.locator(".submit-decision").click();
  await expect(dialog.locator(".command-feedback--unavailable")).toBeVisible();
  await expect(dialog.locator(".submit-decision")).toBeDisabled();
  const retry = dialog.getByRole("button", { name: "Retry With Same Idempotency Key" });
  await expect(retry).toBeVisible();
  await retry.click();
  await expect(dialog.locator(".command-feedback--unavailable")).toBeVisible();
  const metricsResponse = await request.get("http://127.0.0.1:18082/fixture/metrics");
  const metrics = await metricsResponse.json();
  expect(metrics.commands).toHaveLength(2);
  expect(metrics.commands[0].idempotencyKey).toBe(metrics.commands[1].idempotencyKey);
});

test("themes, motion, contrast, zoom-equivalent width, and visual baselines remain stable", async ({ page, request }) => {
  await configureFixture(request, { sse: "connected" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "no-preference" });
  await openList(page);
  await expect(page).toHaveScreenshot("incident-list-dark-1440.png");

  await page.getByRole("button", { name: "Switch to light theme" }).click();
  await expect(page.locator("html")).toHaveClass(/light/);
  await expect(page).toHaveScreenshot("incident-list-light-1440.png");

  const contrast = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement);
    const parse = (value: string) => {
      const hex = value.trim().replace(/^#/, "");
      const normalized = hex.length === 3 ? hex.split("").map((part) => part + part).join("") : hex;
      return [0, 2, 4].map((offset) => Number.parseInt(normalized.slice(offset, offset + 2), 16) / 255);
    };
    const luminance = (color: number[]) => color.map((channel) => channel <= 0.03928 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4)
      .reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index], 0);
    const ratio = (foreground: string, background: string) => {
      const foregroundLuminance = luminance(parse(foreground));
      const backgroundLuminance = luminance(parse(background));
      return (Math.max(foregroundLuminance, backgroundLuminance) + 0.05) / (Math.min(foregroundLuminance, backgroundLuminance) + 0.05);
    };
    return {
      primary: ratio(style.getPropertyValue("--co-text-primary"), style.getPropertyValue("--co-bg-canvas")),
      secondary: ratio(style.getPropertyValue("--co-text-secondary"), style.getPropertyValue("--co-bg-canvas")),
      muted: ratio(style.getPropertyValue("--co-text-muted"), style.getPropertyValue("--co-bg-surface")),
      action: ratio(style.getPropertyValue("--co-action-primary"), style.getPropertyValue("--co-bg-surface")),
    };
  });
  expect(contrast.primary).toBeGreaterThanOrEqual(4.5);
  expect(contrast.secondary).toBeGreaterThanOrEqual(4.5);
  expect(contrast.muted).toBeGreaterThanOrEqual(4.5);
  expect(contrast.action).toBeGreaterThanOrEqual(4.5);

  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  const reducedMotion = await page.locator(".app-main > *").first().evaluate((element) => ({
    animationDuration: getComputedStyle(element).animationDuration,
    transitionDuration: getComputedStyle(element).transitionDuration,
  }));
  expect(Number.parseFloat(reducedMotion.animationDuration)).toBeLessThanOrEqual(0.001);
  expect(Number.parseFloat(reducedMotion.transitionDuration)).toBeLessThanOrEqual(0.001);

  const resources = await page.evaluate(() => performance.getEntriesByType("resource").map((entry) => entry.name));
  expect(resources.filter((url) => /fonts\.(googleapis|gstatic)|use\.typekit|fonts\.adobe/.test(url))).toEqual([]);
  await page.setViewportSize({ width: 720, height: 900 });
  await expectNoLayoutOverflow(page);
});

test("basic interaction stays below the input feedback budget", async ({ page, request }) => {
  await configureFixture(request, { sse: "connected" });
  await page.addInitScript(() => {
    const metrics = { cls: 0, longTasks: [] as number[] };
    (window as unknown as { __cloudopsMetrics: typeof metrics }).__cloudopsMetrics = metrics;
    if ("PerformanceObserver" in window) {
      try {
        new PerformanceObserver((list) => {
          for (const entry of list.getEntries()) {
            if (entry.entryType === "layout-shift" && !(entry as PerformanceEntry & { hadRecentInput?: boolean }).hadRecentInput) {
              metrics.cls += (entry as PerformanceEntry & { value?: number }).value || 0;
            }
          }
        }).observe({ type: "layout-shift", buffered: true });
      } catch { /* Browser does not expose layout-shift in every mode. */ }
      try {
        new PerformanceObserver((list) => {
          metrics.longTasks.push(...list.getEntries().map((entry) => entry.duration));
        }).observe({ type: "longtask", buffered: true });
      } catch { /* Browser does not expose longtask in every mode. */ }
    }
  });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openList(page);
  await page.waitForTimeout(300);
  const interactionMs = await page.evaluate(async () => {
    const button = document.querySelector<HTMLButtonElement>(".icon-button[aria-label*='theme']");
    if (!button) throw new Error("Theme control is not present");
    const started = performance.now();
    button.click();
    await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    return performance.now() - started;
  });
  const metrics = await page.evaluate(() => (window as unknown as { __cloudopsMetrics: { cls: number; longTasks: number[] } }).__cloudopsMetrics);
  expect(interactionMs).toBeLessThan(100);
  expect(metrics.cls).toBeLessThan(0.1);
  console.log(JSON.stringify({ interactionMs, cls: metrics.cls, maxLongTaskMs: Math.max(0, ...metrics.longTasks) }));
});
