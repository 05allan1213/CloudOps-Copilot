import { defineStore } from "pinia";

import {
  authorizeActionCard,
  authorizeOperationPlan,
  getAgentInvestigations,
  proposeOperationPlan,
  type ActionCard,
  type AgentRun,
  type OperationPlan,
  type OperationPlanProposalInput,
} from "../api/agent";
import { isApiError } from "../api/client";
import {
  executeActionCard,
  executeOperationPlan,
  getDevOpsWorkspace,
  type DevOpsWorkspace,
  type OperationExecution,
} from "../api/devops";
import { getResources, type ResourcePage } from "../api/infrastructure";

function failureMessage(error: unknown, fallback: string): string {
  if (!isApiError(error)) return fallback;
  const identity = [error.code, error.requestID && `Request ${error.requestID}`].filter(Boolean).join(" · ");
  return `${identity ? `${identity}：` : ""}${error.message}`;
}

export const useDevOpsWorkspaceStore = defineStore("devops-workspace", {
  state: () => ({
    workspace: null as DevOpsWorkspace | null,
    scenarioResources: null as ResourcePage | null,
    investigations: [] as AgentRun[],
    scenarioPlanningError: "",
    loading: false,
    loaded: false,
    mutatingSubjectID: "",
    error: "",
    notice: "",
  }),

  getters: {
    activeExecutions(state): OperationExecution[] {
      return state.workspace?.executions.filter((item) => item.status === "ready" || item.status === "running") ?? [];
    },
  },

  actions: {
    async load(preserve = false, signal?: AbortSignal) {
      if (this.loading) return;
      this.loading = true;
      this.error = "";
      try {
        const [workspaceResult, resourceResult, investigationResult] = await Promise.allSettled([
          getDevOpsWorkspace(signal),
          getResources({ cluster: "cloudops-local", namespace: "demo", kind: ["Deployment"], search: "cloudops-scenario-fault", limit: 20 }, signal),
          getAgentInvestigations(signal),
        ]);
        if (workspaceResult.status === "rejected") throw workspaceResult.reason;
        this.workspace = workspaceResult.value;
        this.scenarioResources = resourceResult.status === "fulfilled" ? resourceResult.value : null;
        this.investigations = investigationResult.status === "fulfilled" ? investigationResult.value : [];
        const auxiliaryFailures = [
          resourceResult.status === "rejected" ? "Kubernetes Deployment projection" : "",
          investigationResult.status === "rejected" ? "Agent Investigation index" : "",
        ].filter(Boolean);
        this.scenarioPlanningError = auxiliaryFailures.length
          ? `${auxiliaryFailures.join("、")} 读取失败；不会创建不完整的 Operation Plan。`
          : "";
        this.loaded = true;
      } catch (error) {
        if (signal?.aborted) return;
        if (!preserve) this.workspace = null;
        this.error = failureMessage(error, "DevOps Workspace 读取失败，请检查 API 与 MySQL runtime。");
      } finally {
        this.loading = false;
      }
    },

    async authorizeCard(card: ActionCard, reason: string) {
      await this.runMutation(card.id, async () => {
        await authorizeActionCard(card.id, card.content_hash, reason);
        this.notice = "Action Authorization 已绑定当前 exact Action Card。";
      });
    },

    async authorizePlan(plan: OperationPlan, reason: string) {
      await this.runMutation(plan.id, async () => {
        await authorizeOperationPlan(plan.id, plan.content_hash, reason);
        this.notice = "Action Authorization 已绑定当前 immutable Operation Plan。";
      });
    },

    async executeCard(card: ActionCard): Promise<OperationExecution | null> {
      let execution: OperationExecution | null = null;
      await this.runMutation(card.id, async () => {
        execution = await executeActionCard(card.id, card.content_hash);
        this.notice = "本地可逆动作已按 exact hash 排队。";
      });
      return execution;
    },

    async executePlan(plan: OperationPlan): Promise<OperationExecution | null> {
      let execution: OperationExecution | null = null;
      await this.runMutation(plan.id, async () => {
        execution = await executeOperationPlan(plan.id, plan.content_hash);
        this.notice = "Operation Plan 已按 exact hash 排队；Worker 将再次检查 authority、expiry 与 preconditions。";
      });
      return execution;
    },

    async proposeScenarioPlan(input: OperationPlanProposalInput): Promise<OperationPlan | null> {
      let plan: OperationPlan | null = null;
      await this.runMutation(input.run_id, async () => {
        plan = await proposeOperationPlan(input);
        this.notice = "已基于 Scenario Investigation 与当前 Kubernetes resourceVersion 创建 immutable Operation Plan；尚未授权。";
      });
      return plan;
    },

    async runMutation(subjectID: string, command: () => Promise<void>) {
      if (this.mutatingSubjectID) return;
      this.mutatingSubjectID = subjectID;
      this.error = "";
      this.notice = "";
      try {
        await command();
        await this.load(true);
      } catch (error) {
        this.error = failureMessage(error, "受控操作命令失败。");
      } finally {
        this.mutatingSubjectID = "";
      }
    },

    clearFeedback() {
      this.error = "";
      this.notice = "";
    },
  },
});
