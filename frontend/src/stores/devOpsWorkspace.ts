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
import { apiErrorDetails, isApiError, type ApiErrorDetails } from "../api/client";
import {
  executeActionCard,
  executeOperationPlan,
  getDevOpsWorkspace,
  type DevOpsWorkspace,
  type OperationExecution,
} from "../api/devops";
import { getResources, type ResourcePage } from "../api/infrastructure";

type AuthoritySubject = ActionCard | OperationPlan;

export type DevOpsOwnershipKind = "incident" | "non_incident" | "unknown";
export type IncidentStage = "approval" | "delivery" | "verification";

export interface DevOpsSubjectOwnership {
  kind: DevOpsOwnershipKind;
  incidentID: string;
  reason: string;
}

export function classifyDevOpsRun(runID: string, investigations: AgentRun[]): DevOpsSubjectOwnership {
  const run = investigations.find((item) => item.id === runID);
  if (!run) {
    return {
      kind: "unknown",
      incidentID: "",
      reason: "未加载到 subject 对应的 Agent run，DevOps 写入口保持关闭。",
    };
  }
  if (run.incident_id || run.subject_type === "incident") {
    return {
      kind: "incident",
      incidentID: run.incident_id ?? "",
      reason: run.incident_id
        ? "该 subject 由 Incident 生命周期拥有。"
        : "Agent run 标记为 Incident，但缺少可恢复的 Incident ID。",
    };
  }
  return {
    kind: "non_incident",
    incidentID: "",
    reason: `Agent run subject 为 ${run.subject_type}，保留 DevOps 全局/非事故操作责任。`,
  };
}

export function classifyDevOpsSubject(
  subject: AuthoritySubject,
  executions: OperationExecution[],
  investigations: AgentRun[],
): DevOpsSubjectOwnership {
  const incidentExecution = executions.find((item) => item.subject_id === subject.id && item.incident_id);
  if (incidentExecution?.incident_id) {
    return {
      kind: "incident",
      incidentID: incidentExecution.incident_id,
      reason: "当前 execution 已绑定 Incident，事故写操作只在 Incident 生命周期中进行。",
    };
  }
  const execution = executions.find((item) => item.subject_id === subject.id);
  return classifyDevOpsRun(subject.run_id || execution?.run_id || "", investigations);
}

export function incidentStageHref(incidentID: string, stage: IncidentStage): string {
  const normalized = incidentID.trim();
  return normalized ? `/incidents/${encodeURIComponent(normalized)}#${stage}` : "";
}

function failureMessage(error: unknown, fallback: string): string {
  if (!isApiError(error)) return fallback;
  const identity = [error.code, error.requestID && `Request ${error.requestID}`].filter(Boolean).join(" · ");
  return `${identity ? `${identity}：` : ""}${error.message}`;
}

function ownershipFailure(ownership: DevOpsSubjectOwnership): ApiErrorDetails {
  const code = ownership.kind === "incident" ? "INCIDENT_OWNED_OPERATION" : "DEVOPS_OWNERSHIP_UNKNOWN";
  return {
    message: ownership.reason,
    status: null,
    code,
    requestID: "",
    traceID: "",
    idempotentReplay: null,
    nextSteps: ownership.incidentID
      ? ["前往 Incident 的 Approval、Delivery 或 Verification 阶段继续。"]
      : ["刷新 Agent run 与 DevOps projection 后重新核对 ownership。"],
  };
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
    failure: null as ApiErrorDetails | null,
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
      this.failure = null;
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
        this.failure = apiErrorDetails(error, "DevOps Workspace 读取失败，请检查 API 与 MySQL runtime。");
      } finally {
        this.loading = false;
      }
    },

    async authorizeCard(card: ActionCard, reason: string) {
      if (!this.allowSubjectMutation(card)) return;
      await this.runMutation(card.id, async () => {
        await authorizeActionCard(card.id, card.content_hash, reason);
        this.notice = "Action Authorization 已绑定当前 exact Action Card。";
      });
    },

    async authorizePlan(plan: OperationPlan, reason: string) {
      if (!this.allowSubjectMutation(plan)) return;
      await this.runMutation(plan.id, async () => {
        await authorizeOperationPlan(plan.id, plan.content_hash, reason);
        this.notice = "Action Authorization 已绑定当前 immutable Operation Plan。";
      });
    },

    async executeCard(card: ActionCard): Promise<OperationExecution | null> {
      if (!this.allowSubjectMutation(card)) return null;
      let execution: OperationExecution | null = null;
      await this.runMutation(card.id, async () => {
        execution = await executeActionCard(card.id, card.content_hash);
        this.notice = "本地可逆动作已按 exact hash 排队。";
      });
      return execution;
    },

    async executePlan(plan: OperationPlan): Promise<OperationExecution | null> {
      if (!this.allowSubjectMutation(plan)) return null;
      let execution: OperationExecution | null = null;
      await this.runMutation(plan.id, async () => {
        execution = await executeOperationPlan(plan.id, plan.content_hash);
        this.notice = "Operation Plan 已按 exact hash 排队；Worker 将再次检查 authority、expiry 与 preconditions。";
      });
      return execution;
    },

    async proposeScenarioPlan(input: OperationPlanProposalInput): Promise<OperationPlan | null> {
      const ownership = classifyDevOpsRun(input.run_id, this.investigations);
      if (ownership.kind !== "non_incident") {
        this.blockOwnership(ownership);
        return null;
      }
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
      this.failure = null;
      this.notice = "";
      try {
        await command();
        await this.load(true);
      } catch (error) {
        this.error = failureMessage(error, "受控操作命令失败。");
        this.failure = apiErrorDetails(error, "受控操作命令失败。");
      } finally {
        this.mutatingSubjectID = "";
      }
    },

    allowSubjectMutation(subject: AuthoritySubject): boolean {
      const ownership = classifyDevOpsSubject(
        subject,
        this.workspace?.executions ?? [],
        this.investigations,
      );
      if (ownership.kind === "non_incident") return true;
      this.blockOwnership(ownership);
      return false;
    },

    blockOwnership(ownership: DevOpsSubjectOwnership) {
      const failure = ownershipFailure(ownership);
      this.failure = failure;
      this.error = `${failure.code}：${failure.message}`;
      this.notice = "";
    },

    clearFeedback() {
      this.error = "";
      this.failure = null;
      this.notice = "";
    },
  },
});
