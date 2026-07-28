import { expect, test } from "@playwright/test";

test("Monitoring context navigation renders Logs and Traces workspaces", async ({ page }) => {
  await page.goto("/monitoring", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#monitoring-heading")).toBeVisible();

  await page.getByRole("link", { name: "日志", exact: true }).click();
  await expect(page.locator("#logs-heading")).toBeVisible();

  await page.getByRole("link", { name: "链路", exact: true }).click();
  await expect(page.locator("#traces-heading")).toBeVisible();
});
