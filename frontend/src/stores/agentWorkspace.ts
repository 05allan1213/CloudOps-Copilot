import { defineStore } from "pinia";

import {
  attachAgentSnapshot,
  authorizeActionCard,
  authorizeOperationPlan,
  cancelAgentConsultation,
  cancelAgentInvestigation,
  createAgentConsultation,
  createKnowledgeItem,
  deleteKnowledgeItem,
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
  type AgentContextInput,
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
import { apiErrorDetails } from "../api/client";
import { invalidateQueryDomain } from "../composables/queryCache";
import { createRealtimeBatch, type RealtimeBatch } from "../composables/realtimeBatch";
import type { AgentPageContext } from "../utils/agentContext";

export type AgentSelection = "consultation" | "investigation";
export type StreamState = "connecting" | "connected" | "reconnecting" | "disconnected" | "stopped";

export interface AgentFailure {
  message: string;
  status: number | null;
  code: string;
  requestID: string;
  traceID: string;
  idempotentReplay: boolean | null;
  nextSteps: readonly string[];
  cause: string;
}

interface AgentStreamProjection {
  delta: string;
  eventCount: number;
  refreshDelayMs: number;
}

interface AgentDetailRequest {
  selection: AgentSelection;
  id: string;
  controller: AbortController;
  promise: Promise<void>;
}

let indexController: AbortController | undefined;
let detailController: AbortController | undefined;
let refreshController: AbortController | undefined;
let mutationController: AbortController | undefined;
let streamClose: (() => void) | undefined;
let streamGeneration = 0;
let refreshTimer: number | undefined;
let streamErrorCount = 0;
let streamBatch: RealtimeBatch<AgentStreamProjection> | undefined;
let detailRequest: AgentDetailRequest | undefined;
const seenEventIDs = new Set<string>();

function randomID(prefix: string): string {
  const uuid = globalThis.crypto?.randomUUID?.();
  return uuid ? `${prefix}-${uuid}` : `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function isAbortError(error: unknown): boolean {
  return Boolean(error && typeof error === "object" && "name" in error && (error as { name?: string }).name === "AbortError");
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

function usableContext(input: AgentContextInput | undefined): boolean {
  if (!input) return false;
  const from = Date.parse(input.from);
  const to = Date.parse(input.to);
  return Boolean(
    input.cluster_id.trim()
      && input.environment.trim()
      && input.namespaces.length
      && input.resource_refs.length
      && Number.isFinite(from)
      && Number.isFinite(to)
      && to > from
      && (input.query_execution_refs.length > 0 || input.evidence_refs.length > 0),
  );
}

function contextFailure(input: AgentContextInput | undefined): string {
  if (!input) return "需要先从 Logs、Traces、Alert 或 Incident 恢复真实 Context Snapshot。";
  if (!input.cluster_id.trim() || !input.environment.trim()) return "缺少真实 cluster/environment，入口保持阻止。";
  if (!input.namespaces.length || !input.resource_refs.length) return "缺少真实 namespace/resource，不能创建 Consultation。";
  if (!input.query_execution_refs.length && !input.evidence_refs.length) return "至少需要一个真实 query execution 或 Evidence 引用。";
  if (!Number.isFinite(Date.parse(input.from)) || !Number.isFinite(Date.parse(input.to)) || Date.parse(input.to) <= Date.parse(input.from)) return "时间范围必须是有效且递增的 UTC 区间。";
  return "当前上下文尚未满足 Consultation 的后端约束。";
}

function rememberEvent(id: string): boolean {
  if (seenEventIDs.has(id)) return false;
  seenEventIDs.add(id);
  if (seenEventIDs.size > 256) {
    const oldest = seenEventIDs.values().next().value as string | undefined;
    if (oldest) seenEventIDs.delete(oldest);
  }
  return true;
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
    creating: false,
    creatingMode: "" as "context" | "structured" | "free" | "",
    sending: false,
    mutating: false,
    loaded: false,
    error: "",
    failure: null as AgentFailure | null,
    notice: "",
    liveAnswer: "",
    streamState: "stopped" as StreamState,
    streamCursor: "",
    streamReconnects: 0,
    duplicateEvents: 0,
    lastEventAt: "",
    streamBatchCount: 0,
    streamBufferedEvents: 0,
    pendingMessageContent: "",
    pendingMessageIdempotencyKey: "",
  }),

  getters: {
    selectedRun(state): AgentRun | null {
      return state.selection === "investigation" ? state.investigation : state.consultation?.active_run ?? null;
    },
    activeSnapshot(state): AgentContextSnapshot | undefined {
      const snapshots = state.consultation?.snapshots ?? [];
      if (!snapshots.length) return undefined;
      return snapshots.find((snapshot) => snapshot.id === state.consultation?.active_snapshot_id) ?? snapshots[snapshots.length - 1];
    },
    contextMismatch(): boolean {
      if (!this.consultation) return false;
      if (!this.currentContext || this.currentContext.route !== this.route) return true;
      return contextValue(this.activeSnapshot) !== draftValue(this.currentContext);
    },
    contextReady(): boolean {
      return usableContext(this.currentContext?.input);
    },
    contextBlockReason(): string {
      return contextFailure(this.currentContext?.input);
    },
  },

  actions: {
    setFailure(error: unknown, fallback: string) {
      const details = apiErrorDetails(error, fallback);
      this.failure = { ...details, cause: error instanceof Error ? error.name : "unknown" };
      this.error = details.code ? `${details.code}：${details.message}` : details.message;
    },

    clearFailure() {
      this.failure = null;
      this.error = "";
    },

    async loadIndex(force = false, preferredInvestigationID = "", preferredConsultationID = "") {
      if (this.loading && !force) return;
      if (this.loaded && !force) {
        if (preferredConsultationID) await this.selectConsultationFromRoute(preferredConsultationID);
        else if (preferredInvestigationID) await this.selectInvestigationFromRoute(preferredInvestigationID);
        return;
      }
      if (force) invalidateQueryDomain("agent");
      indexController?.abort();
      const controller = new AbortController();
      indexController = controller;
      this.loading = true;
      this.clearFailure();
      try {
        const [investigations, consultations, knowledge, runbooks, plans] = await Promise.all([
          getAgentInvestigations(controller.signal),
          getAgentConsultations(controller.signal),
          getKnowledgeItems(controller.signal),
          getRunbookGuidance(controller.signal),
          getOperationPlans(controller.signal),
        ]);
        if (controller.signal.aborted || indexController !== controller) return;
        this.investigations = investigations;
        this.consultations = consultations;
        this.knowledge = knowledge;
        this.runbooks = runbooks;
        this.operationPlans = plans;
        this.loaded = true;
        if (preferredConsultationID) await this.selectConsultationFromRoute(preferredConsultationID);
        else if (preferredInvestigationID) await this.selectInvestigationFromRoute(preferredInvestigationID);
        else if (!this.selectedID) {
          if (consultations.length) await this.selectConsultation(consultations[0].id);
          else if (investigations.length) await this.selectInvestigation(investigations[0].id);
        }
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Agent Workspace 读取失败，请检查 API 与 MySQL runtime。 ");
      } finally {
        if (indexController === controller) {
          indexController = undefined;
          this.loading = false;
        }
      }
    },

    async selectConsultationFromRoute(id: string): Promise<boolean> {
      if (!id) return false;
      if (!this.consultations.some((item) => item.id === id)) {
        this.error = `Context Link 指向的 Consultation ${id} 不在当前持久化索引中。`;
        this.failure = { message: this.error, status: 404, code: "CONSULTATION_NOT_FOUND", requestID: "", traceID: "", idempotentReplay: null, nextSteps: [], cause: "route" };
        return false;
      }
      if (this.selection === "consultation" && this.selectedID === id && this.consultation) {
        if (this.streamState === "stopped") this.startStream(id);
        return true;
      }
      await this.selectConsultation(id);
      return this.selection === "consultation" && this.selectedID === id && this.consultation !== null;
    },

    async selectInvestigationFromRoute(id: string): Promise<boolean> {
      if (!id) return false;
      if (!this.investigations.some((item) => item.id === id)) {
        this.error = `Context Link 指向的 Investigation ${id} 不在当前持久化索引中。`;
        this.failure = { message: this.error, status: 404, code: "INVESTIGATION_NOT_FOUND", requestID: "", traceID: "", idempotentReplay: null, nextSteps: [], cause: "route" };
        return false;
      }
      if (this.selection === "investigation" && this.selectedID === id && this.investigation) return true;
      await this.selectInvestigation(id);
      return this.selection === "investigation" && this.selectedID === id && this.investigation !== null;
    },

    async selectFromRoute(consultationID: string, investigationID: string): Promise<boolean> {
      if (consultationID) return this.selectConsultationFromRoute(consultationID);
      if (investigationID) return this.selectInvestigationFromRoute(investigationID);
      return false;
    },

    setRoute(route: string) {
      this.route = route;
    },

    setCurrentContext(context: AgentPageContext | null) {
      this.currentContext = context;
    },

    async selectConsultation(id: string) {
      if (!id) return;
      const activeRequest = detailRequest;
      if (activeRequest?.selection === "consultation" && activeRequest.id === id && !activeRequest.controller.signal.aborted) {
        await activeRequest.promise;
        return;
      }
      this.stopStream();
      detailController?.abort();
      const controller = new AbortController();
      detailController = controller;
      this.selection = "consultation";
      this.selectedID = id;
      this.investigation = null;
      this.liveAnswer = "";
      this.loading = true;
      this.clearFailure();
      const promise = (async () => {
        try {
          const consultation = await getAgentConsultation(id, controller.signal);
          if (controller.signal.aborted || detailController !== controller || this.selectedID !== id) return;
          this.consultation = consultation;
          this.startStream(id);
        } catch (error) {
          if (!controller.signal.aborted) this.setFailure(error, "Consultation 读取失败。 ");
        } finally {
          if (detailController === controller) {
            detailController = undefined;
            this.loading = false;
          }
          if (detailRequest?.controller === controller) detailRequest = undefined;
        }
      })();
      detailRequest = { selection: "consultation", id, controller, promise };
      await promise;
    },

    async selectInvestigation(id: string) {
      if (!id) return;
      const activeRequest = detailRequest;
      if (activeRequest?.selection === "investigation" && activeRequest.id === id && !activeRequest.controller.signal.aborted) {
        await activeRequest.promise;
        return;
      }
      this.stopStream();
      detailController?.abort();
      const controller = new AbortController();
      detailController = controller;
      this.selection = "investigation";
      this.selectedID = id;
      this.consultation = null;
      this.liveAnswer = "";
      this.loading = true;
      this.clearFailure();
      const promise = (async () => {
        try {
          const investigation = await getAgentInvestigation(id, controller.signal);
          if (controller.signal.aborted || detailController !== controller || this.selectedID !== id) return;
          this.investigation = investigation;
        } catch (error) {
          if (!controller.signal.aborted) this.setFailure(error, "Investigation 读取失败。 ");
        } finally {
          if (detailController === controller) {
            detailController = undefined;
            this.loading = false;
          }
          if (detailRequest?.controller === controller) detailRequest = undefined;
        }
      })();
      detailRequest = { selection: "investigation", id, controller, promise };
      await promise;
    },

    startStream(id: string) {
      streamBatch?.dispose();
      streamClose?.();
      streamGeneration += 1;
      const generation = streamGeneration;
      seenEventIDs.clear();
      streamErrorCount = 0;
      this.streamCursor = "";
      this.streamBatchCount = 0;
      this.streamBufferedEvents = 0;
      this.streamState = "connecting";
      streamBatch = createRealtimeBatch<AgentStreamProjection>({
        maximumItems: 128,
        compact: (items) => ({
          delta: items.map((item) => item.delta).join(""),
          eventCount: items.reduce((count, item) => count + item.eventCount, 0),
          refreshDelayMs: Math.min(...items.map((item) => item.refreshDelayMs)),
        }),
        flush: (items) => this.applyStreamBatch(items, generation),
      });
      streamClose = openAgentEventStream(id, (event) => {
        if (generation === streamGeneration) this.receiveStreamEvent(event, generation);
      }, () => {
        if (generation !== streamGeneration) return;
        streamBatch?.flush();
        streamErrorCount += 1;
        this.streamReconnects += 1;
        this.streamState = streamErrorCount >= 3 ? "disconnected" : "reconnecting";
      }, () => {
        if (generation !== streamGeneration) return;
        streamErrorCount = 0;
        this.streamState = "connected";
      });
    },

    stopStream() {
      streamBatch?.dispose(true);
      streamBatch = undefined;
      streamGeneration += 1;
      streamClose?.();
      streamClose = undefined;
      if (refreshTimer !== undefined) {
        globalThis.clearTimeout(refreshTimer);
        refreshTimer = undefined;
      }
      refreshController?.abort();
      refreshController = undefined;
      indexController?.abort();
      detailController?.abort();
      mutationController?.abort();
      indexController = undefined;
      detailController = undefined;
      detailRequest = undefined;
      mutationController = undefined;
      seenEventIDs.clear();
      this.streamState = "stopped";
      this.streamCursor = "";
      this.streamBufferedEvents = 0;
      this.loading = false;
      this.creating = false;
      this.sending = false;
      this.mutating = false;
    },

    teardown() {
      this.stopStream();
      indexController?.abort();
      detailController?.abort();
      refreshController?.abort();
      mutationController?.abort();
      indexController = undefined;
      detailController = undefined;
      refreshController = undefined;
      mutationController = undefined;
      this.loading = false;
      this.creating = false;
      this.sending = false;
      this.mutating = false;
    },

    receiveStreamEvent(event: AgentStreamEvent, generation = streamGeneration) {
      if (generation !== streamGeneration || !event.id || (this.selection === "consultation" && event.consultation_id && event.consultation_id !== this.selectedID)) return;
      if (!rememberEvent(event.id)) {
        this.duplicateEvents += 1;
        return;
      }
      this.streamCursor = event.id;
      this.lastEventAt = event.created_at;
      const projection: AgentStreamProjection = {
        delta: event.type === "answer.delta" && typeof event.payload.delta === "string" ? event.payload.delta : "",
        eventCount: 1,
        refreshDelayMs: event.type === "answer.delta" ? 250 : 80,
      };
      if (!streamBatch) {
        this.applyStreamBatch([projection], generation);
        return;
      }
      streamBatch.enqueue(projection);
      this.streamBufferedEvents += 1;
    },

    applyStreamBatch(items: readonly AgentStreamProjection[], generation = streamGeneration) {
      if (generation !== streamGeneration || !items.length) return;
      const delta = items.map((item) => item.delta).join("");
      if (delta) this.liveAnswer += delta;
      this.streamBatchCount += 1;
      this.streamBufferedEvents = Math.max(0, this.streamBufferedEvents
        - items.reduce((count, item) => count + item.eventCount, 0));
      if (refreshTimer !== undefined) globalThis.clearTimeout(refreshTimer);
      const delay = Math.min(...items.map((item) => item.refreshDelayMs));
      refreshTimer = globalThis.setTimeout(() => {
        refreshTimer = undefined;
        void this.refreshSelection();
      }, delay) as unknown as number;
    },

    async refreshSelection() {
      if (!this.selectedID) return;
      invalidateQueryDomain("agent");
      refreshController?.abort();
      const controller = new AbortController();
      refreshController = controller;
      const selectedID = this.selectedID;
      const selection = this.selection;
      try {
        if (selection === "consultation") {
          const consultation = await getAgentConsultation(selectedID, controller.signal);
          if (!controller.signal.aborted && refreshController === controller && this.selectedID === selectedID) {
            this.consultation = consultation;
            if (consultation.active_run?.status !== "running") this.liveAnswer = "";
          }
        } else {
          const investigation = await getAgentInvestigation(selectedID, controller.signal);
          if (!controller.signal.aborted && refreshController === controller && this.selectedID === selectedID) this.investigation = investigation;
        }
        const [investigations, consultations] = await Promise.all([
          getAgentInvestigations(controller.signal),
          getAgentConsultations(controller.signal),
        ]);
        if (!controller.signal.aborted && refreshController === controller) {
          this.investigations = investigations;
          this.consultations = consultations;
        }
      } catch (error) {
        if (!controller.signal.aborted && refreshController === controller) this.setFailure(error, "Agent 状态刷新失败。 ");
      } finally {
        if (refreshController === controller) refreshController = undefined;
      }
    },

    async createConsultation(input: AgentContextInput, mode: "context" | "structured" | "free"): Promise<boolean> {
      if (this.creating) return false;
      if (!usableContext(input)) {
        this.error = contextFailure(input);
        this.failure = { message: this.error, status: 422, code: "CONTEXT_NOT_READY", requestID: "", traceID: "", idempotentReplay: null, nextSteps: ["从 Logs 或 Traces 完成一次查询并保留 Evidence。"], cause: "validation" };
        return false;
      }
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.creating = true;
      this.creatingMode = mode;
      this.clearFailure();
      this.notice = "";
      try {
        const created = await createAgentConsultation(input, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return false;
        this.notice = mode === "free"
          ? "自由查询已创建；它保留真实 Scope/Evidence，但不会关联未提供的事件。"
          : "Consultation 已创建并绑定不可变 Context Snapshot。";
        await this.refreshIndexAfterCreation(created.id, controller.signal);
        await this.selectConsultation(created.id);
        return true;
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Consultation 创建失败。 ");
        return false;
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.creating = false;
          this.creatingMode = "";
        }
      }
    },

    async refreshIndexAfterCreation(id: string, signal?: AbortSignal) {
      try {
        const [consultations, investigations] = await Promise.all([getAgentConsultations(signal), getAgentInvestigations(signal)]);
        if (signal?.aborted) return;
        this.consultations = consultations;
        this.investigations = investigations;
        if (!this.consultations.some((item) => item.id === id)) this.consultations = [{ id, title: "新 Consultation", status: "open", active_snapshot_id: "", scope: this.currentContext?.input ? { name: "", cluster_id: this.currentContext.input.cluster_id, environment: this.currentContext.input.environment, namespaces: this.currentContext.input.namespaces, active: true } : { name: "", cluster_id: "", environment: "", namespaces: [], active: false }, message_count: 0, created_at: new Date().toISOString(), updated_at: new Date().toISOString() }, ...this.consultations];
      } catch (error) {
        if (!signal?.aborted && !isAbortError(error)) this.setFailure(error, "Consultation 已创建，但历史索引刷新失败。 ");
      }
    },

    async sendMessage(content: string) {
      if (!this.consultation || this.sending || !content.trim()) return;
      const consultationID = this.consultation.id;
      const normalized = content.trim();
      if (this.pendingMessageContent !== normalized) {
        this.pendingMessageContent = normalized;
        this.pendingMessageIdempotencyKey = randomID("agent-message");
      }
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.sending = true;
      this.clearFailure();
      this.notice = "";
      try {
        await sendAgentMessage(consultationID, normalized, this.pendingMessageIdempotencyKey, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.pendingMessageContent = "";
        this.pendingMessageIdempotencyKey = "";
        this.notice = "消息已持久化，Worker 将按当前 snapshot 执行 bounded tools。";
        this.startStream(consultationID);
        await this.refreshSelection();
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "消息发送失败；再次提交相同内容会复用同一个 Idempotency-Key。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.sending = false;
        }
      }
    },

    async cancelRun() {
      const run = this.selectedRun;
      if (!run || this.mutating || (run.status !== "pending" && run.status !== "running")) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        if (this.selection === "consultation" && this.consultation) await cancelAgentConsultation(this.consultation.id, controller.signal);
        else await cancelAgentInvestigation(run.id, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.notice = "取消请求已记录；已完成的 Evidence 与消息仍会保留。";
        await this.refreshSelection();
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "取消请求失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async attachCurrentContext() {
      if (!this.consultation || !this.currentContext || this.currentContext.route !== this.route || this.mutating) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        await attachAgentSnapshot(this.consultation.id, this.currentContext.input, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.notice = "已显式创建新的不可变 Context Snapshot；旧 snapshot 保持不变。";
        await this.refreshSelection();
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "附加当前上下文失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async saveKnowledgeFromMessage(message: ConsultationMessage, title: string) {
      const snapshot = this.activeSnapshot;
      if (!this.consultation || !snapshot || message.role !== "assistant" || this.mutating) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
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
        }, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.knowledge = [created, ...this.knowledge.filter((item) => item.id !== created.id)];
        this.notice = `Knowledge 已由 Owner 确认并保存为 revision ${created.current_revision.revision}。`;
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Knowledge 保存失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async setKnowledgeStatus(item: KnowledgeItem, status: "active" | "disabled") {
      if (this.mutating) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        const updated = await updateKnowledgeItem(item.id, { status }, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.knowledge = this.knowledge.map((candidate) => candidate.id === updated.id ? updated : candidate);
        this.notice = status === "active" ? "Knowledge 已启用，可用于后续自动检索。" : "Knowledge 已禁用，不会进入后续自动检索。";
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Knowledge 状态更新失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async reviseKnowledge(item: KnowledgeItem, content: string): Promise<boolean> {
      const normalized = content.trim();
      if (this.mutating || !normalized || normalized === item.current_revision.content.trim()) return false;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        const updated = await updateKnowledgeItem(item.id, { content: normalized }, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return false;
        this.knowledge = this.knowledge.map((candidate) => candidate.id === updated.id ? updated : candidate);
        this.notice = `Knowledge 已保存为不可变 revision ${updated.current_revision.revision}。`;
        return true;
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Knowledge revision 保存失败。 ");
        return false;
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async deleteKnowledge(item: KnowledgeItem): Promise<boolean> {
      if (this.mutating) return false;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        await deleteKnowledgeItem(item.id, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return false;
        this.knowledge = this.knowledge.filter((candidate) => candidate.id !== item.id);
        this.notice = `Knowledge ${item.title} 已删除；其他 Knowledge 未修改。`;
        return true;
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Knowledge 删除失败。 ");
        return false;
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async authorizeCard(card: ActionCard, reason: string) {
      if (this.mutating) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        await authorizeActionCard(card.id, card.content_hash, reason, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.notice = "Owner 已确认 exact action card；本页面没有执行 mutation。";
        await this.refreshSelection();
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Action Card 授权失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },

    async authorizePlan(plan: OperationPlan, reason: string) {
      if (this.mutating) return;
      mutationController?.abort();
      const controller = new AbortController();
      mutationController = controller;
      this.mutating = true;
      this.clearFailure();
      try {
        const updated = await authorizeOperationPlan(plan.id, plan.content_hash, reason, controller.signal);
        if (controller.signal.aborted || mutationController !== controller) return;
        this.operationPlans = this.operationPlans.map((item) => item.id === updated.id ? updated : item);
        this.notice = "Owner 已授权 exact Operation Plan；执行仍属于独立受控阶段。";
        await this.refreshSelection();
      } catch (error) {
        if (!controller.signal.aborted) this.setFailure(error, "Operation Plan 授权失败。 ");
      } finally {
        if (mutationController === controller) {
          mutationController = undefined;
          this.mutating = false;
        }
      }
    },
  },
});
