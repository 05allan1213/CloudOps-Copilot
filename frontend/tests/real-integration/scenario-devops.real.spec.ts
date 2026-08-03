import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const scenarioID = process.env.CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID || "";
const targetName = "cloudops-scenario-fault";
const freezeReason = `${runID} reversible Scenario freeze proof`;
const restoreReason = `${runID} restore Scenario freeze to disabled`;

interface ActionAuthorization {
  id: string;
  authorized_content_hash: string;
  reason: string;
}

interface ActionCard {
  id: string;
  run_id: string;
  action_type: string;
  target: Record<string, unknown>;
  parameters: Record<string, unknown>;
  preconditions: Array<Record<string, unknown>>;
  content_hash: string;
  status: string;
  authorization?: ActionAuthorization;
}

interface OperationPlan {
  id: string;
  run_id: string;
  configuration_revision_id: string;
  operation_type: string;
  target: Record<string, unknown>;
  parameters: Record<string, unknown>;
  intended_state: Record<string, unknown>;
  preconditions: Array<Record<string, unknown>>;
  verification_intent: Record<string, unknown>;
  content_hash: string;
  status: string;
  authorization?: ActionAuthorization;
}

interface OperationExecution {
  id: string;
  subject_id: string;
  status: "ready" | "running" | "succeeded" | "failed" | "precondition_failed" | "verification_failed" | "cancelled";
  result?: Record<string, unknown>;
  failure_code?: string;
  failure_summary?: string;
  verification?: {
    id: string;
    source: string;
    status: "passed" | "failed";
    summary: string;
    provider_identity: Record<string, unknown>;
    evidence: Record<string, unknown>;
  };
}

interface ChangeFreezeState {
  target: Record<string, unknown>;
  enabled: boolean;
  reason: string;
  row_version: number;
}

interface DevOpsWorkspace {
  operation_plans: OperationPlan[];
  action_cards: ActionCard[];
  executions: OperationExecution[];
  change_freezes: ChangeFreezeState[];
}

interface DevOpsArtifact {
  run_id: string;
  scenario_id: string;
  freeze_card_id?: string;
  freeze_card_hash?: string;
  freeze_authorization_id?: string;
  freeze_execution_id?: string;
  freeze_verification_id?: string;
  restore_card_id?: string;
  restore_card_hash?: string;
  restore_authorization_id?: string;
  restore_execution_id?: string;
  restore_verification_id?: string;
  final_enabled?: boolean;
  final_row_version?: number;
  operation_plan_id?: string;
  operation_plan_hash?: string;
  operation_plan_authorization_id?: string;
  operation_plan_execution_id?: string;
  operation_plan_verification_id?: string;
  final_replicas?: number;
}

function requiredScenarioID(): string {
  if (!scenarioID) throw new Error("CLOUDOPS_REAL_INTEGRATION_SCENARIO_ID is required");
  return scenarioID;
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return "data" in (body as { data?: T }) ? (body as { data: T }).data : body as T;
}

function currentArtifact(): DevOpsArtifact {
  return readRunArtifact<DevOpsArtifact>("objects/devops-action-cards.json") ?? {
    run_id: runID,
    scenario_id: requiredScenarioID(),
  };
}

function saveArtifact(update: Partial<DevOpsArtifact>) {
  recordRunArtifact("objects/devops-action-cards.json", { ...currentArtifact(), ...update });
}

function scenarioFreeze(workspace: DevOpsWorkspace): ChangeFreezeState | undefined {
  return workspace.change_freezes.find((item) => (
    item.target.scenario_id === requiredScenarioID()
    && item.target.namespace === "demo"
    && item.target.workload_name === targetName
  ));
}

function matchingCard(workspace: DevOpsWorkspace, enabled: boolean, reason: string): ActionCard | undefined {
  return workspace.action_cards.find((item) => (
    item.action_type === "local.change_freeze.set"
    && item.target.scenario_id === requiredScenarioID()
    && item.parameters.enabled === enabled
    && item.parameters.reason === reason
  ));
}

function matchingPlan(workspace: DevOpsWorkspace): OperationPlan | undefined {
  return workspace.operation_plans.find((item) => (
    item.operation_type === "kubernetes.deployment.scale"
    && item.target.scenario_id === requiredScenarioID()
  ));
}

