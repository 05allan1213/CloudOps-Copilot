import { expect, test, type Page, type Response } from "@playwright/test";

import {
  proveCapability,
  readRunArtifact,
  recordRunArtifact,
  trackBrowserEvidence,
  waitForApiResponse,
} from "./support";

const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const scenarioID = "scenario-20260802180235-ba6defe2";
const scenarioWorkload = "cloudops-scenario-fault";
const artifactPath = "objects/agent-consultation.json";

interface AgentArtifact {
  run_id: string;
  scenario_id: string;
  consultation_id?: string;
  consultation_title?: string;
  initial_query_id?: string;
  attached_query_id?: string;
  navigation_query_id?: string;
  attached_snapshot_id?: string;
  quality_query_id?: string;
  quality_snapshot_id?: string;
  quality_revision_id?: string;
  first_prompt?: string;
  first_answer?: string;
  followup_prompt?: string;
  followup_answer?: string;
  preexisting_owner_messages?: number;
  preexisting_assistant_messages?: number;
  quality_preexisting_owner_messages?: number;
  quality_preexisting_assistant_messages?: number;
  knowledge_id?: string;
  knowledge_title?: string;
  knowledge_revision?: number;
  knowledge_content_marker?: string;
  knowledge_updated?: boolean;
  knowledge_deleted?: boolean;
  cancelled_run_id?: string;
  cancelled?: boolean;
}

function currentArtifact(): AgentArtifact {
  return readRunArtifact<AgentArtifact>(artifactPath) ?? { run_id: runID, scenario_id: scenarioID };
}

function saveArtifact(patch: Partial<AgentArtifact>) {
  recordRunArtifact(artifactPath, { ...currentArtifact(), ...patch });
}

async function responseData<T>(response: Response): Promise<T> {
  const body = await response.json() as T | { data: T };
  return (body && typeof body === "object" && "data" in body ? body.data : body) as T;
}

async function selectOption(page: Page, combobox: string | RegExp, option: string | RegExp) {
  await page.getByRole("combobox", { name: combobox }).click();
  await page.getByRole("option", { name: option, exact: typeof option === "string" }).click();
}

