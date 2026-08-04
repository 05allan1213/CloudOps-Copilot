import { expect, test, type BrowserContext, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
  waitForApiResponseResult,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const restoreMode = process.env.CLOUDOPS_REAL_INTEGRATION_SETTINGS_RESTORE === "1";
const artifactPath = "objects/settings.json";
const llmTokenLimit = 4096;
const llmTimeoutMS = 180000;
const settingsValidationResponseTimeoutMS = 75_000;
const runSuffix = runID.replace(/[^a-zA-Z0-9]/g, "").slice(-12).toLowerCase();
const secretPurpose = `ui_int_${runSuffix}`;
const secretValue = `synthetic-write-only-${runID}`;
const policyName = `ui-int ${runSuffix} disabled policy`;
const externalProviderLabels = {
  github: "GitHub",
  argocd: "Argo CD",
} as const;

type SectionKey = "system" | "scopes" | "policies" | "providers" | "secret-references";

interface ProviderConfiguration {
  provider: string;
  enabled: boolean;
  endpoint: string;
  model: string;
  timeout_ms: number;
  max_results: number;
  context_link_base: string;
}

interface OperationalScope {
  id?: string;
  name: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  active: boolean;
}

interface SecretReference {
  provider: string;
  purpose: string;
  secret_version_id: string;
  state?: string;
  fingerprint?: string;
}

interface EscalationPolicy {
  id?: string;
  configuration_revision_id?: string;
  name: string;
  enabled: boolean;
  severities: string[];
  namespaces: string[];
  label_matchers: Record<string, string>;
  minimum_firing_seconds: number;
  minimum_recurrence_count: number;
  create_incident: true;
}

interface ConfigurationRevision {
  id: string;
  number: number;
  hash: string;
  summary: string;
  general: {
    query_max_lookback_seconds: number;
    query_max_results: number;
    telemetry_retention_days: number;
    browser_notifications_enabled: boolean;
    automatic_escalation_enabled: boolean;
  };
  scope: OperationalScope;
  scopes: OperationalScope[];
  providers: ProviderConfiguration[];
  escalation_policies: EscalationPolicy[];
  secret_references: SecretReference[];
  worker_boundary?: {
    status: string;
    observed_hash?: string;
  };
}

interface SettingsSnapshot {
  active_revision: ConfigurationRevision;
  history: ConfigurationRevision[];
}

interface ConfigurationValidation {
  id: string;
  draft_hash: string;
  valid: boolean;
  errors: Array<{ field: string; code: string; message: string }>;
  provider_results: Array<{ provider: string; state: string; detail: string }>;
  expires_at: string;
}

interface SectionProof {
  validation_id: string;
  validation_hash: string;
  revision_id: string;
  revision_number: number;
  revision_hash: string;
  summary: string;
}

interface SettingsArtifact {
  run_id: string;
  pre_run?: ConfigurationRevision;
  provider_test?: { provider: string; state: string; detail: string };
  isolation?: {
    revision_id: string;
    revision_number: number;
    revision_hash: string;
    validation_id: string;
    validation_hash: string;
    summary: string;
    disabled_providers: Array<keyof typeof externalProviderLabels>;
  };
  conflict?: {
    base_revision_id: string;
    base_revision_hash: string;
    winning_revision_id: string;
    stale_status: number;
    stale_code: string;
    retry_revision_id: string;
    llm_max_tokens: number;
    query_max_results: number;
  };
  secret?: {
    id: string;
    provider: string;
    purpose: string;
    fingerprint: string;
    state: string;
    value_echoed: false;
    revision_id: string;
  };
  sections?: Partial<Record<SectionKey, SectionProof>>;
  llm_runtime?: {
    revision_id: string;
    revision_number: number;
    revision_hash: string;
    timeout_ms: number;
    max_results: number;
  };
  restored?: {
    revision_id: string;
    revision_number: number;
    revision_hash: string;
    equivalent_to_pre_run: true;
  };
}

function currentArtifact(): SettingsArtifact {
  return readRunArtifact<SettingsArtifact>(artifactPath) ?? { run_id: runID };
}

function saveArtifact(patch: Partial<SettingsArtifact>) {
  recordRunArtifact(artifactPath, { ...currentArtifact(), ...patch });
}

function saveSection(key: SectionKey, proof: SectionProof) {
  const artifact = currentArtifact();
  saveArtifact({ sections: { ...artifact.sections, [key]: proof } });
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return (body && typeof body === "object" && "data" in body ? body.data : body) as T;
}

async function openSettings(page: Page): Promise<SettingsSnapshot> {
  const response = await waitForApiResponse(page, "GET /api/v1/settings", () => page.goto("/settings"));
  await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
  const snapshot = await responseData<SettingsSnapshot>(response);
  const artifact = currentArtifact();
  if (!artifact.pre_run) saveArtifact({ pre_run: snapshot.active_revision });
  return snapshot;
}

async function refreshSettings(page: Page): Promise<SettingsSnapshot> {
  const response = await waitForApiResponse(page, "GET /api/v1/settings", () => page.reload());
  await expect(page.getByRole("heading", { name: "设置", level: 1 })).toBeVisible();
  return responseData<SettingsSnapshot>(response);
}

async function selectSection(page: Page, key: SectionKey) {
  await page.locator(`[data-settings-section="${key}"]`).click();
  await expect(page.locator(`[data-settings-section="${key}"]`)).toHaveAttribute("aria-current", "page");
}

function fieldControl(page: Page, field: string) {
  return page.locator(`[data-field="${field}"]`).locator("input, textarea").first();
}

async function fillField(page: Page, field: string, value: string | number) {
  const control = fieldControl(page, field);
  await expect(control).toBeVisible();
  await control.fill(String(value));
  await control.blur();
}

async function fillSummary(page: Page, section: SectionKey, summary: string) {
  await fillField(page, `${section}.summary`, summary);
}

async function validateActiveSection(page: Page): Promise<ConfigurationValidation> {
  const button = page.getByRole("button", { name: /^(验证配置|重新验证)$/ });
  await expect(button).toBeEnabled();
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/settings/validate",
    () => button.click(),
    settingsValidationResponseTimeoutMS,
  );
  const validation = await responseData<ConfigurationValidation>(response);
  expect(validation.valid, JSON.stringify(validation.errors)).toBe(true);
  expect(validation.id).toBeTruthy();
  expect(validation.draft_hash).toMatch(/^[0-9a-f]{64}$/);
  expect(validation.provider_results.length).toBeGreaterThan(0);
  await expect(page.getByText("Validation 通过", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "查看变更并应用" })).toBeEnabled();
  return validation;
}