async function openDevOps(page: Page) {
  await page.goto("/devops");
  await expect(page.getByRole("heading", { name: "DevOps Workspace", level: 1 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Change Freeze", level: 2 })).toBeVisible();
}

async function readWorkspace(page: Page): Promise<DevOpsWorkspace> {
  const refresh = page.getByRole("button", { name: "刷新", exact: true });
  await expect(refresh).toBeEnabled();
  const response = await waitForApiResponse(page, "GET /api/v1/devops", () => refresh.click());
  return responseData<DevOpsWorkspace>(response);
}

async function openSubject(page: Page, subject: { id: string; content_hash: string }) {
  await page.goto(`/devops?view=operations&subject=${encodeURIComponent(subject.id)}`);
  await expect(page.getByRole("heading", { name: "不可变 Subject", level: 3 })).toBeVisible();
  await expect(page.getByText(subject.id, { exact: true }).first()).toBeVisible();
  await expect(page.getByText(subject.content_hash, { exact: true }).first()).toBeVisible();
}

async function createFreezeCard(page: Page, enabled: boolean, reason: string): Promise<ActionCard> {
  await openDevOps(page);
  const freeze = scenarioFreeze(await readWorkspace(page));
  expect(freeze?.enabled ?? false).toBe(!enabled);
  const expectedVersion = freeze?.row_version ?? 0;
  await page.getByRole("button", {
    name: enabled ? "创建 Freeze Action Card" : "创建解冻 Action Card",
  }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", {
    name: enabled ? "创建 Change Freeze Action Card" : "创建解除 Change Freeze Action Card",
  })).toBeVisible();
  await dialog.getByRole("textbox", { name: "命令原因" }).fill(reason);
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/agent/action-cards",
    () => dialog.getByRole("button", { name: "创建 exact Action Card" }).click(),
  );
  const card = await responseData<ActionCard>(response);
  expect(card.action_type).toBe("local.change_freeze.set");
  expect(card.target.scenario_id).toBe(requiredScenarioID());
  expect(card.parameters).toMatchObject({ enabled, reason });
  expect(card.preconditions).toContainEqual(expect.objectContaining({
    type: "local.change_freeze",
    expected_enabled: !enabled,
    expected_version: expectedVersion,
  }));
  expect(card.status).toBe("proposed");
  await expect(page).toHaveURL(new RegExp(`subject=${card.id}`));
  await openSubject(page, card);
  const workspace = await readWorkspace(page);
  expect(workspace.action_cards.find((item) => item.id === card.id)?.status).toBe("proposed");
  return card;
}

async function authorizeCard(page: Page, initial: ActionCard, reason: string): Promise<ActionCard> {
  let workspace = await readWorkspace(page);
  let card = workspace.action_cards.find((item) => item.id === initial.id) ?? initial;
  if (!card.authorization) {
    await openSubject(page, card);
    await page.getByRole("button", { name: "授权精确 Hash" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: "负责人审查 · 精确授权" })).toBeVisible();
    await expect(dialog.getByText(card.content_hash, { exact: true })).toBeVisible();
    await dialog.getByRole("textbox", { name: "命令原因" }).fill(reason);
    const response = await waitForApiResponse(
      page,
      "POST /api/v1/agent/action-cards/{id}/authorizations",
      () => dialog.getByRole("button", { name: "授权精确 Hash" }).click(),
    );
    card = await responseData<ActionCard>(response);
  }
  expect(card.authorization?.authorized_content_hash).toBe(card.content_hash);
  expect(card.authorization?.reason).toContain(runID);
  await openSubject(page, card);
  workspace = await readWorkspace(page);
  const durable = workspace.action_cards.find((item) => item.id === card.id);
  expect(durable?.status).toBe("authorized");
  expect(durable?.authorization?.authorized_content_hash).toBe(card.content_hash);
  if (workspace.executions.some((item) => item.subject_id === card.id)) {
    await expect(page.getByText("当前状态没有可用 DevOps 命令。")).toBeVisible();
  } else {
    await expect(page.getByRole("button", { name: "排队执行" })).toBeVisible();
  }
  return durable ?? card;
}

async function waitForTerminalExecution(
  page: Page,
  card: { id: string },
  initial?: OperationExecution,
): Promise<{ execution: OperationExecution; workspace: DevOpsWorkspace }> {
  let execution = initial;
  let workspace = await readWorkspace(page);
  execution = workspace.executions.find((item) => item.subject_id === card.id) ?? execution;
  for (let attempt = 0; attempt < 60 && (!execution || ["ready", "running"].includes(execution.status)); attempt += 1) {
    await page.waitForTimeout(1_500);
    workspace = await readWorkspace(page);
    execution = workspace.executions.find((item) => item.subject_id === card.id) ?? execution;
  }
  expect(execution, `Execution missing for ${card.id}`).toBeTruthy();
  expect(execution?.status, `${execution?.failure_code ?? ""} ${execution?.failure_summary ?? ""}`).toBe("succeeded");
  expect(execution?.verification?.status).toBe("passed");
  return { execution: execution!, workspace };
}