async function runScenarioLogQuery(page: Page, text: string): Promise<{ id: string; configuration_revision_id: string }> {
  await page.goto("/logs");
  await expect(page.getByRole("heading", { name: "日志分析", level: 1 })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Namespace" })).toBeEnabled();
  await selectOption(page, "Namespace", "demo");
  await selectOption(page, "Workload", new RegExp(scenarioWorkload));
  await page.getByRole("button", { name: "6h", exact: true }).click();
  await page.getByRole("textbox", { name: "日志文本过滤" }).fill(text);
  const response = await waitForApiResponse(page, "POST /api/v1/logs/queries", () => (
    page.getByRole("button", { name: "搜索日志" }).click()
  ));
  const query = await responseData<{ id: string; configuration_revision_id: string }>(response);
  expect(query.id).toBeTruthy();
  expect(query.configuration_revision_id).toBeTruthy();
  await expect(page).toHaveURL(new RegExp(`query=${query.id}`));
  await expect(page.getByTestId("app-main")).toContainText(scenarioWorkload);
  await expect(page.getByRole("button", { name: "交给 Agent" })).toBeEnabled();
  return query;
}

async function openFullAgentFromLogs(page: Page) {
  await page.getByRole("button", { name: "交给 Agent" }).click();
  const drawer = page.getByTestId("global-agent-drawer");
  await expect(drawer).toBeVisible();
  await drawer.getByRole("link", { name: "打开完整 Agent 工作台" }).click();
  await expect(page).toHaveURL(/\/agent/);
  await expect(page.getByTestId("agent-workspace")).toBeVisible();
}

async function openConsultation(page: Page, consultationID: string) {
  await waitForApiResponse(
    page,
    "GET /api/v1/agent/consultations/{id}/events",
    () => page.goto(`/agent?consultation=${consultationID}`),
  );
  await expect(page).toHaveURL(new RegExp(`consultation=${consultationID}`));
  await expect(page.getByTestId("agent-stream-state")).toContainText("实时同步");
}

async function sendMessage(page: Page, content: string): Promise<{ answer: string; streamingSeen: boolean }> {
  const workspace = page.getByTestId("agent-workspace");
  const ownerMessages = workspace.locator("article.owner-message");
  const assistantMessages = workspace.locator("article.assistant-message");
  const priorOwnerCount = await ownerMessages.count();
  const priorCount = await assistantMessages.count();
  await workspace.locator('textarea[name="agent_message"]').fill(content);
  await waitForApiResponse(
    page,
    "POST /api/v1/agent/consultations/{id}/messages",
    () => workspace.getByRole("button", { name: "发送消息" }).click(),
  );
  await expect(ownerMessages).toHaveCount(priorOwnerCount + 1);
  await expect(ownerMessages.last().getByText(content, { exact: true })).toBeVisible();

  let streamingSeen = false;
  await expect.poll(async () => {
    if (await page.getByText("Agent · Streaming", { exact: true }).isVisible()) streamingSeen = true;
    const status = await workspace.locator(".run-status-copy").textContent().catch(() => "");
    const count = await assistantMessages.count();
    if (status?.includes("调查失败")) throw new Error(`DeepSeek Consultation failed: ${status}`);
    if (count > priorCount) {
      const answer = await assistantMessages.last().innerText();
      if (/模型(?:没有返回可用答案| Provider 调用失败)|Agent 工作因内部依赖错误而失败/.test(answer)) {
        throw new Error(`DeepSeek Consultation returned a failure answer: ${answer.slice(0, 240)}`);
      }
    }
    return status?.includes("调查已完成") && count > priorCount;
  }, {
    message: "DeepSeek Consultation turn should stream and reach a durable completed answer",
    timeout: 600_000,
    intervals: [200, 300, 500, 1_000],
  }).toBe(true);
  const answer = await assistantMessages.last().innerText();
  expect(answer.length).toBeGreaterThan(160);
  return { answer, streamingSeen };
}

async function durableAnswerAfter(page: Page, prompt: string): Promise<string> {
  const messages = page.getByTestId("agent-workspace").locator("article.message");
  const count = await messages.count();
  for (let index = count - 1; index >= 0; index -= 1) {
    const message = messages.nth(index);
    if (!await message.getByText(prompt, { exact: true }).count()) continue;
    for (let answerIndex = index + 1; answerIndex < count; answerIndex += 1) {
      const candidate = messages.nth(answerIndex);
      const classes = await candidate.getAttribute("class") ?? "";
      if (classes.includes("owner-message")) break;
      if (classes.includes("assistant-message")) {
        const answer = await candidate.innerText();
        if (/模型(?:没有返回可用答案| Provider 调用失败)|Agent 工作因内部依赖错误而失败/.test(answer)) return "";
        return answer;
      }
    }
  }
  return "";
}

function assertFirstAnswerQuality(answer: string) {
  expect(answer.length).toBeGreaterThan(160);
  expect(answer).toMatch(new RegExp(`${scenarioWorkload}|REQUIRED_ENV`, "i"));
  expect(answer).toMatch(/Kubernetes|Prometheus|Metrics|指标|Elasticsearch|Logs|日志|Tempo|Trace/i);
  expect(answer).toMatch(/证据|Evidence|观察|observed/i);
  expect(answer).toMatch(/建议|未执行|没有执行|尚未|需要验证|证据不足/i);
}

async function showKnowledge(page: Page) {
  const tab = page.getByRole("tab", { name: /^Evidence / });
  await tab.click();
  await expect(page.getByRole("heading", { name: "Owner Knowledge" })).toBeVisible();
}

test.describe.serial("Scenario Agent Consultation, DeepSeek and Knowledge lifecycle", () => {
  test("telemetry.consultation-create", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    await proveCapability(tracker, testInfo, {
      capabilityID: "telemetry.consultation-create",
      uiAction: "从本轮真实 Logs 查询点击交给 Agent，在完整工作台以 run_id 标题创建 Consultation",
      uiResult: "刷新和直接重进后同一 Consultation、不可变 Snapshot、Scenario resource、absolute time 与 Query identity 保持",
      ...(artifact.consultation_id ? { expectedOperations: ["GET /api/v1/agent/consultations/{id}"] } : {}),
    }, async () => {
      if (artifact.consultation_id) {
        const activeQueryID = artifact.quality_query_id ?? artifact.attached_query_id ?? artifact.initial_query_id;
        await waitForApiResponse(
          page,
          "GET /api/v1/agent/consultations/{id}",
          () => page.goto(`/agent?consultation=${artifact.consultation_id}`),
        );
        await expect(page.getByRole("heading", { name: artifact.consultation_title ?? new RegExp(runID) })).toBeVisible();
        await expect(page.getByTestId("agent-workspace")).toContainText(scenarioWorkload);
        await page.getByRole("button", { name: "展开技术详情" }).click();
        await expect(page.getByTestId("agent-workspace")).toContainText(activeQueryID!);
        return;
      }

      const query = await runScenarioLogQuery(page, "");
      saveArtifact({ initial_query_id: query.id });
      await openFullAgentFromLogs(page);
      await page.getByRole("button", { name: "基于上下文" }).click();
      const title = `${runID} Scenario evidence consultation`;
      const dialog = page.getByRole("dialog");
      await expect(dialog.getByRole("heading", { name: "从当前上下文开始" })).toBeVisible();
      await dialog.getByRole("textbox", { name: "Consultation 标题" }).fill(title);
      const response = await waitForApiResponse(
        page,
        "POST /api/v1/agent/consultations",
        () => dialog.getByRole("button", { name: "创建 Consultation" }).click(),
      );
      const consultation = await responseData<{ id: string; context_snapshot: { id: string } }>(response);
      expect(consultation.id).toBeTruthy();
      await expect(page).toHaveURL(new RegExp(`consultation=${consultation.id}`));
      await expect(page.getByRole("heading", { name: title })).toBeVisible();
      saveArtifact({
        consultation_id: consultation.id,
        consultation_title: title,
        initial_query_id: query.id,
      });
      await waitForApiResponse(page, "GET /api/v1/agent/consultations/{id}", () => page.reload());
      await expect(page.getByRole("heading", { name: title })).toBeVisible();
      await expect(page.getByTestId("agent-workspace")).toContainText(scenarioWorkload);
      await page.getByRole("button", { name: "展开技术详情" }).click();
      await expect(page.getByTestId("agent-workspace")).toContainText(query.id);
    });
  });

  test("agent.consultation-multiturn-sse", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const initial = currentArtifact();
    expect(initial.consultation_id).toBeTruthy();
    const expectedOperations = ["GET /api/v1/agent/consultations/{id}/events"];
    if (!initial.first_answer) expectedOperations.push("POST /api/v1/agent/consultations/{id}/snapshots");
    if (!initial.first_answer || !initial.followup_answer) expectedOperations.push("POST /api/v1/agent/consultations/{id}/messages");

    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.consultation-multiturn-sse",
      uiAction: "从第二个真实 Logs Query 附加新 Snapshot，向 DeepSeek V4 Flash 发送 Scenario 初始问题与上下文追问",
      expectedOperations,
      uiResult: "SSE streaming、两轮语义回答、Context Snapshot 与消息在刷新和 consultation deep link 后保持",
    }, async () => {
      let artifact = currentArtifact();
      const consultationID = artifact.consultation_id!;
      await openConsultation(page, consultationID);

      if (!artifact.first_answer) {
        const query = await runScenarioLogQuery(page, "");
        await page.getByRole("button", { name: "交给 Agent" }).click();
        const drawer = page.getByTestId("global-agent-drawer");
        await expect(drawer).toBeVisible();
        await drawer.getByRole("tab", { name: "证据" }).click();
        const response = await waitForApiResponse(
          page,
          "POST /api/v1/agent/consultations/{id}/snapshots",
          () => drawer.getByRole("button", { name: "附加当前上下文" }).click(),
        );
        const snapshot = await responseData<{ id: string; configuration_revision_id: string }>(response);
        expect(snapshot.id).toBeTruthy();
        expect(snapshot.configuration_revision_id).toBe(query.configuration_revision_id);
        saveArtifact({
          quality_query_id: query.id,
          quality_snapshot_id: snapshot.id,
          quality_revision_id: snapshot.configuration_revision_id,
        });
        await drawer.getByRole("link", { name: "打开完整 Agent 工作台" }).click();
        await expect(page).toHaveURL(new RegExp(`consultation=${consultationID}`));
        saveArtifact({
          quality_preexisting_owner_messages: await page.locator("article.owner-message").count(),
          quality_preexisting_assistant_messages: await page.locator("article.assistant-message").count(),
        });
        artifact = currentArtifact();
      } else if (!artifact.navigation_query_id) {
        const navigationQuery = await runScenarioLogQuery(page, "");
        saveArtifact({ navigation_query_id: navigationQuery.id });
        await page.getByRole("button", { name: "交给 Agent" }).click();
        const drawer = page.getByTestId("global-agent-drawer");
        await expect(drawer).toBeVisible();
        await drawer.getByRole("link", { name: "打开完整 Agent 工作台" }).click();
        await expect(page).toHaveURL(new RegExp(`consultation=${consultationID}`));
        await expect(drawer).toBeHidden();
      }

      if (artifact.quality_preexisting_owner_messages === undefined || artifact.quality_preexisting_assistant_messages === undefined) {
        saveArtifact({
          quality_preexisting_owner_messages: await page.locator("article.owner-message").count(),
          quality_preexisting_assistant_messages: await page.locator("article.assistant-message").count(),
        });
        artifact = currentArtifact();
      }

      const firstPrompt = `请只依据当前质量重跑 Snapshot ${artifact.quality_snapshot_id} 的不可变证据，核查本轮 Scenario 服务故障。先指出 ${scenarioWorkload} 的实际异常对象和主要症状，再分别引用 Kubernetes、Prometheus、Elasticsearch 与 Tempo 中可核验的观察；对 REQUIRED_ENV 只在直接证据足够时下结论。最后列出尚未执行的处置建议与验证步骤，不得暗示你已经修改资源。`;
      if (!artifact.first_answer) {
        const recovered = await durableAnswerAfter(page, firstPrompt);
        if (recovered) {
          assertFirstAnswerQuality(recovered);
          saveArtifact({ first_prompt: firstPrompt, first_answer: recovered });
        } else {
          const turn = await sendMessage(page, firstPrompt);
          expect(turn.streamingSeen, "first DeepSeek answer should be observed through live SSE chunks").toBe(true);
          assertFirstAnswerQuality(turn.answer);
          saveArtifact({ first_prompt: firstPrompt, first_answer: turn.answer });
        }
        artifact = currentArtifact();
      }

      const followupPrompt = `继续基于 Snapshot ${artifact.quality_snapshot_id} 和上一轮回答：REQUIRED_ENV 是否已被证据证明为根因？如果不能证明，请明确缺少哪项直接证据；如果能，请指出具体证据。并说明当前 replicas=0/recovered 是观察状态，还是你执行的动作。`;
      if (!artifact.followup_answer) {
        const recovered = await durableAnswerAfter(page, followupPrompt);
        const answer = recovered || (await sendMessage(page, followupPrompt)).answer;
        expect(answer).toMatch(/REQUIRED_ENV/i);
        expect(answer).toMatch(/证据|Evidence|直接|证明|不足/i);
        expect(answer).toMatch(/replicas|副本|recovered/i);
        expect(answer).toMatch(/观察|状态|未执行|没有执行|不是.*执行/i);
        saveArtifact({ followup_prompt: followupPrompt, followup_answer: answer });
      }

      artifact = currentArtifact();
      await openConsultation(page, consultationID);
      await expect(page.getByText(firstPrompt, { exact: true })).toBeVisible();
      await expect(page.getByText(followupPrompt, { exact: true })).toBeVisible();
      await expect(page.locator("article.owner-message")).toHaveCount((artifact.quality_preexisting_owner_messages ?? 0) + 2);
      await expect(page.locator("article.assistant-message")).toHaveCount((artifact.quality_preexisting_assistant_messages ?? 0) + 2);
      await expect(page.getByTestId("agent-workspace")).toContainText(artifact.quality_snapshot_id!);
      await page.getByRole("button", { name: "展开技术详情" }).click();
      await expect(page.getByTestId("agent-workspace")).toContainText(artifact.quality_query_id!);
    });
  });

  test("agent.knowledge-create", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.consultation_id).toBeTruthy();
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.knowledge-create",
      uiAction: "从本轮第二条 durable DeepSeek assistant message 点击确认为 Knowledge，并填写 run_id 标题",
      uiResult: "刷新后 Knowledge revision 1、来源 Consultation/Message 和非敏感内容保持权威回显",
      ...(artifact.knowledge_id ? { expectedOperations: ["GET /api/v1/knowledge-items"] } : {}),
    }, async () => {
      await page.goto(`/agent?consultation=${artifact.consultation_id}`);
      if (!artifact.knowledge_id) {
        await page.getByRole("button", { name: "将这条 Agent 回复确认为 Knowledge" }).last().click();
        const dialog = page.getByRole("dialog");
        const title = `${runID} Scenario evidence finding`;
        await dialog.getByRole("textbox", { name: "Knowledge 标题" }).fill(title);
        const response = await waitForApiResponse(
          page,
          "POST /api/v1/knowledge-items",
          () => dialog.getByRole("button", { name: "Owner 确认保存" }).click(),
        );
        const knowledge = await responseData<{ id: string; current_revision: { revision: number } }>(response);
        expect(knowledge.id).toBeTruthy();
        saveArtifact({ knowledge_id: knowledge.id, knowledge_title: title, knowledge_revision: knowledge.current_revision.revision });
      }
      const current = currentArtifact();
      await page.reload();
      await showKnowledge(page);
      const row = page.locator("article.knowledge-row").filter({ hasText: current.knowledge_title! });
      await expect(row).toContainText("active");
      await expect(row).toContainText("revision 1");
      await expect(row).toContainText(/REQUIRED_ENV 尚未被当前 Evidence 证明为根因/);
    });
  });

  test("agent.knowledge-status", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    let artifact = currentArtifact();
    expect(artifact.knowledge_id).toBeTruthy();
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.knowledge-status",
      uiAction: "从 Owner Knowledge 单项控件禁用本轮 item，刷新确认后再重新启用检索",
      uiResult: "同一 Knowledge ID 在刷新后保持 active，lifecycle 已持久化且不可变 revision identity 未被重写",
    }, async () => {
      await page.goto(`/agent?consultation=${artifact.consultation_id}`);
      await showKnowledge(page);
      let row = page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! });
      const revision = artifact.knowledge_revision!;
      await waitForApiResponse(
        page,
        "PATCH /api/v1/knowledge-items/{id}",
        () => row.getByRole("button", { name: "禁用检索" }).click(),
      );
      await expect(row).toContainText("disabled");
      await page.reload();
      await showKnowledge(page);
      row = page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! });
      await expect(row).toContainText("disabled");
      await waitForApiResponse(
        page,
        "PATCH /api/v1/knowledge-items/{id}",
        () => row.getByRole("button", { name: "启用检索" }).click(),
      );
      await expect(row).toContainText("active");
      await expect(row).toContainText(`revision ${revision}`);
      artifact = currentArtifact();
      await page.reload();
      await showKnowledge(page);
      row = page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! });
      await expect(row).toContainText("active");
      await expect(row).toContainText(`revision ${revision}`);
    });
  });

  test("agent.knowledge-update", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    let artifact = currentArtifact();
    expect(artifact.knowledge_id).toBeTruthy();
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.knowledge-update",
      uiAction: "从本轮 Knowledge 的编辑控件追加 run_id 合成内容并确认新不可变 revision",
      uiResult: "同一 Knowledge ID 在刷新后显示单调递增 revision 和 Owner 确认的新内容",
      ...(artifact.knowledge_updated ? { expectedOperations: ["GET /api/v1/knowledge-items"] } : {}),
    }, async () => {
      await page.goto(`/agent?consultation=${artifact.consultation_id}`);
      await showKnowledge(page);
      let row = page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! });
      if (!artifact.knowledge_updated) {
        const previousRevision = artifact.knowledge_revision!;
        const marker = `${runID} Owner-confirmed revision ${previousRevision + 1}`;
        await row.getByRole("button", { name: `编辑 Knowledge ${artifact.knowledge_title}` }).click();
        const dialog = page.getByRole("dialog");
        const input = dialog.getByRole("textbox", { name: "Knowledge 内容" });
        await input.fill(`${await input.inputValue()}\n\n${marker}`);
        const response = await waitForApiResponse(
          page,
          "PATCH /api/v1/knowledge-items/{id}",
          () => dialog.getByRole("button", { name: "保存新 revision" }).click(),
        );
        const updated = await responseData<{ current_revision: { revision: number; content: string } }>(response);
        expect(updated.current_revision.revision).toBe(previousRevision + 1);
        expect(updated.current_revision.content).toContain(marker);
        saveArtifact({
          knowledge_revision: updated.current_revision.revision,
          knowledge_content_marker: marker,
          knowledge_updated: true,
        });
        artifact = currentArtifact();
      }
      await page.reload();
      await showKnowledge(page);
      row = page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! });
      await expect(row).toContainText(`revision ${artifact.knowledge_revision}`);
      await expect(row).toContainText(artifact.knowledge_content_marker!);
    });
  });

  test("agent.knowledge-delete", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.knowledge_id).toBeTruthy();
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.knowledge-delete",
      uiAction: "从本轮 Knowledge 的精确删除按钮核对 ID/revision 后确认永久删除",
      uiResult: "刷新后本轮 Knowledge 不再出现，其他历史 Knowledge 与来源 Consultation 保持",
      ...(artifact.knowledge_deleted ? { expectedOperations: ["GET /api/v1/knowledge-items"] } : {}),
    }, async () => {
      await page.goto(`/agent?consultation=${artifact.consultation_id}`);
      await showKnowledge(page);
      if (!artifact.knowledge_deleted) {
        await page.getByRole("button", { name: `删除 Knowledge ${artifact.knowledge_title}` }).click();
        const dialog = page.getByRole("dialog");
        await expect(dialog).toContainText(artifact.knowledge_id!);
        await waitForApiResponse(
          page,
          "DELETE /api/v1/knowledge-items/{id}",
          () => dialog.getByRole("button", { name: "确认删除此 Knowledge" }).click(),
        );
        saveArtifact({ knowledge_deleted: true });
      }
      await page.reload();
      await showKnowledge(page);
      await expect(page.locator("article.knowledge-row").filter({ hasText: artifact.knowledge_title! })).toHaveCount(0);
      await expect(page.getByRole("heading", { name: artifact.consultation_title! })).toBeVisible();
    });
  });

  test("agent.consultation-cancel", async ({ page }, testInfo) => {
    const tracker = trackBrowserEvidence(page);
    const artifact = currentArtifact();
    expect(artifact.consultation_id).toBeTruthy();
    await proveCapability(tracker, testInfo, {
      capabilityID: "agent.consultation-cancel",
      uiAction: "从本轮 Consultation 发送第三条消息，并立即点击取消当前 Agent 运行",
      uiResult: "刷新和 deep link 后 run 保持 cancelled，原两轮消息、回答、Snapshot 与 Evidence 均保留",
      ...(artifact.cancelled ? { expectedOperations: ["GET /api/v1/agent/consultations/{id}"] } : {}),
    }, async () => {
      await page.goto(`/agent?consultation=${artifact.consultation_id}`);
      if (!artifact.cancelled) {
        const prompt = "再次检查当前 Snapshot，但在形成新结论前等待 Owner 取消；不要执行任何变更。";
        await page.locator('textarea[name="agent_message"]').fill(prompt);
        const messageResponse = await waitForApiResponse(
          page,
          "POST /api/v1/agent/consultations/{id}/messages",
          () => page.getByRole("button", { name: "发送消息" }).click(),
        );
        const messageResult = await responseData<{ run: { id: string } }>(messageResponse);
        const cancel = page.getByRole("button", { name: "取消当前 Agent 运行" });
        await expect(cancel).toBeVisible({ timeout: 15_000 });
        await waitForApiResponse(
          page,
          "POST /api/v1/agent/consultations/{id}/cancel",
          () => cancel.click(),
        );
        await expect(page.locator(".run-status-copy")).toContainText("调查已取消");
        saveArtifact({ cancelled_run_id: messageResult.run.id, cancelled: true });
      }
      await waitForApiResponse(page, "GET /api/v1/agent/consultations/{id}", () => page.reload());
      await expect(page.locator(".run-status-copy")).toContainText("调查已取消");
      await expect(page.locator("article.owner-message")).toHaveCount((artifact.quality_preexisting_owner_messages ?? 0) + 3);
      await expect(page.locator("article.assistant-message")).toHaveCount((artifact.quality_preexisting_assistant_messages ?? 0) + 2);
      await expect(page.getByTestId("agent-workspace")).toContainText(artifact.quality_snapshot_id!);
    });
  });
});
