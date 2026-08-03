import { expect, test } from "@playwright/test";

import {
  capabilityPassed,
  recordCapabilityDisposition,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const incidentID = "cc7db3b0-6cd7-400f-8bcd-5168712db0f4";
const alertID = "9d8da37f-9bea-44e0-ad6c-2d5267eac285";

test("single registered Scope is recorded without a fake activation", async ({ page }, testInfo) => {
	test.skip(capabilityPassed("shell.scope-activate"), "Scope activation already has retained PASS evidence");
  const tracker = trackBrowserEvidence(page);
  await recordCapabilityDisposition(tracker, testInfo, {
    capabilityID: "shell.scope-activate",
    status: "NOT RUN",
    blockedBy: "NO_ALTERNATE_SCOPE",
    uiAction: "从真实页面读取 Scope selector 与注册 Scope 列表；没有第二个候选时不激活同一 Scope 冒充切换",
    expectedOperations: ["GET /api/v1/scopes"],
    uiResult: "页面仅显示 cloudops-local，Scope selector 因只有一个真实候选而禁用",
  }, async () => {
    await waitForApiResponse(page, "GET /api/v1/scopes", () => page.goto("/overview"));
    const selector = page.getByRole("combobox", { name: /活动运行范围/ });
    await expect(selector).toBeVisible();
    await expect(selector).toBeDisabled();
    await expect(selector).toContainText("cloudops-local");
  });
});

for (const capabilityID of ["incidents.remediation-approve", "incidents.remediation-reject"] as const) {
  test(`${capabilityID} requires a run-scoped immutable Plan`, async ({ page }, testInfo) => {
		test.skip(capabilityPassed(capabilityID), `${capabilityID} already has retained PASS evidence`);
    const tracker = trackBrowserEvidence(page);
    await recordCapabilityDisposition(tracker, testInfo, {
      capabilityID,
      status: "NOT RUN",
      blockedBy: "NO_RUN_SCOPED_REMEDIATION_PLAN",
      uiAction: "从本轮 Incident 的 Approval 区读取服务端 Remediation Plan 投影；不伪造模型未生成的 Plan",
      expectedOperations: ["GET /api/v1/incidents/{id}/remediation-plans"],
      uiResult: "真实页面明确回显 no-change recovery 可以没有 immutable remediation Plan",
    }, async () => {
      await waitForApiResponse(
        page,
        "GET /api/v1/incidents/{id}/remediation-plans",
        () => page.goto(`/incidents/${incidentID}#approval`),
      );
      await expect(page.getByRole("heading", { name: "Remediation Plan & Approval" })).toBeVisible();
      await expect(page.getByText("No immutable remediation Plan exists. This can be correct for a no-change recovery.")).toBeVisible();
      await expect(page.locator("#remediation-plans .plan-stack > article.approval-panel")).toHaveCount(0);
    });
  });
}

test("agent Investigation cancel requires a run-scoped pending run", async ({ page }, testInfo) => {
	test.skip(capabilityPassed("agent.investigation-cancel"), "Investigation cancellation already has retained PASS evidence");
  const tracker = trackBrowserEvidence(page);
  await recordCapabilityDisposition(tracker, testInfo, {
    capabilityID: "agent.investigation-cancel",
    status: "NOT RUN",
    blockedBy: "NO_RUN_SCOPED_PENDING_INVESTIGATION",
    uiAction: "从本轮 resolved Alert 与 closed Incident 的原始 Investigation 控件核对是否存在可安全取消的新 run",
    expectedOperations: [
      "GET /api/v1/alerts/{id}",
      "GET /api/v1/incidents/{id}",
      "GET /api/v1/incidents/{id}/investigations",
    ],
    uiResult: "Alert 与 Incident 均回显最终权威状态，两个原始启动控件 disabled，历史 Investigation 均为 completed",
  }, async () => {
    await waitForApiResponse(
      page,
      "GET /api/v1/alerts/{id}",
      () => page.goto(`/alerts/${alertID}`),
    );
    await expect(page.getByText("已恢复 · 严重", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "启动 Investigation" })).toBeDisabled();

    await waitForApiResponse(
      page,
      "GET /api/v1/incidents/{id}/investigations",
      () => page.goto(`/incidents/${incidentID}#investigations`),
    );
    await expect(page.getByText("closed", { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "发起调查" })).toBeDisabled();
    const investigations = page.getByRole("region", { name: "Investigation 记录" });
    await expect(investigations).toContainText("已完成");
    await expect(investigations).not.toContainText(/等待中|运行中|pending|running/);
  });
});