async function executeCard(page: Page, card: ActionCard): Promise<{ execution: OperationExecution; workspace: DevOpsWorkspace }> {
  const workspace = await readWorkspace(page);
  const existing = workspace.executions.find((item) => item.subject_id === card.id);
  if (existing) return waitForTerminalExecution(page, card, existing);
  await openSubject(page, card);
  await page.getByRole("button", { name: "排队执行" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "执行本地可逆动作" })).toBeVisible();
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/agent/action-cards/{id}/executions",
    () => dialog.getByRole("button", { name: "排队执行" }).click(),
  );
  const initial = await responseData<OperationExecution>(response);
  expect(initial.subject_id).toBe(card.id);
  return waitForTerminalExecution(page, card, initial);
}

async function assertFreezeUI(page: Page, enabled: boolean, reason: string) {
  await openDevOps(page);
  const workspace = await readWorkspace(page);
  const freeze = scenarioFreeze(workspace);
  expect(freeze?.enabled ?? false).toBe(enabled);
  expect(freeze?.reason ?? "").toBe(reason);
  const section = page.getByRole("heading", { name: "Change Freeze", level: 2 }).locator("xpath=ancestor::section[1]");
  await expect(section).toContainText(reason);
  await expect(section).toContainText(enabled ? "已冻结" : "未冻结");
  return freeze!;
}

async function createOperationPlan(page: Page): Promise<OperationPlan> {
  await openDevOps(page);
  const workspace = await readWorkspace(page);
  const freeze = scenarioFreeze(workspace);
  expect(freeze?.enabled).toBe(false);
  await expect(page.getByRole("button", { name: "创建恢复计划" })).toBeEnabled();
  await page.getByRole("button", { name: "创建恢复计划" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "创建不可变 Scenario 恢复计划" })).toBeVisible();
  await expect(dialog).toContainText("replicas=0");
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/operation-plans",
    () => dialog.getByRole("button", { name: "创建精确 Plan" }).click(),
  );
  const plan = await responseData<OperationPlan>(response);
  expect(plan.operation_type).toBe("kubernetes.deployment.scale");
  expect(plan.target.scenario_id).toBe(requiredScenarioID());
  expect(plan.parameters).toEqual({ replicas: 0 });
  expect(plan.intended_state).toEqual({ replicas: 0 });
  expect(plan.preconditions).toContainEqual(expect.objectContaining({
    type: "local.change_freeze",
    expected_enabled: false,
    expected_version: freeze?.row_version,
  }));
  expect(plan.status).toBe("proposed");
  await expect(page).toHaveURL(new RegExp(`subject=${plan.id}`));
  await openSubject(page, plan);
  const durable = matchingPlan(await readWorkspace(page));
  expect(durable?.id).toBe(plan.id);
  expect(durable?.content_hash).toBe(plan.content_hash);
  return durable ?? plan;
}

async function authorizePlan(page: Page, initial: OperationPlan, reason: string): Promise<OperationPlan> {
  let workspace = await readWorkspace(page);
  let plan = workspace.operation_plans.find((item) => item.id === initial.id) ?? initial;
  if (!plan.authorization) {
    await openSubject(page, plan);
    await page.getByRole("button", { name: "授权精确 Hash" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: "负责人审查 · 精确授权" })).toBeVisible();
    await expect(dialog.getByText(plan.content_hash, { exact: true })).toBeVisible();
    await dialog.getByRole("textbox", { name: "命令原因" }).fill(reason);
    const response = await waitForApiResponse(
      page,
      "POST /api/v1/operation-plans/{id}/authorizations",
      () => dialog.getByRole("button", { name: "授权精确 Hash" }).click(),
    );
    plan = await responseData<OperationPlan>(response);
  }
  expect(plan.authorization?.authorized_content_hash).toBe(plan.content_hash);
  expect(plan.authorization?.reason).toContain(runID);
  await openSubject(page, plan);
  workspace = await readWorkspace(page);
  const durable = workspace.operation_plans.find((item) => item.id === plan.id);
  expect(durable?.status).toBe("authorized");
  expect(durable?.authorization?.authorized_content_hash).toBe(plan.content_hash);
  if (workspace.executions.some((item) => item.subject_id === plan.id)) {
    await expect(page.getByText("当前状态没有可用 DevOps 命令。")).toBeVisible();
  } else {
    await expect(page.getByRole("button", { name: "排队执行" })).toBeVisible();
  }
  return durable ?? plan;
}

