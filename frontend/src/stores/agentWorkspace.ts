import { defineStore } from "pinia";

import {
  attachAgentSnapshot,
  authorizeActionCard,
  authorizeOperationPlan,
  cancelAgentConsultation,
  cancelAgentInvestigation,
  createKnowledgeItem,
  getAgentConsultation,
  getAgentConsultations,
  getAgentInvestigation,
  getAgentInvestigations,
  getKnowledgeItems,
  getOperationPlans,
  getRunbookGuidance,
  openAgentEventStream,
  sendAgentMessage,
  updateKnowledgeItem,
  type ActionCard,
  type AgentContextSnapshot,
  type AgentRun,
  type AgentStreamEvent,
  type ConsultationDetail,
  type ConsultationMessage,
  type ConsultationSummary,
  type KnowledgeItem,
  type OperationPlan,
  type RunbookGuidance,
} from "../api/agent";
import { isApiError } from "../api/client";
import type { AgentPageContext } from "../utils/agentContext";

type AgentSelection = "consultation" | "investigation";
type StreamState = "connected" | "reconnecting" | "stopped";

let closeStream: (() => void) | undefined;
let refreshTimer: number | undefined;

function failureMessage(error: unknown, fallback: string): string {
  if (!isApiError(error)) return fallback;
  return `${error.code || "REQUEST_FAILED"}：${error.message}`;
}

function contextValue(snapshot: AgentContextSnapshot | undefined): string {
  if (!snapshot) return "";
  return JSON.stringify({
    cluster_id: snapshot.scope.cluster_id,
    environment: snapshot.scope.environment,
    namespaces: snapshot.scope.namespaces,
    resource_refs: snapshot.resource_refs,
    filters: snapshot.filters ?? {},
    from: snapshot.time_range.from,
    to: snapshot.time_range.to,
    query_definition_refs: snapshot.query_definition_refs ?? [],
    query_execution_refs: snapshot.query_execution_refs ?? [],
    evidence_refs: snapshot.evidence_refs ?? [],
  });
}

function draftValue(context: AgentPageContext | null): string {
  if (!context) return "";
  const input = context.input;
  return JSON.stringify({
    cluster_id: input.cluster_id,
    environment: input.environment,
    namespaces: input.namespaces,
    resource_refs: input.resource_refs,
    filters: input.filters ?? {},
    from: input.from,
    to: input.to,
    query_definition_refs: input.query_definition_refs ?? [],
    query_execution_refs: input.query_execution_refs ?? [],
    evidence_refs: input.evidence_refs ?? [],
  });
}

