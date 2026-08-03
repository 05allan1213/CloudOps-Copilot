import { expect, test, type Page } from "@playwright/test";

import {
  configureFixture,
  expectHeadingOrder,
  expectNoLayoutOverflow,
  fixtureOrigin,
  incidentID,
  monitorBrowser,
} from "./support";

test.beforeEach(async ({ page }) => {
  await page.clock.setFixedTime(new Date("2026-07-23T04:00:00.000Z"));
});

async function openList(page: Page, marker: string) {
  await page.goto(`/incidents?e2e=${marker}`, { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { level: 1, name: "Incident" })).toBeVisible();
}

async function openDetail(page: Page, marker: string, hash = "") {
  await page.goto(`/incidents/${incidentID}?e2e=${marker}${hash}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".incident-detail-view h1")).toBeVisible();
  await expect(page.locator(".incident-detail-view h1")).toBeFocused();
}

test("Incident list preserves keyboard entry, URL filters, sorting, and Back state", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/\/events: net::ERR_ABORTED$/] });
  const mutations: string[] = [];
  page.on("request", (outgoing) => {
    if (!new Set(["GET", "HEAD", "OPTIONS"]).has(outgoing.method())) mutations.push(`${outgoing.method()} ${outgoing.url()}`);
  });

  await configureFixture(request, { list: "ready" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openList(page, "list-contract");
  await expect(page.getByTestId("incident-results").locator("tbody tr")).toHaveCount(3);

  await expect.poll(() => page.evaluate(() => {
    const selector = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";
    const first = [...document.querySelectorAll<HTMLElement>(selector)].find((element) =>
      element.tabIndex >= 0 && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== "hidden");
    return first?.classList.contains("skip-link") ?? false;
  })).toBe(true);
  await page.locator(".skip-link").focus();
  await expect(page.locator(".skip-link")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("app-main")).toBeFocused();

  await page.getByLabel("状态").selectOption("resolved");
  await page.getByLabel("Attention").selectOption("false");
  await page.getByLabel("服务").fill("checkout-api");
  await page.getByTestId("incident-filter-apply").click();
  await expect(page).toHaveURL(/status=resolved/);
  await expect(page).toHaveURL(/attention=false/);
  await expect(page).toHaveURL(/service=checkout-api/);

  const updatedHeader = page.getByRole("columnheader", { name: /更新/ });
  await expect(updatedHeader).toHaveAttribute("aria-sort", "descending");
  await updatedHeader.getByRole("button").click();
  await expect(updatedHeader).toHaveAttribute("aria-sort", "ascending");

  await page.locator(`[data-testid="incident-row-link"][href="/incidents/${incidentID}"]`).click();
  await expect(page.locator(".incident-detail-view h1")).toBeFocused();
  await page.goBack();
  await expect(page.getByTestId("incident-results").locator("tbody tr")).toHaveCount(3);
  await expect(page.getByLabel("状态")).toHaveValue("resolved");
  await expect(page.getByLabel("Attention")).toHaveValue("false");
  await expect(page.getByLabel("服务")).toHaveValue("checkout-api");

  expect(mutations).toEqual([]);
  browser.expectClean();
});

test("Incident list exposes loading, empty, forbidden, and unavailable states", async ({ page, request }) => {
  await configureFixture(request, { list: "loading" });
  await openList(page, "list-loading");
  await expect(page.locator(".incident-skeleton")).toBeVisible();
  await expect(page.getByTestId("incident-results")).toBeVisible({ timeout: 5_000 });

  await configureFixture(request, { list: "empty" });
  await openList(page, "list-empty");
  await expect(page.locator(".state-block").getByRole("heading", { name: "没有 Incident" })).toBeVisible();

  await configureFixture(request, { list: "forbidden" });
  await openList(page, "list-forbidden");
  await expect(page.locator(".state-block").getByRole("heading", { name: "请求被拒绝" })).toBeVisible();
  await expect(page.locator(".state-block .request-identity")).toContainText("fixture-request-403");

  await configureFixture(request, { list: "error" });
  await openList(page, "list-unavailable");
  await expect(page.locator(".state-block").getByRole("heading", { name: "Incident 投影暂不可用" })).toBeVisible();
  await expect(page.locator(".state-block .request-identity")).toContainText("fixture-request-503");
});

test("Incident cursor pagination and long content remain stable on desktop widths", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/\/api\/v1\/notification-events: net::ERR_ABORTED$/] });
  await configureFixture(request, { list: "paginated" });
  await openList(page, "list-pagination");
  const rows = page.getByTestId("incident-results").locator("tbody tr");
  await expect(rows).toHaveCount(20);
  await page.getByRole("button", { name: "加载更多 Incident" }).click();
  await expect(rows).toHaveCount(50);

  const popupPromise = page.context().waitForEvent("page");
  await page.getByTestId("incident-row-link").first().click({ modifiers: ["Control"] });
  const popup = await popupPromise;
  await popup.waitForLoadState("domcontentloaded");
  await expect(popup).toHaveURL(new RegExp(`/incidents/${incidentID}`));
  await popup.close();

  for (const [width, height] of [[1024, 768], [1280, 800], [1440, 900], [1920, 1080]]) {
    await page.setViewportSize({ width, height });
    await page.waitForTimeout(300);
    await expectNoLayoutOverflow(page);
  }

  await configureFixture(request, { list: "long" });
  await openList(page, "list-long-content");
  await expect(page.getByTestId("incident-results").locator("tbody tr")).toHaveCount(20);
  await expectNoLayoutOverflow(page);
  browser.expectClean();
});

test("Incident detail renders typed projections, context links, and the four-zone contract", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/\/events: net::ERR_ABORTED$/] });
  const mutations: string[] = [];
  page.on("request", (outgoing) => {
    if (!new Set(["GET", "HEAD", "OPTIONS"]).has(outgoing.method())) mutations.push(`${outgoing.method()} ${outgoing.url()}`);
  });

  await configureFixture(request, { sections: "ready", verification: "passed", sse: "connected" });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await openDetail(page, "detail-contract");

  await expect(page.getByRole("navigation", { name: "Incident detail zones" }).getByRole("link")).toHaveCount(4);
  await expect(page.locator(".context-links a")).toHaveCount(6);
  await expect(page.locator("#related-alerts li")).toHaveCount(1);
  await expect(page.locator("#timeline li")).toHaveCount(1);
  await expect(page.locator("#investigations li")).toHaveCount(1);
  await expect(page.locator("#evidence li")).toHaveCount(1);
  await expect(page.locator("#decision .decision-record")).toBeVisible();
  await expect(page.locator("#verifications .verification-run")).toBeVisible();
  await expect(page.locator("#resolution-report .report")).toBeVisible();
  await expectHeadingOrder(page);
  await expectNoLayoutOverflow(page);

  await page.getByRole("navigation", { name: "Incident detail zones" }).getByRole("link", { name: /调查/ }).click();
  await expect(page).toHaveURL(/#investigation-zone$/);
  await page.getByRole("link", { name: "返回 Incident 列表" }).click();
  await expect(page.getByTestId("incident-results")).toBeVisible();

  expect(mutations).toEqual([]);
  browser.expectClean();
});

test("Incident detail keeps empty, failed, deep-link, and missing projections explicit", async ({ page, request }) => {
  await configureFixture(request, { sections: "empty", verification: "not_run", sse: "connected" });
  await openDetail(page, "detail-empty", "#recovery-zone");
  await expect(page).toHaveURL(/#recovery-zone$/);
  await expect(page.locator("#related-alerts .section-message")).toBeVisible();
  await expect(page.locator("#decision .section-message")).toBeVisible();
  await expect(page.locator("#verifications .section-message")).toContainText("NOT RUN");
  await expect(page.locator("#resolution-report .section-message")).toBeVisible();
  await expect(page.locator("#resolution-report .report")).toHaveCount(0);

  await configureFixture(request, { sections: "error", verification: "passed", sse: "connected" });
  await openDetail(page, "detail-projection-error");
  await expect(page.locator("#evidence").getByRole("alert")).toBeVisible();
  await expect(page.locator("#evidence .request-identity")).toContainText("fixture-request-503");

  await page.goto("/incidents/not-a-public-id?e2e=detail-missing", { waitUntil: "domcontentloaded" });
  await expect(page.locator(".state-block").getByRole("heading", { name: "未找到投影" })).toBeVisible();
  await expect(page.locator(".state-block .request-identity")).toContainText("fixture-request-404");
});

test("finite Incident SSE deduplicates events, resumes the cursor, and appends durable timeline data", async ({ page, request }) => {
  const browser = monitorBrowser(page);
  await configureFixture(request, { sse: "finite", sections: "ready", verification: "passed" });
  await openDetail(page, "finite-sse");

  await expect(page.locator("#timeline li")).toHaveCount(2, { timeout: 8_000 });
  await expect(page.locator(".realtime-chip")).toContainText("实时");
  await expect.poll(async () => {
    const response = await request.get(`${fixtureOrigin}/fixture/metrics`);
    return (await response.json()).eventConnections as number;
  }, { timeout: 8_000 }).toBeGreaterThanOrEqual(2);
  const metrics = await (await request.get(`${fixtureOrigin}/fixture/metrics`)).json();
  expect(metrics.lastEventID).toBe("00000095-0000-4000-8000-000000000001");
  expect(metrics.timelineRequests).toBe(2);
  browser.expectClean();
});

test("Incident SSE reconnect preserves the projection and returns to realtime", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/503.*\/events/] });
  await configureFixture(request, { sse: "reconnect", sections: "ready", verification: "passed" });
  await openDetail(page, "reconnect-sse");
  await expect(page.locator(".realtime-chip")).toContainText("重连中", { timeout: 4_000 });
  await expect(page.locator(".realtime-chip")).toContainText("实时", { timeout: 6_000 });
  const metrics = await (await request.get(`${fixtureOrigin}/fixture/metrics`)).json();
  expect(metrics.eventConnections).toBeGreaterThanOrEqual(2);
  expect(metrics.sseAttempts).toBeGreaterThanOrEqual(2);
  browser.expectClean();
});

test("theme, reduced motion, contrast, and desktop density remain deterministic", async ({ page, request }) => {
  await configureFixture(request, { list: "loading" });
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await openList(page, "theme-motion");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator(".skeleton-row span").first()).toBeVisible();
  await expect.poll(() => page.locator(".skeleton-row span").first().evaluate((element) => getComputedStyle(element).animationName)).toBe("none");

  await page.getByRole("button", { name: "切换浅色主题" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await expect(page.getByTestId("incident-results")).toBeVisible({ timeout: 5_000 });

  const contrast = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement);
    const parse = (value: string) => {
      const hex = value.trim().replace(/^#/, "");
      return [0, 2, 4].map((offset) => Number.parseInt(hex.slice(offset, offset + 2), 16) / 255);
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
    };
  });
  expect(contrast.primary).toBeGreaterThanOrEqual(4.5);
  expect(contrast.secondary).toBeGreaterThanOrEqual(4.5);
});
