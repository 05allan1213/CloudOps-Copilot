import { expect, test } from "@playwright/test";

import { expectCleanPage, expectNoPageOverflow, observePage } from "./evidence";

test.describe("prototype interaction contracts", () => {
  test("Incident URL, history, unavailable targets and dirty Inspector remain truthful", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/incidents?severity=critical&page=2&from=2026-07-30T07%3A00%3A00Z&to=2026-07-30T08%3A00%3A00Z");
    await expect(page.getByRole("heading", { level: 1, name: "Incident 操作面" })).toBeFocused();
    await expect(page).toHaveURL(/severity=critical/);
    await expect(page).toHaveURL(/page=2/);

    await page.setViewportSize({ width: 1440, height: 480 });
    const firstTrigger = page.locator('[data-testid^="inspect-inc-"]').last();
    await firstTrigger.scrollIntoViewIfNeeded();
    const scrollBeforeInspector = await page.evaluate(() => Math.max(window.scrollY, document.querySelector<HTMLElement>("#main-content")?.scrollTop ?? 0));
    expect(scrollBeforeInspector).toBeGreaterThan(0);
    const firstId = (await firstTrigger.getAttribute("data-testid"))?.replace("inspect-", "") ?? "";
    await firstTrigger.click();
    await expect(page).toHaveURL(new RegExp(`selected=${firstId}`));
    const inspector = page.getByRole("dialog").filter({ hasText: firstId });
    await expect(inspector).toBeVisible();

    await page.getByTestId("inspector-dirty").click();
    await page.getByRole("button", { name: "关闭", exact: true }).click();
    const dirtyDialog = page.getByRole("dialog", { name: "放弃未保存的 Inspector 编辑？" });
    await expect(dirtyDialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(dirtyDialog).toBeVisible();
    await page.getByTestId("continue-editing").click();
    await expect(inspector).toBeVisible();

    await page.getByRole("button", { name: "关闭", exact: true }).click();
    await page.getByTestId("discard-inspector").click();
    await expect(page).not.toHaveURL(/selected=/);
    await expect(firstTrigger).toBeFocused();
    await expect(page).toHaveURL(/severity=critical/);
    await expect(page).toHaveURL(/page=2/);
    await expect.poll(async () => page.evaluate(() => Math.max(window.scrollY, document.querySelector<HTMLElement>("#main-content")?.scrollTop ?? 0))).toBe(scrollBeforeInspector);

    await page.setViewportSize({ width: 1440, height: 900 });
    await firstTrigger.click();
    const secondTrigger = page.locator('[data-testid^="inspect-inc-"]').nth(2);
    const secondId = (await secondTrigger.getAttribute("data-testid"))?.replace("inspect-", "") ?? "";
    await page.getByTestId("inspector-previous").click();
    await expect(page).toHaveURL(new RegExp(`selected=${secondId}`));
    await page.goBack();
    await expect(page).not.toHaveURL(/selected=/);
    await page.goForward();
    await expect(page).toHaveURL(new RegExp(`selected=${secondId}`));
    await expect(page.getByRole("dialog").filter({ hasText: secondId })).toBeVisible();
    await page.reload();
    await expect(page).toHaveURL(new RegExp(`selected=${secondId}`));
    await expect(page.getByRole("dialog").filter({ hasText: secondId })).toBeVisible();

    await page.goto("/incidents?selected=inc-deleted&q=payments&page=2");
    await expect(page.getByTestId("incident-unavailable")).toContainText("Incident 已删除");
    await expect(page).toHaveURL(/q=payments/);
    await page.goto("/incidents?selected=inc-denied&access=denied&q=payments&page=2");
    await expect(page.getByTestId("incident-unavailable")).toContainText("Permission Denied");
    await page.goto("/incidents/not-a-real-id?q=payments&page=2");
    await expect(page.getByTestId("incident-detail-unavailable")).toContainText("Incident ID 无效");
    await expect(page).toHaveURL(/q=payments/);
    await page.goto(`/incidents?incident=${firstId}&workload=cloudops-api&compat=legacy-query`);
    await expect(page.getByRole("dialog").filter({ hasText: firstId })).toBeVisible();
    await expect(page).toHaveURL(/workload=cloudops-api/);

    await page.goto(`/incidents/${firstId}`);
    await expect(page.getByRole("region", { name: "完整 Incident 工作页" })).toBeVisible();
    await expect(page.getByText("Verified", { exact: true })).toBeVisible();
    await expect(page.getByText("尚无当前 Verification 支持")).toBeVisible();
    await expect(page.getByRole("button", { name: /Approval|Delivery|Rollback|执行|回滚/ })).toHaveCount(0);
    await expectNoPageOverflow(page);
    await expectCleanPage(diagnostics, testInfo);
  });

  test("Settings validates, protects drafts, handles revision conflict and reports partial truth", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/settings");
    await expect(page.getByRole("heading", { level: 1, name: "设置与 Revision" })).toBeFocused();

    await page.getByTestId("revision-summary").fill("");
    await page.getByTestId("prometheus-url").fill("javascript:alert(1)");
    await page.getByTestId("apply-settings").click();
    await expect(page.getByText("Revision 摘要不能为空")).toBeVisible();
    await expect(page.getByText("必须使用 http 或 https 协议")).toBeVisible();

    await page.getByTestId("revision-summary").fill("Owner reviewed provider timeout");
    await page.getByTestId("prometheus-url").fill("https://prometheus.example.test");
    await page.getByRole("button", { name: "模拟 Revision 冲突" }).click();
    await expect(page.getByTestId("revision-conflict")).toBeVisible();
    await expect(page.getByTestId("apply-settings")).toBeDisabled();
    await page.getByTestId("reload-revision").click();
    await expect(page.getByTestId("revision-conflict")).toHaveCount(0);

    await page.getByTestId("revision-summary").fill("Owner reviewed provider timeout");
    await page.getByTestId("prometheus-url").fill("https://prometheus.example.test");
    await page.getByTestId("apply-settings").click();
    await expect(page.getByTestId("partial-result")).toBeVisible();
    await expect(page.getByTestId("partial-result")).toContainText("未声明原子成功");
    await page.getByTestId("retry-provider").click();
    await expect(page.getByTestId("retry-passed")).toBeVisible();
    await expect(page.getByTestId("retry-passed")).toContainText("Verification 仍由独立流程决定");

    await page.getByTestId("revision-summary").fill("unsaved local draft");
    await page.getByRole("button", { name: "返回 Incident" }).click();
    const leaveDialog = page.getByRole("dialog", { name: "离开并放弃当前 Draft？" });
    await expect(leaveDialog).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(leaveDialog).toBeVisible();
    await page.getByTestId("stay-settings").click();
    await expect(page).toHaveURL(/\/settings$/);
    await page.getByRole("button", { name: "返回 Incident" }).click();
    await page.getByTestId("leave-settings").click();
    await expect(page).toHaveURL(/\/incidents$/);
    await expectCleanPage(diagnostics, testInfo);
  });

  test("SSE fault injection preserves lifecycle distinctions and teardown", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.goto("/states");
    await expect(page.getByText("10 domain states")).toBeVisible();
    await expect(page.getByTestId("sse-state")).toContainText("Connecting");
    const stateIndex = page.getByRole("navigation", { name: "异常状态" });
    for (const state of ["Permission Denied", "Partial", "Stale", "Disconnected", "Expired Authority", "Hash Changed", "Provider Disagreement", "Accepted, not observed", "Observed, not verified", "Verification Failed"]) {
      await stateIndex.getByRole("button", { name: state, exact: true }).click();
      await expect(page.locator('[data-testid^="state-"]')).toBeVisible();
    }

    await page.getByTestId("sse-live").click();
    await expect(page.getByTestId("sse-state")).toContainText("Live");
    await page.getByTestId("sse-duplicate").click();
    await expect(page.getByText(/duplicates ignored 1/)).toBeVisible();
    await page.getByRole("button", { name: "Reconnecting", exact: true }).click();
    await expect(page.getByTestId("sse-state")).toContainText("Reconnecting");
    await page.getByRole("button", { name: "Disconnect", exact: true }).click();
    await expect(page.getByTestId("sse-state")).toContainText("Disconnected");
    await page.getByRole("group", { name: "SSE 故障注入" }).getByRole("button", { name: "Stale", exact: true }).click();
    await expect(page.getByTestId("sse-state")).toContainText("Stale");
    await page.getByTestId("sse-expire").click();
    await expect(page.getByTestId("sse-state")).toContainText("Cursor expired");
    await page.getByTestId("sse-resync-fail").click();
    await expect(page.getByTestId("sse-state")).toContainText("Resync failed");
    await expect(page.getByText("保持 stale，不伪造连续")).toBeVisible();
    await page.getByTestId("sse-resync-pass").click();
    await expect(page.getByTestId("sse-state")).toContainText("Live");
    await page.getByTestId("sse-teardown").click();
    await expect(page.getByTestId("sse-state")).toContainText("Torn down");
    await expect(page.getByText("连接与监听器已清理")).toBeVisible();
    await expectCleanPage(diagnostics, testInfo);
  });

  test("large-data modes virtualize, cancel stale requests and preserve full-value copy", async ({ page }, testInfo) => {
    const diagnostics = observePage(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/scale");
    await expect(page.getByText("10,000 rows")).toBeVisible();
    const rendered = Number((await page.getByTestId("virtual-render-count").textContent())?.match(/\d+/)?.[0] ?? 0);
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(100);

    await page.getByTestId("scroll-scale-end").click();
    await expect.poll(async () => Number(await page.locator('.virtual-row').last().getAttribute("data-index"))).toBeGreaterThan(9900);
    await page.getByRole("button", { name: "滚动到顶部" }).click();
    await page.getByTestId("copy-first-full-value").click();
    await expect(page.getByTestId("copy-status")).toContainText("完整值已复制");
    await page.getByTestId("simulate-stale-filter").click();
    await expect(page.getByText(/stale requests canceled 1/)).toBeVisible();
    await expect(page.getByText("2,500 rows")).toBeVisible();

    const counts: Record<string, string> = { "Trace 2.5k": "2,500 rows", "Timeline 5k": "5,000 rows", "Table 20k": "20,000 rows" };
    for (const [tab, count] of Object.entries(counts)) {
      await page.getByRole("tab", { name: tab }).click();
      await expect(page.getByText(count)).toBeVisible();
      const activeRendered = Number((await page.getByTestId("virtual-render-count").textContent())?.match(/\d+/)?.[0] ?? 0);
      expect(activeRendered).toBeLessThan(100);
    }
    await expectNoPageOverflow(page);
    await expectCleanPage(diagnostics, testInfo);
  });
});