async function executePlan(page: Page, plan: OperationPlan): Promise<{ execution: OperationExecution; workspace: DevOpsWorkspace }> {
  const workspace = await readWorkspace(page);
  const existing = workspace.executions.find((item) => item.subject_id === plan.id);
  if (existing) return waitForTerminalExecution(page, plan, existing);
  await openSubject(page, plan);
  await page.getByRole("button", { name: "排队执行" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "执行高影响 Operation Plan" })).toBeVisible();
  const response = await waitForApiResponse(
    page,
    "POST /api/v1/operation-plans/{id}/executions",
    () => dialog.getByRole("button", { name: "排队执行" }).click(),
  );
  const initial = await responseData<OperationExecution>(response);
  expect(initial.subject_id).toBe(plan.id);
  return waitForTerminalExecution(page, plan, initial);
}

async function assertScenarioRecoveredUI(page: Page, plan: OperationPlan, execution: OperationExecution) {
  await openSubject(page, plan);
  await readWorkspace(page);
  const matrix = page.getByTestId("devops-verification-matrix");
  await expect(matrix).toBeVisible();
  await expect(matrix).toContainText(execution.verification?.id ?? "missing-verification");
  await expect(matrix).toContainText("kubernetes");
  await expect(matrix).toContainText("验证通过");
  await openDevOps(page);
  await readWorkspace(page);
  await expect(page.getByText("Scenario fault 已恢复到 0 replicas。")).toBeVisible();
  await expect(page.getByText("副本就绪 0/0")).toBeVisible();
}