export const useAgentWorkspaceStore = defineStore("agent-workspace", {
  state: () => ({
    investigations: [] as AgentRun[],
    consultations: [] as ConsultationSummary[],
    knowledge: [] as KnowledgeItem[],
    runbooks: [] as RunbookGuidance[],
    operationPlans: [] as OperationPlan[],
    selection: "consultation" as AgentSelection,
    selectedID: "",
    consultation: null as ConsultationDetail | null,
    investigation: null as AgentRun | null,
    currentContext: null as AgentPageContext | null,
    route: "",
    loading: false,
    sending: false,
    mutating: false,
    loaded: false,
    error: "",
    notice: "",
    liveAnswer: "",
    streamState: "stopped" as StreamState,
  }),

  getters: {
    selectedRun(state): AgentRun | null {
      return state.selection === "investigation" ? state.investigation : state.consultation?.active_run ?? null;
    },
    activeSnapshot(state): AgentContextSnapshot | undefined {
      if (!state.consultation?.snapshots.length) return undefined;
      return state.consultation.snapshots[state.consultation.snapshots.length - 1];
    },
    contextMismatch(): boolean {
      if (!this.consultation) return false;
      if (!this.currentContext || this.currentContext.route !== this.route) return true;
      return contextValue(this.activeSnapshot) !== draftValue(this.currentContext);
    },
  },

  actions: {
    async loadIndex(force = false, preferredInvestigationID = "") {
      if (this.loading && !force) return;
      if (this.loaded && !force) {
        if (preferredInvestigationID) await this.selectInvestigationFromRoute(preferredInvestigationID);
        return;
      }
      this.loading = true;
      this.error = "";
      try {
        const [investigations, consultations, knowledge, runbooks, plans] = await Promise.all([
          getAgentInvestigations(),
          getAgentConsultations(),
          getKnowledgeItems(),
          getRunbookGuidance(),
          getOperationPlans(),
        ]);
        this.investigations = investigations;
        this.consultations = consultations;
        this.knowledge = knowledge;
        this.runbooks = runbooks;
        this.operationPlans = plans;
        this.loaded = true;
        if (preferredInvestigationID) {
          await this.selectInvestigationFromRoute(preferredInvestigationID);
        } else if (!this.selectedID) {
          if (consultations.length) await this.selectConsultation(consultations[0].id);
          else if (investigations.length) await this.selectInvestigation(investigations[0].id);
        }
      } catch (error) {
        this.error = failureMessage(error, "Agent Workspace 读取失败，请检查 API 与 MySQL runtime。 ");
      } finally {
        this.loading = false;
      }
    },

    async selectInvestigationFromRoute(id: string): Promise<boolean> {
      if (!id) return false;
      if (!this.investigations.some((item) => item.id === id)) {
        this.error = `Context Link 指向的 Investigation ${id} 不在当前持久化索引中。`;
        return false;
      }
      if (this.selection === "investigation" && this.selectedID === id && this.investigation) return true;
      await this.selectInvestigation(id);
      return this.selection === "investigation" && this.selectedID === id && this.investigation !== null;
    },

    setRoute(route: string) {
      this.route = route;
    },

    setCurrentContext(context: AgentPageContext | null) {
      this.currentContext = context;
    },

    async selectConsultation(id: string) {
      if (!id) return;
      this.stopStream();
      this.selection = "consultation";
      this.selectedID = id;
      this.investigation = null;
      this.liveAnswer = "";
      this.loading = true;
      this.error = "";
      try {
        this.consultation = await getAgentConsultation(id);
        this.startStream(id);
      } catch (error) {
        this.error = failureMessage(error, "Consultation 读取失败。 ");
      } finally {
        this.loading = false;
      }
    },

    async selectInvestigation(id: string) {
      if (!id) return;
      this.stopStream();
      this.selection = "investigation";
      this.selectedID = id;
      this.consultation = null;
      this.liveAnswer = "";
      this.loading = true;
      this.error = "";
      try {
        this.investigation = await getAgentInvestigation(id);
      } catch (error) {
        this.error = failureMessage(error, "Investigation 读取失败。 ");
      } finally {
        this.loading = false;
      }
    },

    startStream(id: string) {
      this.streamState = "reconnecting";
      closeStream = openAgentEventStream(id, (event) => this.receiveStreamEvent(event), () => {
        this.streamState = "reconnecting";
      }, () => {
        this.streamState = "connected";
      });
    },

    stopStream() {
      closeStream?.();
      closeStream = undefined;
      this.streamState = "stopped";
      if (refreshTimer !== undefined) window.clearTimeout(refreshTimer);
      refreshTimer = undefined;
    },

    receiveStreamEvent(event: AgentStreamEvent) {
      if (event.type === "answer.delta" && typeof event.payload.delta === "string") {
        this.liveAnswer += event.payload.delta;
      }
      if (refreshTimer !== undefined) window.clearTimeout(refreshTimer);
      refreshTimer = window.setTimeout(() => {
        void this.refreshSelection();
      }, event.type === "answer.delta" ? 250 : 80);
    },

    async refreshSelection() {
      try {
        if (this.selection === "consultation" && this.selectedID) {
          this.consultation = await getAgentConsultation(this.selectedID);
          if (this.consultation.active_run?.status !== "running") this.liveAnswer = "";
        } else if (this.selectedID) {
          this.investigation = await getAgentInvestigation(this.selectedID);
        }
        const [investigations, consultations] = await Promise.all([getAgentInvestigations(), getAgentConsultations()]);
        this.investigations = investigations;
        this.consultations = consultations;
      } catch (error) {
        this.error = failureMessage(error, "Agent 状态刷新失败。 ");
      }
    },

    async sendMessage(content: string) {
      if (!this.consultation || this.sending || !content.trim()) return;
      this.sending = true;
      this.error = "";
      this.notice = "";
      try {
        const idempotencyKey = globalThis.crypto?.randomUUID?.() ?? `message-${Date.now()}`;
        await sendAgentMessage(this.consultation.id, content.trim(), idempotencyKey);
        this.notice = "消息已持久化，Worker 将按当前 snapshot 执行 bounded tools。";
        await this.refreshSelection();
      } catch (error) {
        this.error = failureMessage(error, "消息发送失败。 ");
      } finally {
        this.sending = false;
      }
    },

    async cancelRun() {
      const run = this.selectedRun;
      if (!run || this.mutating || (run.status !== "pending" && run.status !== "running")) return;
      this.mutating = true;
      this.error = "";
      try {
        if (this.selection === "consultation" && this.consultation) await cancelAgentConsultation(this.consultation.id);
        else await cancelAgentInvestigation(run.id);
        this.notice = "取消请求已记录；已完成的 Evidence 与消息仍会保留。";
        await this.refreshSelection();
      } catch (error) {
        this.error = failureMessage(error, "取消请求失败。 ");
      } finally {
        this.mutating = false;
      }
    },

    async attachCurrentContext() {
      if (!this.consultation || !this.currentContext || this.currentContext.route !== this.route || this.mutating) return;
      this.mutating = true;
      this.error = "";
      try {
        await attachAgentSnapshot(this.consultation.id, this.currentContext.input);
        this.notice = "已显式创建新的不可变 Context Snapshot；旧 snapshot 保持不变。";
        await this.refreshSelection();
      } catch (error) {
        this.error = failureMessage(error, "附加当前上下文失败。 ");
      } finally {
        this.mutating = false;
      }
    },

    async saveKnowledgeFromMessage(message: ConsultationMessage, title: string) {
      const snapshot = this.activeSnapshot;
      if (!this.consultation || !snapshot || message.role !== "assistant" || this.mutating) return;
      this.mutating = true;
      this.error = "";
      try {
        const created = await createKnowledgeItem({
          title: title.trim(),
          content: message.content,
          source_consultation_id: this.consultation.id,
          source_message_id: message.id,
          cluster_id: snapshot.scope.cluster_id,
          environment: snapshot.scope.environment,
          namespaces: snapshot.scope.namespaces,
          resource_refs: snapshot.resource_refs,
        });
        this.knowledge = [created, ...this.knowledge.filter((item) => item.id !== created.id)];
        this.notice = `Knowledge 已由 Owner 确认并保存为 revision ${created.current_revision.revision}。`;
      } catch (error) {
        this.error = failureMessage(error, "Knowledge 保存失败。 ");
      } finally {
        this.mutating = false;
      }
    },

    async setKnowledgeStatus(item: KnowledgeItem, status: "active" | "disabled") {
      if (this.mutating) return;
      this.mutating = true;
      this.error = "";
      try {
        const updated = await updateKnowledgeItem(item.id, { status });
        this.knowledge = this.knowledge.map((candidate) => candidate.id === updated.id ? updated : candidate);
        this.notice = status === "active" ? "Knowledge 已启用新 revision。" : "Knowledge 已禁用，不会进入后续自动检索。";
      } catch (error) {
        this.error = failureMessage(error, "Knowledge 状态更新失败。 ");
      } finally {
        this.mutating = false;
      }
    },

    async authorizeCard(card: ActionCard, reason: string) {
      if (this.mutating) return;
      this.mutating = true;
      this.error = "";
      try {
        await authorizeActionCard(card.id, card.content_hash, reason);
        this.notice = "Owner 已确认 exact action card；本页面没有执行 mutation。";
        await this.refreshSelection();
      } catch (error) {
        this.error = failureMessage(error, "Action Card 授权失败。 ");
      } finally {
        this.mutating = false;
      }
    },

    async authorizePlan(plan: OperationPlan, reason: string) {
      if (this.mutating) return;
      this.mutating = true;
      this.error = "";
      try {
        const updated = await authorizeOperationPlan(plan.id, plan.content_hash, reason);
        this.operationPlans = this.operationPlans.map((item) => item.id === updated.id ? updated : item);
        this.notice = "Owner 已授权 exact Operation Plan；执行仍属于独立受控阶段。";
        await this.refreshSelection();
      } catch (error) {
        this.error = failureMessage(error, "Operation Plan 授权失败。 ");
      } finally {
        this.mutating = false;
      }
    },
  },
});