async function waitForWorkerSuccess(page: Page, expectedHash: string) {
  for (let attempt = 0; attempt < 40; attempt += 1) {
    const workerBoundary = page.getByRole("row").filter({ hasText: "Worker boundary" });
    if (await workerBoundary.isVisible().catch(() => false)) {
      const observation = await workerBoundary.innerText();
      if (observation.includes("succeeded") && observation.includes(expectedHash)) return;
    }
    const refresh = page.getByRole("button", { name: "刷新当前 Revision 观测" });
    if (await refresh.isVisible().catch(() => false)) {
      await waitForApiResponse(page, "GET /api/v1/settings", () => refresh.click());
    }
    await page.waitForTimeout(500);
  }
  throw new Error("Configuration Revision did not reach Worker succeeded observation");
}

async function applyActiveSection(page: Page): Promise<ConfigurationRevision> {
  await page.getByRole("button", { name: "查看变更并应用" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "查看变更并应用" })).toBeVisible();
  await expect(dialog).toContainText("Exact hash");
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/configuration-revisions",
    () => dialog.getByRole("button", { name: "应用配置" }).click(),
  );
  const revision = await responseData<ConfigurationRevision>(response);
  expect(revision.id).toBeTruthy();
  expect(revision.hash).toMatch(/^[0-9a-f]{64}$/);
  await expect(page.locator(".settings-context-bar")).toContainText(`Revision #${revision.number}`);
  await waitForWorkerSuccess(page, revision.hash);
  return revision;
}

async function editLLMLimit(page: Page, value: number) {
  await selectSection(page, "providers");
  await page.getByRole("button", { name: "编辑 LLM" }).click();
  await expect(page.getByRole("heading", { name: "编辑 LLM" })).toBeVisible();
  await fillField(page, "providers.llm.max_results", value);
  await expect(fieldControl(page, "providers.llm.max_results")).toHaveAttribute("aria-valuenow", String(value));
  await page.getByRole("button", { name: "完成编辑" }).click();
  await expect(page.getByRole("heading", { name: "编辑 LLM" })).toBeHidden();
}

async function editLLMRuntime(page: Page, timeoutMS: number, maxResults: number) {
  await selectSection(page, "providers");
  await page.getByRole("button", { name: "编辑 LLM" }).click();
  await expect(page.getByRole("heading", { name: "编辑 LLM" })).toBeVisible();
  await fillField(page, "providers.llm.timeout_ms", timeoutMS / 1000);
  await expect(fieldControl(page, "providers.llm.timeout_ms")).toHaveAttribute("aria-valuenow", String(timeoutMS / 1000));
  await fillField(page, "providers.llm.max_results", maxResults);
  await expect(fieldControl(page, "providers.llm.max_results")).toHaveAttribute("aria-valuenow", String(maxResults));
  await page.getByRole("button", { name: "完成编辑" }).click();
  await expect(page.getByRole("heading", { name: "编辑 LLM" })).toBeHidden();
}

