import { expect, test } from "@playwright/test";

test("Monitoring context navigation renders Logs and Traces workspaces", async ({ page }) => {
  await page.goto("/monitoring", { waitUntil: "domcontentloaded" });
  await expect(page.locator("#monitoring-heading")).toBeVisible();

  await page.getByRole("link", { name: "日志", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "日志分析" })).toBeVisible();

  await page.getByRole("link", { name: "链路", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "链路分析" })).toBeVisible();
});
