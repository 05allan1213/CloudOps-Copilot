import { expect, test } from "@playwright/test";

import { expectCleanPage, expectNoPageOverflow, observePage } from "./evidence";

test("critical read-only shell, theme and Inspector flow works", async ({ page }, testInfo) => {
  const diagnostics = observePage(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto("/incidents");
  await expect(page.getByRole("heading", { level: 1, name: "Incident 操作面" })).toBeVisible();
  await expect(page.getByTestId("incident-table")).toBeVisible();

  await page.getByTestId("theme-toggle").click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  const trigger = page.locator('[data-testid^="inspect-inc-"]').first();
  await trigger.click();
  const inspector = page.getByRole("dialog").filter({ hasText: "Exact hash" });
  await expect(inspector).toBeVisible();
  await page.getByRole("button", { name: "关闭", exact: true }).click();
  await expect(inspector).toHaveCount(0);
  await expect(trigger).toBeFocused();

  await page.goto("/monitoring");
  await expect(page.getByTestId("monitor-chart")).toBeVisible();
  await expect(page.getByTestId("monitor-partial")).toBeVisible();
  await expectNoPageOverflow(page);
  await expectCleanPage(diagnostics, testInfo);
});