async function editProviderEnabled(
  page: Page,
  provider: keyof typeof externalProviderLabels,
  enabled: boolean,
) {
  await selectSection(page, "providers");
  const label = externalProviderLabels[provider];
  await page.getByRole("button", { name: `编辑 ${label}` }).click();
  await expect(page.getByRole("heading", { name: `编辑 ${label}` })).toBeVisible();
  const enabledSwitch = page.locator(".settings-provider-editor").getByRole("switch");
  if (await enabledSwitch.isChecked() !== enabled) await enabledSwitch.click();
  await expect(enabledSwitch).toHaveAttribute("aria-checked", String(enabled));
  await page.getByRole("button", { name: "完成编辑" }).click();
  await expect(page.getByRole("heading", { name: `编辑 ${label}` })).toBeHidden();
}

function externalProvidersDisabled(revision: ConfigurationRevision): boolean {
  return Object.keys(externalProviderLabels).every((provider) => (
    revision.providers.find((item) => item.provider === provider)?.enabled === false
  ));
}

async function ensureExternalProviderIsolation(page: Page): Promise<SettingsSnapshot> {
  const initial = await openSettings(page);
  if (externalProvidersDisabled(initial.active_revision)) {
    if (!currentArtifact().isolation) {
      saveArtifact({
        isolation: {
          revision_id: initial.active_revision.id,
          revision_number: initial.active_revision.number,
          revision_hash: initial.active_revision.hash,
          validation_id: "recovered-from-active-revision",
          validation_hash: initial.active_revision.hash,
          summary: initial.active_revision.summary,
          disabled_providers: ["github", "argocd"],
        },
      });
    }
    return initial;
  }

  for (const provider of Object.keys(externalProviderLabels) as Array<keyof typeof externalProviderLabels>) {
    const configuration = initial.active_revision.providers.find((item) => item.provider === provider);
    if (configuration?.enabled !== false) await editProviderEnabled(page, provider, false);
  }
  const summary = `${runID} isolate unavailable external providers`;
  await fillSummary(page, "providers", summary);
  const validation = await validateActiveSection(page);
  const revision = await applyActiveSection(page);
  expect(externalProvidersDisabled(revision)).toBe(true);
  saveArtifact({
    isolation: {
      revision_id: revision.id,
      revision_number: revision.number,
      revision_hash: revision.hash,
      validation_id: validation.id,
      validation_hash: validation.draft_hash,
      summary,
      disabled_providers: ["github", "argocd"],
    },
  });
  const observed = await refreshSettings(page);
  expect(observed.active_revision.id).toBe(revision.id);
  expect(externalProvidersDisabled(observed.active_revision)).toBe(true);
  return observed;
}

function llmConfiguration(revision: ConfigurationRevision): ProviderConfiguration {
  const configuration = revision.providers.find((item) => item.provider === "llm");
  if (!configuration) throw new Error("active Revision has no LLM Provider configuration");
  return configuration;
}

function sectionProof(validation: ConfigurationValidation, revision: ConfigurationRevision): SectionProof {
  return {
    validation_id: validation.id,
    validation_hash: validation.draft_hash,
    revision_id: revision.id,
    revision_number: revision.number,
    revision_hash: revision.hash,
    summary: revision.summary,
  };
}

async function verifyRetainedRevisions(page: Page, revisionIDs: string[]) {
  const snapshot = await openSettings(page);
  for (const revisionID of revisionIDs) {
    expect(snapshot.history.some((revision) => revision.id === revisionID), `retained Settings Revision ${revisionID}`).toBe(true);
  }
}

async function closeContexts(...contexts: BrowserContext[]) {
  await Promise.all(contexts.map((context) => context.close().catch(() => undefined)));
}

function strippedScope(scope: OperationalScope) {
  return {
    name: scope.name,
    cluster_id: scope.cluster_id,
    environment: scope.environment,
    namespaces: scope.namespaces,
    active: scope.active,
  };
}

function strippedPolicy(policy: EscalationPolicy) {
  return {
    name: policy.name,
    enabled: policy.enabled,
    severities: policy.severities,
    namespaces: policy.namespaces,
    label_matchers: policy.label_matchers,
    minimum_firing_seconds: policy.minimum_firing_seconds,
    minimum_recurrence_count: policy.minimum_recurrence_count,
    create_incident: policy.create_incident,
  };
}

