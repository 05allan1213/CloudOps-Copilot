import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

interface OperationalScope {
  id?: string;
  name: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  active: boolean;
}

interface ConfigurationRevision {
  id: string;
  number: number;
  hash: string;
  summary: string;
  scope: OperationalScope;
  scopes: OperationalScope[];
}

interface SettingsSnapshot {
  active_revision: ConfigurationRevision;
}

interface Validation {
  id: string;
  draft_hash: string;
  valid: boolean;
  errors: Array<{ field: string; code: string; message: string }>;
}

interface BootstrapSnapshot {
  active_scope: OperationalScope;
}

interface IntegrationScopeEnvironment {
  run_id: string;
  cluster_name: string;
  cluster_id: string;
  context: string;
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return body && typeof body === "object" && "data" in body ? body.data : body as T;
}

function fieldControl(page: Page, field: string) {
  return page.locator(`[data-field="${field}"]`).locator("input, textarea").first();
}

async function fillField(page: Page, field: string, value: string) {
  const control = fieldControl(page, field);
  await expect(control).toBeVisible();
  await control.fill(value);
  await control.blur();
}

async function openSettings(page: Page): Promise<SettingsSnapshot> {
  const response = await waitForApiResponse(page, "GET /api/v1/settings", () => page.goto("/settings#operational-scope"));
  await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
  await page.locator('[data-settings-section="scopes"]').click();
  return responseData<SettingsSnapshot>(response);
}

async function refreshSettings(page: Page): Promise<SettingsSnapshot> {
  const response = await waitForApiResponse(page, "GET /api/v1/settings", () => page.reload());
  await page.locator('[data-settings-section="scopes"]').click();
  return responseData<SettingsSnapshot>(response);
}

async function validateAndApply(page: Page): Promise<ConfigurationRevision> {
  const validationResponse = await waitForApiResponse(
    page,
    "POST /api/v1/settings/validate",
    () => page.getByRole("button", { name: /^(验证配置|重新验证)$/ }).click(),
  );
  const validation = await responseData<Validation>(validationResponse);
  expect(validation.valid, JSON.stringify(validation.errors)).toBe(true);
  expect(validation.draft_hash).toMatch(/^[0-9a-f]{64}$/);
  await page.getByRole("button", { name: "查看变更并应用" }).click();
  const dialog = page.getByRole("dialog");
  const applyResponse = await waitForApiResponse(
    page,
    "POST /api/v1/configuration-revisions",
    () => dialog.getByRole("button", { name: "应用配置" }).click(),
  );
  const revision = await responseData<ConfigurationRevision>(applyResponse);
  for (let attempt = 0; attempt < 40; attempt += 1) {
    const workerBoundary = page.getByRole("row").filter({ hasText: "Worker boundary" });
    if (await workerBoundary.isVisible().catch(() => false)) {
      const text = await workerBoundary.innerText();
      if (text.includes("succeeded") && text.includes(revision.hash)) return revision;
    }
    const refresh = page.getByRole("button", { name: "刷新当前 Revision 观测" });
    if (await refresh.isVisible().catch(() => false)) {
      await waitForApiResponse(page, "GET /api/v1/settings", () => refresh.click());
    }
    await page.waitForTimeout(500);
  }
  throw new Error(`Configuration Revision ${revision.id} did not reach Worker succeeded`);
}

async function activateScopeFromHeader(page: Page, clusterID: string): Promise<OperationalScope> {
  const selector = page.getByRole("combobox", { name: /活动运行范围/ });
  await expect(selector).toBeEnabled();
  await selector.click();
  const option = page.getByRole("option").filter({ hasText: clusterID });
  await expect(option).toBeVisible();
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/scopes/{id}/activate",
    () => option.click(),
  );
  const scope = await responseData<OperationalScope>(response);
  expect(scope.cluster_id).toBe(clusterID);
  await expect(selector).toContainText(clusterID);
  return scope;
}

test("activate and restore a genuinely independent Kubernetes Scope", async ({ page }, testInfo) => {
  const reader = readRunArtifact<IntegrationScopeEnvironment>("scope-environment.json");
  if (!reader) {
    throw new Error("independent Scope is required; run `make real-integration-scope-up` with the current CLOUDOPS_REAL_INTEGRATION_RUN_ID");
  }
  const tracker = trackBrowserEvidence(page);
  const secondaryName = `Integration ${reader.cluster_id}`;

  await proveCapability(tracker, testInfo, {
    capabilityID: "shell.scope-activate",
    uiAction: "通过 Settings 注册独立 Kubernetes Scope，从 Header 切换并刷新验证，再切回原 Scope 并恢复配置",
    expectedOperations: ["POST /api/v1/scopes/{id}/activate"],
    uiResult: "第二 Scope 的 Worker 探测、active_operational_scope 持久化与跨页回显均成功，最终 active hash 与原 Scope 恢复",
  }, async () => {
    const initial = await openSettings(page);
    const original = initial.active_revision.scope;
    expect(original.id).toMatch(/^[0-9a-f-]{36}$/);
    expect(initial.active_revision.scopes.some((scope) => scope.cluster_id === reader.cluster_id)).toBe(false);

    await page.getByRole("button", { name: "添加运行范围" }).click();
    const secondaryIndex = initial.active_revision.scopes.length;
    await fillField(page, `scopes.${secondaryIndex}.name`, secondaryName);
    await fillField(page, `scopes.${secondaryIndex}.cluster_id`, reader.cluster_id);
    await fillField(page, `scopes.${secondaryIndex}.environment`, "integration");
    await fillField(page, `scopes.${secondaryIndex}.namespaces`, "default");
    await fillField(page, "scopes.summary", `${reader.run_id} independent Scope integration`);
    await validateAndApply(page);

    let observed = await refreshSettings(page);
    const secondary = observed.active_revision.scopes.find((scope) => scope.cluster_id === reader.cluster_id);
    expect(secondary?.id).toMatch(/^[0-9a-f-]{36}$/);

    await waitForApiResponse(page, "GET /api/v1/scopes", () => page.goto("/overview"));
    await activateScopeFromHeader(page, reader.cluster_id);
    let bootstrapResponse = await waitForApiResponse(page, "GET /api/v1/bootstrap", () => page.reload());
    let bootstrap = await responseData<BootstrapSnapshot>(bootstrapResponse);
    expect(bootstrap.active_scope.cluster_id).toBe(reader.cluster_id);

    await waitForApiResponse(page, "GET /api/v1/scopes", () => page.goto("/settings"));
    observed = await refreshSettings(page);
    expect(observed.active_revision.scope.cluster_id).toBe(reader.cluster_id);

    await page.goto("/overview");
    await activateScopeFromHeader(page, original.cluster_id);
    bootstrapResponse = await waitForApiResponse(page, "GET /api/v1/bootstrap", () => page.reload());
    bootstrap = await responseData<BootstrapSnapshot>(bootstrapResponse);
    expect(bootstrap.active_scope.id).toBe(original.id);

    observed = await openSettings(page);
    await page.locator(".settings-object-list button").filter({ hasText: reader.cluster_id }).click();
    await page.getByRole("button", { name: `从草稿移除 ${secondaryName}` }).click();
    await fillField(page, "scopes.summary", initial.active_revision.summary);
    await validateAndApply(page);
    observed = await refreshSettings(page);
    expect(observed.active_revision.hash).toBe(initial.active_revision.hash);
    expect(observed.active_revision.scope.id).toBe(original.id);
    expect(observed.active_revision.scopes.some((scope) => scope.cluster_id === reader.cluster_id)).toBe(false);
  });
});
