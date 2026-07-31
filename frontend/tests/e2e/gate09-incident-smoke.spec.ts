import { expect, test, type APIRequestContext } from "@playwright/test";

import { incidentID, monitorBrowser } from "./support";

const fixtureOrigin = "http://127.0.0.1:18147";
const offlineIconFailures = process.env.CLOUDOPS_E2E_OFFLINE === "1"
  ? [/https:\/\/api\.(?:iconify\.design|unisvg\.com|simplesvg\.com)\/lucide\.json/]
  : [];

async function configureFixture(request: APIRequestContext) {
  const response = await request.get(`${fixtureOrigin}/fixture/config?reset=1&list=ready&sections=ready&verification=passed&sse=connected`);
  expect(response.ok()).toBeTruthy();
}

test.beforeEach(async ({ page, request }) => {
  await page.clock.setFixedTime(new Date("2026-07-23T04:00:00.000Z"));
  await page.emulateMedia({ colorScheme: "light" });
  await page.setViewportSize({ width: 1440, height: 900 });
  await configureFixture(request);
});

test("Gate 9 Incident list keeps URL sort state and opens the read-only Inspector", async ({ page }) => {
  const browser = monitorBrowser(page, {
    allowedFailures: [/\/events: net::ERR_ABORTED$/, ...offlineIconFailures],
  });
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (!["GET", "HEAD", "OPTIONS"].includes(request.method())) mutations.push(`${request.method()} ${request.url()}`);
  });

  await page.goto("/incidents?sort=severity&direction=asc", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { level: 1, name: "Incident" })).toBeVisible();
  await expect(page.getByTestId("incident-results")).toBeVisible();
  await expect(page.getByTestId("incident-row-summary").first()).toBeVisible();
  await expect(page).toHaveURL(/sort=severity/);
  await expect(page).toHaveURL(/direction=asc/);

  await page.locator(".dense-data-table-row").filter({ hasText: "Checkout API recovery" }).click();
  await expect(page).toHaveURL(/selected=/);
  await expect(page.getByTestId("incident-inspector")).toBeVisible();
  await expect(page.getByRole("link", { name: "打开完整 Incident 详情" })).toBeVisible();

  await page.goBack();
  await expect(page).not.toHaveURL(/selected=/);
  await expect(page).toHaveURL(/sort=severity/);
  await expect(page).toHaveURL(/direction=asc/);
  await expect(page.getByTestId("incident-inspector")).not.toBeVisible();

  expect(mutations).toEqual([]);
  browser.expectClean();
});

test("Gate 9 Incident detail renders the seven-stage single-owner lifecycle", async ({ page }) => {
  const browser = monitorBrowser(page, {
    allowedFailures: [/\/events: net::ERR_ABORTED$/, ...offlineIconFailures],
  });
  const mutations: string[] = [];
  page.on("request", (request) => {
    if (!["GET", "HEAD", "OPTIONS"].includes(request.method())) mutations.push(`${request.method()} ${request.url()}`);
  });

  await page.goto(`/incidents/${incidentID}`, { waitUntil: "domcontentloaded" });
  await expect(page.locator(".incident-detail-view h1")).toBeVisible();
  const zoneLinks = page.getByRole("navigation", { name: "Incident detail zones" }).getByRole("link");
  await expect(zoneLinks).toHaveCount(7);
  await expect(zoneLinks).toHaveText(["01 Agent 调查", "02 Evidence", "03 Approval", "04 Delivery", "05 Verification", "06 Timeline", "07 Resolution"]);
  await expect(page.locator("#approval").getByRole("heading", { name: "Remediation Plan & Approval" })).toBeVisible();
  await expect(page.locator("#delivery").getByRole("heading", { name: "Delivery Rail" })).toBeVisible();
  await expect(page.locator("#verification").getByRole("heading", { name: "恢复验证" })).toBeVisible();

  await zoneLinks.filter({ hasText: "Delivery" }).click();
  await expect(page).toHaveURL(/#delivery$/);

  expect(mutations).toEqual([]);
  browser.expectClean();
});
