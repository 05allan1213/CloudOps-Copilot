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

async function openList(page: Page, marker = "list") {
  await page.goto(`/incidents?e2e=${marker}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("#incident-list-title")).toBeVisible();
}

async function openDetail(page: Page, marker = "detail") {
  await page.goto(`/incidents/${incidentID}?e2e=${marker}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".incident-detail-view")).toBeVisible();
  await expect(page.locator(".incident-detail-view h1")).toBeVisible();
}

async function submitDecision(page: Page, decision: "approved" | "rejected" = "approved", reason = "Exact persisted evidence supports this bounded fixture decision") {
  await page.locator(decision === "approved" ? ".approve-button" : ".reject-button").click();
  const dialog = page.locator("dialog.decision-dialog");
  await expect(dialog).toHaveAttribute("open", "");
  await dialog.locator("textarea[name=decision_reason]").fill(reason);
  await dialog.locator(".submit-decision").click();
  return dialog;
}

test("Incident List is keyboard navigable and URL-synced", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/\/events: net::ERR_ABORTED$/] });
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
  await page.locator('.desktop-zone-list a[href="#investigation-zone"]').click();
  await expect(page).toHaveURL(/#investigation-zone$/);
  await expect(page.locator('.desktop-zone-list a[aria-current="location"]')).toHaveAttribute("href", "#investigation-zone");
  await page.goBack();
  await expect(page.locator("#incident-list-title")).toBeVisible();
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

test("session expiry redirects to OAuth and non-401 failures render a recoverable boundary", async ({ page, request }) => {
  await configureFixture(request, { session: "expired" });
  await page.goto("/incidents?e2e=session-expired", { waitUntil: "domcontentloaded" }).catch(() => undefined);
  await expect(page).toHaveURL(/\/oauth2\/start\?rd=%2Fincidents%3Fe2e%3Dsession-expired/);
  const sensitiveStorageKeys = await page.evaluate(() => Object.keys(localStorage).filter((key) => /auth|jwt|token/i.test(key)));
  expect(sensitiveStorageKeys).toEqual([]);

  await configureFixture(request, { session: "forbidden" });
  await page.goto("/incidents?e2e=session-forbidden", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: "Workbench access is forbidden" })).toBeVisible();
  await expect(page.locator(".auth-boundary .request-identity")).toContainText("fixture-request-403");
  await expect(page.getByRole("button", { name: "Retry Session" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Re-authenticate" })).toBeVisible();
  await expectNoLayoutOverflow(page);

  await configureFixture(request, { session: "error" });
  await page.getByRole("button", { name: "Retry Session" }).click();
  await expect(page.getByRole("heading", { name: "Workbench session unavailable" })).toBeVisible();
  await expect(page.locator(".auth-boundary .request-identity")).toContainText("fixture-request-503");
});

test("Incident List covers edge datasets, cursor append, native links, and back restoration", async ({ page, request, context }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  for (const [mode, count] of [["one", 1], ["twenty", 20], ["fifty", 50]] as const) {
    await configureFixture(request, { list: mode });
    await openList(page, `list-${mode}`);
    await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(count);
  }

  await configureFixture(request, { list: "long" });
  await openList(page, "list-long");
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(20);
  await expect(page.locator(".desktop-table-wrap tbody tr").first()).toContainText("结账服务");
  await expectNoLayoutOverflow(page);
  await page.setViewportSize({ width: 414, height: 896 });
  await expectNoLayoutOverflow(page);

  await page.setViewportSize({ width: 1440, height: 900 });
  await configureFixture(request, { list: "paginated" });
  await openList(page, "list-paginated");
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(20);
  await page.getByRole("button", { name: "Load More Incidents" }).click();
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(50);
  await expect(page).toHaveURL(/cursor=00000001-0000-4000-8000-000000000020/);

  const firstIncident = page.locator(".desktop-table-wrap tbody a").first();
  await expect(firstIncident).toHaveAttribute("href", new RegExp(`^/incidents/${incidentID}$`));
  const popupPromise = context.waitForEvent("page");
  await firstIncident.click({ modifiers: ["Control"] });
  const popup = await popupPromise;
  await popup.waitForLoadState("domcontentloaded");
  await expect(popup).toHaveURL(new RegExp(`/incidents/${incidentID}`));
  await popup.close();

  const main = page.locator("#incident-content");
  await main.evaluate((element) => { element.scrollTop = 360; });
  await firstIncident.click();
  await expect(page.locator(".incident-detail-view h1")).toBeFocused();
  await page.goBack();
  await expect(page.locator(".desktop-table-wrap tbody tr")).toHaveCount(50);
  await expect.poll(() => main.evaluate((element) => element.scrollTop)).toBeGreaterThan(300);
});

test("Incident List timeout keeps a stable retryable state", async ({ page, request }) => {
  test.setTimeout(25_000);
  await configureFixture(request, { list: "timeout" });
  await openList(page, "list-timeout");
  await expect(page.locator(".incident-skeleton")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Incidents could not be loaded" })).toBeVisible({ timeout: 15_000 });
  await expect(page.locator(".state-block")).toContainText("Request timed out");
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

  await page.locator('.desktop-zone-list a[href="#investigation-zone"]').click();
  await expect(page).toHaveURL(/#investigation-zone$/);
  const inspectEvidence = page.locator(".evidence-desktop .inspect-button").first();
  await inspectEvidence.click();
  await expect(page).toHaveURL(/evidence=00000040-0000-4000-8000-000000000001#investigation-zone$/);
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

  const zonePopupPromise = page.context().waitForEvent("page");
  await page.locator('.desktop-zone-list a[href="#remediation-delivery"]').click({ modifiers: ["Control"] });
  const zonePopup = await zonePopupPromise;
  await zonePopup.waitForLoadState("domcontentloaded");
  await expect(zonePopup).toHaveURL(/#remediation-delivery$/);
  await zonePopup.close();

  for (const [width, height] of [[320, 812], [375, 812], [414, 896], [667, 375], [720, 900], [768, 900], [1024, 768], [1440, 1000]]) {
    await page.setViewportSize({ width, height });
    await page.waitForTimeout(250);
    await expectNoLayoutOverflow(page);
  }

  await page.setViewportSize({ width: 375, height: 812 });
  await expectMinimumTarget(page, ".mobile-navigation-trigger, .icon-button, #incident-zone-select, .approve-button, .reject-button");
  const mobileControls = await page.locator("input:visible, textarea:visible, select:visible").evaluateAll((elements) => elements.map((element) => ({
    height: element.getBoundingClientRect().height,
    fontSize: Number.parseFloat(getComputedStyle(element).fontSize),
  })));
  for (const control of mobileControls) {
    expect(control.height).toBeGreaterThanOrEqual(44);
    expect(control.fontSize).toBeGreaterThanOrEqual(16);
  }
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

test("Investigation and Evidence preserve every frozen presentation state", async ({ page, request }) => {
  await configureFixture(request, { sections: "states", sse: "connected" });
  await openDetail(page, "resource-states");

  const runOptions = page.locator("#agent-run-select option");
  await expect(runOptions).toHaveCount(6);
  for (const label of ["Pending", "Running", "Diagnosed", "Insufficient", "Failed", "Cancelled"]) {
    await expect(runOptions.filter({ hasText: label })).toHaveCount(1);
  }

  await expect(page.locator(".evidence-desktop tbody tr")).toHaveCount(6);
  for (const label of ["Available", "Partial", "No Data", "Unavailable", "Invalid", "Superseded"]) {
    await expect(page.locator(".evidence-desktop tbody")).toContainText(label);
  }
  await page.locator(".evidence-desktop .inspect-button").nth(2).click();
  await expect(page.locator("dialog.evidence-drawer")).toContainText("contains no source data");
  await page.keyboard.press("Escape");
});

test("Timeline cursor appends more than 200 persisted events without replacement", async ({ page, request }) => {
  test.setTimeout(45_000);
  await configureFixture(request, { sections: "paged", sse: "connected" });
  await openDetail(page, "timeline-paged");
  const events = page.locator("#timeline .timeline-rail > li");
  await expect(events).toHaveCount(100);
  await page.locator("#timeline").getByRole("button", { name: "Load More Timeline Events" }).click();
  await expect(events).toHaveCount(200);
  await page.locator("#timeline").getByRole("button", { name: "Load More Timeline Events" }).click();
  await expect(events).toHaveCount(205);
  await expect(events.first()).toContainText("001");
  await expect(events.last()).toContainText("205");
});

test("finite SSE batches resume with Last-Event-ID and preserve focus and scroll", async ({ page, request }) => {
  const browser = monitorBrowser(page);
  await configureFixture(request, { sse: "finite", sections: "ready" });
  await openDetail(page, "finite-sse");
  const inspectEvidence = page.locator(".evidence-desktop .inspect-button").first();
  await inspectEvidence.focus();
  const main = page.locator("#incident-content");
  await main.evaluate((element) => { element.scrollTop = 420; });

  await expect(page.locator("#timeline .timeline-rail > li")).toHaveCount(2, { timeout: 8_000 });
  await expect(inspectEvidence).toBeFocused();
  await expect.poll(() => main.evaluate((element) => element.scrollTop)).toBeGreaterThan(400);
  await expect(page.locator(".realtime-chip")).toContainText("Live");
  await expect.poll(async () => {
    const response = await request.get("http://127.0.0.1:18082/fixture/metrics");
    const metrics = await response.json();
    return metrics.eventConnections;
  }, { timeout: 8_000 }).toBeGreaterThanOrEqual(2);
  const metricsResponse = await request.get("http://127.0.0.1:18082/fixture/metrics");
  const metrics = await metricsResponse.json();
  expect(metrics.lastEventID).toBe("00000095-0000-4000-8000-000000000001");
  expect(metrics.timelineRequests).toBe(2);
  browser.expectClean();
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

test("operator command contract presents 202, 403, 409, and 422 without unsafe retries", async ({ page, request }) => {
  await configureFixture(request, { command: "202", role: "operator", sse: "connected" });
  await openDetail(page, "command-202");
  const dialog = await submitDecision(page, "rejected", "The persisted evidence does not support the proposed exact change.");
  await expect(page.locator(".persisted-decision")).toContainText("rejected");
  await expect(page.locator(".command-feedback--accepted").first()).toContainText("202");
  const acceptedMetrics = await (await request.get("http://127.0.0.1:18082/fixture/metrics")).json();
  expect(acceptedMetrics.commands[0].csrfToken).toBe("fixture-csrf-token");
  expect(acceptedMetrics.commands[0].idempotencyKey.length).toBeLessThanOrEqual(128);
  expect(acceptedMetrics.commands[0].body).toMatchObject({ decision: "rejected", expected_version: 5 });
  await expect(dialog).not.toHaveAttribute("open", "");

  for (const [mode, state, code] of [["403", "forbidden", "COMMAND_FORBIDDEN"], ["409", "conflict", "STALE_EXPECTATION"], ["422", "invalid", "INVALID_TRANSITION"]] as const) {
    await configureFixture(request, { command: mode, role: "operator", sse: "connected" });
    await openDetail(page, `command-${mode}`);
    const failedDialog = await submitDecision(page);
    await expect(page.locator(`.command-feedback--${state}`).first()).toBeVisible();
    await expect(page.locator(".command-feedback").first()).toContainText(code);
    await expect(page.locator(".approve-button")).toBeDisabled();
    if (mode === "409") {
      await expect(page.locator(".command-feedback--conflict").first()).toContainText("Refresh the current projection before trying again");
      await failedDialog.getByRole("button", { name: "Refresh Current Projection" }).click();
      await expect(page.locator(".command-feedback")).toHaveCount(0);
      await expect(page.locator(".approve-button")).toBeEnabled();
    }
  }
});

test("expired and stale Plans fail closed before a Command is submitted", async ({ page, request }) => {
  for (const [mode, message] of [["expired", "Plan Expired"], ["stale", "Plan Version Is Stale"]] as const) {
    await configureFixture(request, { plan: mode, command: "202", role: "operator", sse: "connected" });
    await openDetail(page, `plan-${mode}`);
    await expect(page.getByRole("status").filter({ hasText: message })).toBeVisible();
    await expect(page.locator(".approve-button")).toBeDisabled();
    const metrics = await (await request.get("http://127.0.0.1:18082/fixture/metrics")).json();
    expect(metrics.commands).toHaveLength(0);
  }
});

test("verification states stay distinct and only a passing latest run exposes ResolutionReport", async ({ page, request }) => {
  const cases = [
    ["failed", "Failed", 0],
    ["timed_out", "Timed out", 0],
    ["inconclusive", "Inconclusive", 0],
    ["not_run", "NOT RUN", 0],
    ["passed", "Passed", 1],
  ] as const;
  for (const [mode, label, reports] of cases) {
    await configureFixture(request, { verification: mode, sections: "ready", sse: "connected" });
    await openDetail(page, `verification-${mode}`);
    await expect(page.locator("#verifications")).toContainText(label);
    await expect(page.locator(".report")).toHaveCount(reports);
  }
});

test("no-change recovery has no Plan or Delivery while retaining a passing ResolutionReport", async ({ page, request }) => {
  await configureFixture(request, { verification: "no_change", sections: "ready", sse: "connected" });
  await openDetail(page, "no-change");
  await expect(page.locator("#remediation-plans .section-message")).toContainText("No immutable remediation Plan");
  await expect(page.locator("#delivery .section-message")).toContainText("No persisted Delivery");
  await expect(page.locator(".no-change-banner")).toHaveCount(2);
  await expect(page.locator(".no-change-banner").first()).toContainText("No-change verification path");
  await expect(page.locator(".report")).toHaveCount(1);
  await expect(page.locator(".approval-panel")).toHaveCount(0);
});

test("command timeout remains pending, then becomes retryable with the same identity", async ({ page, request }) => {
  test.setTimeout(30_000);
  await configureFixture(request, { command: "timeout", role: "operator", sse: "connected" });
  await openDetail(page, "command-timeout");
  const dialog = await submitDecision(page);
  await expect(dialog.locator(".submit-decision")).toBeDisabled();
  await expect(dialog.locator(".submit-decision")).toHaveText("Submitting…");
  await expect(dialog.locator(".command-feedback--error")).toBeVisible({ timeout: 15_000 });
  await expect(dialog.getByRole("button", { name: "Retry With Same Idempotency Key" })).toBeVisible();
  const metrics = await (await request.get("http://127.0.0.1:18082/fixture/metrics")).json();
  expect(metrics.commands).toHaveLength(1);
});

test("themes, motion, contrast, zoom-equivalent width, and visual baselines remain stable", async ({ page, request }) => {
  await configureFixture(request, { sse: "connected" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "no-preference" });
  await openList(page);
  await expect(page.locator('meta[name="viewport"]')).toHaveAttribute("content", /viewport-fit=cover/);
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
  await page.evaluate(() => { document.documentElement.style.zoom = "2"; });
  await expectNoLayoutOverflow(page);
  await page.evaluate(() => { document.documentElement.style.zoom = ""; });
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