function equivalentConfiguration(left: ConfigurationRevision, right: ConfigurationRevision): boolean {
  const comparable = (revision: ConfigurationRevision) => ({
    summary: revision.summary,
    general: revision.general,
    scope: strippedScope(revision.scope),
    scopes: revision.scopes.map(strippedScope),
    providers: revision.providers,
    escalation_policies: revision.escalation_policies.map(strippedPolicy),
    secret_references: revision.secret_references.map((reference) => ({
      provider: reference.provider,
      purpose: reference.purpose,
      secret_version_id: reference.secret_version_id,
    })),
  });
  return JSON.stringify(comparable(left)) === JSON.stringify(comparable(right));
}

test.describe.serial("Settings real revisions, CAS and restoration", () => {
  test.describe("temporary Settings writes", () => {
    test.skip(restoreMode, "temporary Settings writes are disabled in restore mode");

  test("settings.provider-test", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    await proveCapability(tracker, testInfo, {
      capabilityID: "settings.provider-test",
      uiAction: "从 Provider 分区打开 LLM 编辑器，点击测试连接并确认执行一次真实 Provider test",
      uiResult: "独立 test 返回当前 DeepSeek Provider available/detail，页面明确不把 test 冒充 apply",
    }, async () => {
      await openSettings(page);
      await selectSection(page, "providers");
      await page.getByRole("button", { name: "编辑 LLM" }).click();
      await page.getByRole("button", { name: "测试连接" }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByRole("heading", { name: "执行 Provider test？" })).toBeVisible();
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/providers/{provider}/tests",
        () => dialog.getByRole("button", { name: "执行一次 test" }).click(),
      );
      const result = await responseData<{ provider: string; state: string; detail: string }>(response);
      expect(result.provider).toBe("llm");
      expect(result.state, result.detail).toBe("available");
      await expect(page.getByText("最近一次本地测试：可用", { exact: true })).toBeVisible();
      await expect(page.getByText(result.detail, { exact: true })).toBeVisible();
      saveArtifact({ provider_test: result });
    });
  });

  test("settings.llm-runtime-bounds", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const existing = currentArtifact().llm_runtime;
    const snapshot = await openSettings(page);
    const currentLLM = llmConfiguration(snapshot.active_revision);
    const requiresApply = currentLLM.timeout_ms !== llmTimeoutMS || currentLLM.max_results !== llmTokenLimit;
    await proveCapability(tracker, testInfo, {
      capabilityID: "settings.validate",
      uiAction: requiresApply
        ? "从 Provider/LLM 原控件把请求 timeout 放宽到 180 秒并保持 4096 token，经服务端 validate、Diff 与 apply 生效"
        : "从真实 Settings 刷新确认本轮 DeepSeek timeout/token 参数仍由 active Revision 持久化",
      expectedOperations: requiresApply
        ? ["POST /api/v1/settings/validate", "POST /api/v1/configuration-revisions"]
        : ["GET /api/v1/settings"],
      uiResult: "active Revision 与 Worker 回显 DeepSeek timeout=180000ms、max_results=4096；最终 restore 仍恢复 pre-run 等价配置",
    }, async () => {
      if (requiresApply) {
        await editLLMRuntime(page, llmTimeoutMS, llmTokenLimit);
        for (const provider of Object.keys(externalProviderLabels) as Array<keyof typeof externalProviderLabels>) {
          if (snapshot.active_revision.providers.find((item) => item.provider === provider)?.enabled) {
            await editProviderEnabled(page, provider, false);
          }
        }
        await fillSummary(page, "providers", `${runID} widen DeepSeek runtime bounds`);
        const validation = await validateActiveSection(page);
        const revision = await applyActiveSection(page);
        saveSection("providers", sectionProof(validation, revision));
        saveArtifact({
          llm_runtime: {
            revision_id: revision.id,
            revision_number: revision.number,
            revision_hash: revision.hash,
            timeout_ms: llmConfiguration(revision).timeout_ms,
            max_results: llmConfiguration(revision).max_results,
          },
        });
      } else if (!existing) {
        saveArtifact({
          llm_runtime: {
            revision_id: snapshot.active_revision.id,
            revision_number: snapshot.active_revision.number,
            revision_hash: snapshot.active_revision.hash,
            timeout_ms: currentLLM.timeout_ms,
            max_results: currentLLM.max_results,
          },
        });
      }
      const observed = await refreshSettings(page);
      expect(llmConfiguration(observed.active_revision)).toMatchObject({
        timeout_ms: llmTimeoutMS,
        max_results: llmTokenLimit,
      });
    });
  });

  test("settings.stale-revision-conflict", async ({ browser }, testInfo) => {
    const existing = currentArtifact().conflict;
    if (existing) {
      const context = await browser.newContext();
      const page = await context.newPage();
      const tracker = trackBrowserEvidence(page);
      try {
        await proveCapability(tracker, testInfo, {
          capabilityID: "settings.stale-revision-conflict",
          uiAction: "从 Revision history 重新读取本轮 winner、409 stale writer 与 rebase/retry durable revisions",
          expectedOperations: ["GET /api/v1/settings"],
          uiResult: "历史仍保留原子 CAS winner 与 rebase/retry Revision；当前 active 状态不被重放",
        }, () => verifyRetainedRevisions(page, [existing.winning_revision_id, existing.retry_revision_id]));
      } finally {
        await closeContexts(context);
      }
      return;
    }

    const contextA = await browser.newContext();
    const contextB = await browser.newContext();
    const pageA = await contextA.newPage();
    const pageB = await contextB.newPage();
    const tracker = trackBrowserEvidence(pageA, pageB);
    try {
      await proveCapability(tracker, testInfo, {
        capabilityID: "settings.stale-revision-conflict",
        uiAction: "从 Provider 原控件临时停用本轮不可用的 GitHub/Argo 并保存 isolation Revision；两个隔离 Chromium Context 再基于同一 Revision 分别编辑 Provider/System，暂停 B 的浏览器 POST，让 A 先 apply，再释放 B 验证服务端 CAS 409，随后从 B rebase、重新验证并 apply",
        uiResult: "A 的 LLM 输出上限与 B rebase 后的查询上限均 durable；stale B 返回 CONFIGURATION_REVISION_CHANGED 且无 Revision，再试后刷新回显",
      }, async () => {
        const baseA = await ensureExternalProviderIsolation(pageA);
        const baseB = await openSettings(pageB);
        expect(baseA.active_revision.id).toBe(baseB.active_revision.id);
        expect(baseA.active_revision.hash).toBe(baseB.active_revision.hash);
        const base = baseA.active_revision;
        const providerRaceValue = llmConfiguration(base).max_results === llmTokenLimit ? 4080 : llmTokenLimit;
        const originalQueryLimit = base.general.query_max_results;
        const queryLimit = originalQueryLimit > 20 ? originalQueryLimit - 10 : originalQueryLimit + 10;

        await editLLMLimit(pageA, providerRaceValue);
        const providerSummary = `${runID} temporary LLM output token limit`;
        await fillSummary(pageA, "providers", providerSummary);
        const providerValidation = await validateActiveSection(pageA);

        await selectSection(pageB, "system");
        await fillField(pageB, "system.query_max_results", queryLimit);
        await expect(fieldControl(pageB, "system.query_max_results")).toHaveAttribute("aria-valuenow", String(queryLimit));
        const systemSummary = `${runID} stale CAS system writer`;
        await fillSummary(pageB, "system", systemSummary);
        await validateActiveSection(pageB);

        let releaseRequest: () => void = () => {};
        const releaseBarrier = new Promise<void>((resolve) => { releaseRequest = resolve; });
        let observeIntercept: () => void = () => {};
        const intercepted = new Promise<void>((resolve) => { observeIntercept = resolve; });
        await pageB.route("**/api/v1/configuration-revisions", async (route) => {
          if (route.request().method() === "POST") {
            observeIntercept();
            await releaseBarrier;
          }
          await route.continue();
        });

        await pageB.getByRole("button", { name: "查看变更并应用" }).click();
        const staleResponsePromise = waitForApiResponseResult(
          pageB,
          "POST /api/v1/configuration-revisions",
          () => pageB.getByRole("dialog").getByRole("button", { name: "应用配置" }).click(),
        );
        await intercepted;

        const winningRevision = await applyActiveSection(pageA);
        saveSection("providers", sectionProof(providerValidation, winningRevision));
        releaseRequest();

        const staleResponse = await staleResponsePromise;
        expect(staleResponse.status()).toBe(409);
        const staleProblem = await responseData<{ code?: string }>(staleResponse);
        expect(staleProblem.code).toBe("CONFIGURATION_REVISION_CHANGED");
        await pageB.unroute("**/api/v1/configuration-revisions");
        await expect(pageB.getByText(/CONFIGURATION_REVISION_CHANGED/)).toBeVisible();
        await expect(pageB.getByRole("button", { name: "保留本区修改并 rebase" })).toBeVisible();
        await pageB.getByRole("button", { name: "保留本区修改并 rebase" }).click();
        const rebasedValidation = await validateActiveSection(pageB);
        const retryRevision = await applyActiveSection(pageB);
        saveSection("system", sectionProof(rebasedValidation, retryRevision));

        let finalRevision = retryRevision;
        if (providerRaceValue !== llmTokenLimit) {
          await editLLMLimit(pageB, llmTokenLimit);
          await fillSummary(pageB, "providers", `${runID} normalize LLM output token limit`);
          const normalizationValidation = await validateActiveSection(pageB);
          finalRevision = await applyActiveSection(pageB);
          saveSection("providers", sectionProof(normalizationValidation, finalRevision));
        }

        const observed = await refreshSettings(pageB);
        expect(llmConfiguration(observed.active_revision).max_results).toBe(llmTokenLimit);
        expect(observed.active_revision.general.query_max_results).toBe(queryLimit);
        expect(observed.history.some((revision) => revision.id === winningRevision.id)).toBe(true);
        expect(observed.history.some((revision) => revision.id === retryRevision.id)).toBe(true);
        saveArtifact({
          conflict: {
            base_revision_id: base.id,
            base_revision_hash: base.hash,
            winning_revision_id: winningRevision.id,
            stale_status: staleResponse.status(),
            stale_code: staleProblem.code ?? "",
            retry_revision_id: retryRevision.id,
            llm_max_tokens: llmConfiguration(finalRevision).max_results,
            query_max_results: observed.active_revision.general.query_max_results,
          },
        });
      });
    } finally {
      await closeContexts(contextA, contextB);
    }
  });

  test("settings.secret-create", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const existing = currentArtifact().secret;
    await proveCapability(tracker, testInfo, {
      capabilityID: "settings.secret-create",
      uiAction: existing
        ? "从真实 Settings history 重新读取本轮 Secret reference metadata"
        : "从 Secret references 分区创建本轮 Kubernetes synthetic write-only Secret Version，并 validate/apply reference",
      expectedOperations: existing
        ? ["GET /api/v1/settings"]
        : ["POST /api/v1/secrets"],
      uiResult: "只回显 version ID/fingerprint/state，Secret value 在请求后和刷新后均不回显；reference Revision durable",
    }, async () => {
      if (existing) {
        const snapshot = await openSettings(page);
        const revision = snapshot.history.find((item) => item.id === existing.revision_id);
        expect(revision?.secret_references.some((item) => item.secret_version_id === existing.id)).toBe(true);
        expect(JSON.stringify(snapshot)).not.toContain(secretValue);
        return;
      }

      await openSettings(page);
      await selectSection(page, "secret-references");
      await page.getByRole("button", { name: "创建 Secret version" }).click();
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByRole("heading", { name: "创建新的 Secret version" })).toBeVisible();
      await dialog.getByRole("combobox", { name: "Provider" }).click();
      await page.getByRole("option", { name: "Kubernetes", exact: true }).click();
      await dialog.getByRole("textbox", { name: "Purpose" }).fill(secretPurpose);
      await dialog.getByLabel("Secret value").fill(secretValue);
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/secrets",
        () => dialog.getByRole("button", { name: "创建 version" }).click(),
      );
      const secret = await responseData<{
        id: string;
        provider: string;
        purpose: string;
        state: string;
        fingerprint: string;
        value?: string;
      }>(response);
      expect(secret.id).toBeTruthy();
      expect(secret.provider).toBe("kubernetes");
      expect(secret.purpose).toBe(secretPurpose);
      expect(secret.state).toBe("configured");
      expect(secret.fingerprint).toMatch(/^[0-9a-f]{20}$/);
      expect(secret.value).toBeUndefined();
      expect(JSON.stringify(secret)).not.toContain(secretValue);
      await expect(page.getByText(secretValue, { exact: true })).toHaveCount(0);
      await expect(page.getByText(secret.fingerprint, { exact: true })).toBeVisible();

      const summary = `${runID} synthetic Secret reference`;
      await fillSummary(page, "secret-references", summary);
      const validation = await validateActiveSection(page);
      const revision = await applyActiveSection(page);
      saveSection("secret-references", sectionProof(validation, revision));
      const observed = await refreshSettings(page);
      const reference = observed.active_revision.secret_references.find((item) => item.secret_version_id === secret.id);
      expect(reference).toMatchObject({ provider: "kubernetes", purpose: secretPurpose, fingerprint: secret.fingerprint });
      expect(JSON.stringify(observed)).not.toContain(secretValue);
      saveArtifact({
        secret: {
          id: secret.id,
          provider: secret.provider,
          purpose: secret.purpose,
          fingerprint: secret.fingerprint,
          state: secret.state,
          value_echoed: false,
          revision_id: revision.id,
        },
      });
    });
  });

  test("settings.validate", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    const retained = artifact.sections?.scopes && artifact.sections?.policies;
    await proveCapability(tracker, testInfo, {
      capabilityID: "settings.validate",
      uiAction: retained
        ? "从真实 Revision history 复核五个独立 section 的 validation/apply 证据"
        : "分别从 Scope 与 Policy 原控件修改、填写 run_id 摘要、服务端 validate、审阅 Diff 并确认 apply；结合已完成的 System/Provider/Secret section 形成五区闭环",
      expectedOperations: retained
        ? ["GET /api/v1/settings"]
        : ["POST /api/v1/settings/validate"],
      uiResult: "五个 section 均有独立 validation identity、Diff、immutable Revision、Worker succeeded 与刷新回显",
    }, async () => {
      if (retained) {
        const revisionIDs = Object.values(artifact.sections ?? {}).flatMap((proof) => proof?.revision_id ? [proof.revision_id] : []);
        await verifyRetainedRevisions(page, revisionIDs);
        return;
      }
      expect(artifact.sections?.providers).toBeTruthy();
      expect(artifact.sections?.system).toBeTruthy();
      expect(artifact.sections?.["secret-references"]).toBeTruthy();

      const snapshot = await openSettings(page);
      const preRun = currentArtifact().pre_run!;
      const originalScope = preRun.scopes.find((scope) => scope.cluster_id === preRun.scope.cluster_id) ?? preRun.scope;
      const temporaryScopeName = `${originalScope.name} [${runSuffix}]`;
      await selectSection(page, "scopes");
      await fillField(page, "scopes.0.name", temporaryScopeName);
      const scopeSummary = `${runID} temporary Scope label`;
      await fillSummary(page, "scopes", scopeSummary);
      const scopeValidation = await validateActiveSection(page);
      const scopeRevision = await applyActiveSection(page);
      saveSection("scopes", sectionProof(scopeValidation, scopeRevision));
      let observed = await refreshSettings(page);
      expect(observed.active_revision.scopes.some((scope) => scope.name === temporaryScopeName)).toBe(true);

      await selectSection(page, "policies");
      await page.getByRole("button", { name: "添加 Policy" }).click();
      await fillField(page, `policies.${snapshot.active_revision.escalation_policies.length}.name`, policyName);
      const policyEditor = page.locator(".settings-object-editor");
      const enabledSwitch = policyEditor.getByRole("switch");
      if (await enabledSwitch.isChecked()) await enabledSwitch.click();
      await expect(enabledSwitch).not.toBeChecked();
      const policySummary = `${runID} disabled synthetic escalation policy`;
      await fillSummary(page, "policies", policySummary);
      const policyValidation = await validateActiveSection(page);
      const policyRevision = await applyActiveSection(page);
      saveSection("policies", sectionProof(policyValidation, policyRevision));
      observed = await refreshSettings(page);
      const policy = observed.active_revision.escalation_policies.find((item) => item.name === policyName);
      expect(policy).toMatchObject({ enabled: false, create_incident: true });

      const completed = currentArtifact().sections ?? {};
      for (const key of ["system", "scopes", "policies", "providers", "secret-references"] as const) {
        expect(completed[key]?.validation_id, `${key} validation identity`).toBeTruthy();
        expect(completed[key]?.revision_id, `${key} Revision identity`).toBeTruthy();
      }
    });
  });

  });

  test.describe("restore pre-run equivalent Settings", () => {
    test.skip(!restoreMode, "run with CLOUDOPS_REAL_INTEGRATION_SETTINGS_RESTORE=1 to exercise the restoration contract");

    test("settings.apply-restore", async ({ page }, testInfo) => {
      const tracker = trackBrowserEvidence(page);
      const artifact = currentArtifact();
      expect(artifact.pre_run).toBeTruthy();
      const initial = await openSettings(page);
      const alreadyRestored = initial.active_revision.hash === artifact.pre_run!.hash
        && equivalentConfiguration(initial.active_revision, artifact.pre_run!);
      await proveCapability(tracker, testInfo, {
        capabilityID: "settings.apply-restore",
        uiAction: alreadyRestored
          ? "从真实 Settings 刷新确认 active hash/config 与 pre-run 等价"
          : "逐 section 从原控件恢复 pre-run 值，最后用 pre-run summary validate/Diff/apply，使 active hash 精确回到 pre-run hash",
        expectedOperations: alreadyRestored
          ? ["GET /api/v1/settings"]
          : ["POST /api/v1/configuration-revisions"],
        uiResult: "active Configuration hash 与 pre-run hash 精确一致，Scope 内容等价，Worker succeeded",
      }, async () => {
        const preRun = artifact.pre_run!;
        let snapshot = initial;

        type RestoreStep = { key: SectionKey; apply: () => Promise<void> };
        const steps: RestoreStep[] = [];
        const externalProviderChanges = (Object.keys(externalProviderLabels) as Array<keyof typeof externalProviderLabels>)
          .filter((provider) => (
            snapshot.active_revision.providers.find((item) => item.provider === provider)?.enabled
            !== preRun.providers.find((item) => item.provider === provider)?.enabled
          ));
        if (llmConfiguration(snapshot.active_revision).timeout_ms !== llmConfiguration(preRun).timeout_ms
          || llmConfiguration(snapshot.active_revision).max_results !== llmConfiguration(preRun).max_results
          || externalProviderChanges.length > 0) {
          steps.push({
            key: "providers",
            apply: async () => {
              if (llmConfiguration(snapshot.active_revision).timeout_ms !== llmConfiguration(preRun).timeout_ms
                || llmConfiguration(snapshot.active_revision).max_results !== llmConfiguration(preRun).max_results) {
                await editLLMRuntime(
                  page,
                  llmConfiguration(preRun).timeout_ms,
                  llmConfiguration(preRun).max_results,
                );
              }
              for (const provider of externalProviderChanges) {
                const enabled = preRun.providers.find((item) => item.provider === provider)?.enabled;
                expect(enabled, `${provider} pre-run enabled state`).toBeDefined();
                await editProviderEnabled(page, provider, enabled!);
              }
            },
          });
        }
        if (snapshot.active_revision.general.query_max_results !== preRun.general.query_max_results) {
          steps.push({
            key: "system",
            apply: async () => {
              await selectSection(page, "system");
              await fillField(page, "system.query_max_results", preRun.general.query_max_results);
            },
          });
        }
        const currentScope = snapshot.active_revision.scopes.find((scope) => scope.cluster_id === preRun.scope.cluster_id);
        const originalScope = preRun.scopes.find((scope) => scope.cluster_id === preRun.scope.cluster_id) ?? preRun.scope;
        if (currentScope?.name !== originalScope.name) {
          steps.push({
            key: "scopes",
            apply: async () => {
              await selectSection(page, "scopes");
              const index = snapshot.active_revision.scopes.findIndex((scope) => scope.cluster_id === preRun.scope.cluster_id);
              await page.locator(".settings-object-list button").filter({ hasText: currentScope?.name ?? originalScope.name }).click();
              await fillField(page, `scopes.${Math.max(0, index)}.name`, originalScope.name);
            },
          });
        }
        if (snapshot.active_revision.escalation_policies.some((policy) => policy.name === policyName)) {
          steps.push({
            key: "policies",
            apply: async () => {
              await selectSection(page, "policies");
              await page.locator(".settings-object-list button").filter({ hasText: policyName }).click();
              await page.getByRole("button", { name: `从草稿移除 ${policyName}` }).click();
            },
          });
        }
        if (snapshot.active_revision.secret_references.some((reference) => reference.purpose === secretPurpose)) {
          steps.push({
            key: "secret-references",
            apply: async () => {
              await selectSection(page, "secret-references");
              await page.getByRole("button", { name: `从本地草稿移除 kubernetes/${secretPurpose} reference` }).click();
            },
          });
        }

        if (steps.length === 0 && snapshot.active_revision.hash !== preRun.hash) {
          const temporaryValue = preRun.general.query_max_results > 20
            ? preRun.general.query_max_results - 10
            : preRun.general.query_max_results + 10;
          await selectSection(page, "system");
          await fillField(page, "system.query_max_results", temporaryValue);
          await fillSummary(page, "system", `${runID} restoration summary preparation`);
          await validateActiveSection(page);
          await applyActiveSection(page);
          snapshot = await refreshSettings(page);
          steps.push({
            key: "system",
            apply: async () => {
              await selectSection(page, "system");
              await fillField(page, "system.query_max_results", preRun.general.query_max_results);
            },
          });
        }

        for (let index = 0; index < steps.length; index += 1) {
          if (index > 0) snapshot = await openSettings(page);
          const step = steps[index];
          await step.apply();
          const summary = index === steps.length - 1 ? preRun.summary : `${runID} restore ${step.key}`;
          await fillSummary(page, step.key, summary);
          await validateActiveSection(page);
          await applyActiveSection(page);
        }

        const restored = await refreshSettings(page);
        expect(restored.active_revision.hash).toBe(preRun.hash);
        expect(equivalentConfiguration(restored.active_revision, preRun)).toBe(true);
        expect(restored.history.some((revision) => (
          revision.id === restored.active_revision.id && revision.hash === preRun.hash
        ))).toBe(true);
        for (const section of Object.values(artifact.sections ?? {})) {
          expect(restored.history.some((revision) => revision.id === section.revision_id)).toBe(true);
        }
        await selectSection(page, "scopes");
        await expect(fieldControl(page, "scopes.0.name")).toHaveValue(originalScope.name);
        saveArtifact({
          restored: {
            revision_id: restored.active_revision.id,
            revision_number: restored.active_revision.number,
            revision_hash: restored.active_revision.hash,
            equivalent_to_pre_run: true,
          },
        });
      });
    });
  });
});
