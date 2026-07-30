import { expect, test } from "@playwright/test";

import { configureFixture, monitorBrowser } from "./support";

test.beforeEach(async ({ page }) => {
  await page.clock.setFixedTime(new Date("2026-07-23T04:00:00.000Z"));
});

test("stable Incident read path preserves keyboard, URL, and back state", async ({ page, request }) => {
  const browser = monitorBrowser(page, { allowedFailures: [/\/events: (?:net::ERR_ABORTED|NS_BINDING_ABORTED)$/] });
  const mutations: string[] = [];
  page.on("request", (outgoing) => {
    if (!new Set(["GET", "HEAD", "OPTIONS"]).has(outgoing.method())) mutations.push(`${outgoing.method()} ${outgoing.url()}`);
  });
  await configureFixture(request, { list: "ready" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/incidents?e2e=stable-readonly", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect.poll(() => page.evaluate(() => {
    const selector = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";
    const first = [...document.querySelectorAll<HTMLElement>(selector)].find((element) =>
      element.tabIndex >= 0 && element.getClientRects().length > 0 && getComputedStyle(element).visibility !== "hidden");
    return first?.classList.contains("skip-link") ?? false;
  })).toBe(true);
  await page.locator(".skip-link").focus();
  await expect(page.locator(".skip-link")).toBeFocused();
  await page.keyboard.press("Enter");
  await expect.poll(() => page.getByTestId("app-main").evaluate((main) => main.contains(document.activeElement))).toBe(true);

  await expect(page.getByTestId("incident-results").locator("tbody tr")).toHaveCount(3);
  await page.getByLabel("服务").fill("checkout-api");
  await page.getByTestId("incident-filter-apply").click();
  await expect(page).toHaveURL(/service=checkout-api/);

  await page.getByTestId("incident-row-link").first().click();
  await expect(page.locator(".incident-detail-view h1")).toBeFocused();
  await page.goBack();
  await expect(page.getByTestId("incident-results").locator("tbody tr")).toHaveCount(3);
  await expect(page.getByLabel("服务")).toHaveValue("checkout-api");
  expect(mutations).toEqual([]);
  browser.expectClean();
});

test("closed Global Agent stays idle while Notification SSE remains independent", async ({ page }) => {
  const requests: string[] = [];
  const mutations: string[] = [];
  page.on("request", (outgoing) => {
    requests.push(outgoing.url());
    if (!new Set(["GET", "HEAD", "OPTIONS"]).has(outgoing.method())) mutations.push(`${outgoing.method()} ${outgoing.url()}`);
  });

  await page.goto("/incidents?e2e=agent-lifecycle", { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(500);
  expect(requests.some((url) => url.includes("/api/v1/agent/"))).toBe(false);
  expect(requests.some((url) => url.includes("/api/v1/notification-events"))).toBe(true);

  await page.getByRole("button", { name: "打开全局 Agent 面板" }).click();
  await expect(page.getByTestId("global-agent-drawer")).toBeVisible();
  await expect.poll(() => requests.some((url) => url.includes("/api/v1/agent/"))).toBe(true);
  await page.getByRole("button", { name: "关闭 Agent 面板" }).click();
  await expect(page.getByTestId("global-agent-drawer")).toBeHidden();
  expect(mutations).toEqual([]);
});