test.describe.serial("Scenario Action Card execution and reversible cleanup", () => {
  test("propose exact Scenario Change Freeze Action Card", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();

    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.action-card-generate",
      uiAction: "在 DevOps Change Freeze 区点击创建 Freeze Action Card，填写本轮 run_id 原因并确认 exact card",
      uiResult: "不可变 Action Card、当前 row version 前置条件与 exact hash 在刷新和直接重进后保持",
      ...(artifact.freeze_card_id ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      await openDevOps(page);
      const workspace = await readWorkspace(page);
      let card = artifact.freeze_card_id
        ? workspace.action_cards.find((item) => item.id === artifact.freeze_card_id)
        : matchingCard(workspace, true, freezeReason);
      if (!card) card = await createFreezeCard(page, true, freezeReason);
      expect(card.parameters).toMatchObject({ enabled: true, reason: freezeReason });
      await openSubject(page, card);
      saveArtifact({ freeze_card_id: card.id, freeze_card_hash: card.content_hash });
    });
  });

  test("authorize exact Scenario Change Freeze Action Card", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.freeze_card_id).toBeTruthy();
    await openDevOps(page);
    const workspace = await readWorkspace(page);
    const initial = workspace.action_cards.find((item) => item.id === artifact.freeze_card_id);
    expect(initial).toBeTruthy();

    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.action-card-authorize",
      uiAction: "从 Action Card 完整详情点击授权精确 Hash，并填写本轮可审计原因",
      uiResult: "刷新和直接重进后 Action Card 保持 authorized，authorized hash 与 immutable content hash 完全一致",
      ...(initial?.authorization ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      const card = await authorizeCard(page, initial!, `${runID} authorize exact freeze card hash`);
      saveArtifact({ freeze_authorization_id: card.authorization?.id });
    });
  });

  test("execute freeze and inverse unfreeze Action Cards through Worker", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.freeze_card_id).toBeTruthy();
    await openDevOps(page);
    let workspace = await readWorkspace(page);
    const primary = workspace.action_cards.find((item) => item.id === artifact.freeze_card_id);
    expect(primary).toBeTruthy();
    const alreadyRestored = artifact.final_enabled === false
      && Boolean(artifact.restore_execution_id)
      && scenarioFreeze(workspace)?.enabled === false;

    await proveCapability(tracker, testInfo, {
      capabilityID: "devops.action-card-execute",
      uiAction: "从已授权 card 点击排队执行，经 Worker 冻结；再从原 Change Freeze 控件创建、授权并执行 exact inverse card",
      uiResult: "两次 Worker execution 与 local verification 均持久化；刷新后先回显已冻结，最终回显未冻结",
      ...(alreadyRestored ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      if (alreadyRestored) {
        await assertFreezeUI(page, false, restoreReason);
        return;
      }

      const frozen = await executeCard(page, primary!);
      const frozenState = scenarioFreeze(frozen.workspace);
      expect(frozenState?.enabled).toBe(true);
      expect(frozenState?.reason).toBe(freezeReason);
      saveArtifact({
        freeze_execution_id: frozen.execution.id,
        freeze_verification_id: frozen.execution.verification?.id,
      });
      await assertFreezeUI(page, true, freezeReason);

      workspace = await readWorkspace(page);
      let restore = artifact.restore_card_id
        ? workspace.action_cards.find((item) => item.id === artifact.restore_card_id)
        : matchingCard(workspace, false, restoreReason);
      if (!restore) restore = await createFreezeCard(page, false, restoreReason);
      saveArtifact({ restore_card_id: restore.id, restore_card_hash: restore.content_hash });

      restore = await authorizeCard(page, restore, `${runID} authorize exact unfreeze card hash`);
      saveArtifact({ restore_authorization_id: restore.authorization?.id });
      const restored = await executeCard(page, restore);
      const finalState = scenarioFreeze(restored.workspace);
      expect(finalState?.enabled).toBe(false);
      expect(finalState?.reason).toBe(restoreReason);
      saveArtifact({
        restore_execution_id: restored.execution.id,
        restore_verification_id: restored.execution.verification?.id,
        final_enabled: false,
        final_row_version: finalState?.row_version,
      });
      await assertFreezeUI(page, false, restoreReason);
    });
  });

  test("propose exact Scenario recovery Operation Plan", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    await openDevOps(page);
    const workspace = await readWorkspace(page);
    let plan = artifact.operation_plan_id
      ? workspace.operation_plans.find((item) => item.id === artifact.operation_plan_id)
      : matchingPlan(workspace);

    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.operation-plan-propose",
      uiAction: "在 DevOps Scenario 恢复计划区点击创建恢复计划，并确认绑定当前 resourceVersion 与 freeze row version 的 replicas=0 immutable plan",
      uiResult: "Operation Plan、Kubernetes/Change Freeze 前置条件与 exact hash 在刷新和直接重进后保持",
      ...(plan ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      if (!plan) plan = await createOperationPlan(page);
      expect(plan.target.scenario_id).toBe(requiredScenarioID());
      expect(plan.parameters).toEqual({ replicas: 0 });
      await openSubject(page, plan);
      saveArtifact({ operation_plan_id: plan.id, operation_plan_hash: plan.content_hash });
    });
  });

  test("authorize exact Scenario recovery Operation Plan", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.operation_plan_id).toBeTruthy();
    await openDevOps(page);
    const workspace = await readWorkspace(page);
    const initial = workspace.operation_plans.find((item) => item.id === artifact.operation_plan_id);
    expect(initial).toBeTruthy();

    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.operation-plan-authorize",
      uiAction: "从 Scenario Operation Plan 完整详情点击授权精确 Hash，并填写本轮可审计原因",
      uiResult: "刷新和直接重进后 Plan 保持 authorized，authorized hash 与 immutable content hash 完全一致",
      ...(initial?.authorization ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      const plan = await authorizePlan(page, initial!, `${runID} authorize exact Scenario recovery plan hash`);
      saveArtifact({ operation_plan_authorization_id: plan.authorization?.id });
    });
  });

  test("execute Scenario recovery plan through Worker and Kubernetes", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.operation_plan_id).toBeTruthy();
    await openDevOps(page);
    const workspace = await readWorkspace(page);
    const plan = workspace.operation_plans.find((item) => item.id === artifact.operation_plan_id);
    expect(plan).toBeTruthy();
    const existing = workspace.executions.find((item) => item.subject_id === plan?.id);

    await proveCapability(tracker, testInfo, {
      capabilityID: "devops.operation-plan-execute",
      uiAction: "从已授权 Scenario Plan 点击排队执行，由 Worker 对 allowlisted Deployment scale 到 replicas=0",
      uiResult: "Kubernetes current verification passed；刷新、直接重进与 DevOps 首页均回显 replicas=0 和 durable execution",
      ...(existing?.status === "succeeded" ? { expectedOperations: ["GET /api/v1/devops"] } : {}),
    }, async () => {
      const result = await executePlan(page, plan!);
      expect(result.execution.verification?.source).toBe("kubernetes");
      expect(result.execution.verification?.status).toBe("passed");
      expect(result.execution.verification?.evidence).toMatchObject({
        expected_replicas: 0,
        deployment: { desired_replicas: 0, ready_replicas: 0 },
        change_freeze: { enabled: false, row_version: 2 },
      });
      saveArtifact({
        operation_plan_execution_id: result.execution.id,
        operation_plan_verification_id: result.execution.verification?.id,
        final_replicas: 0,
      });
      await assertScenarioRecoveredUI(page, plan!, result.execution);
    });
  });
});
