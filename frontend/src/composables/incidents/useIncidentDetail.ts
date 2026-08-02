import { onBeforeUnmount, reactive, ref } from "vue";

import { isApiError } from "../../api/client";
import {
  closeIncident,
  decideRemediation as submitRemediationDecision,
  decideIncidentRecovery,
  getIncidentDelivery,
  getIncident,
  getIncidentDecision,
  getIncidentResolutionReport,
  listIncidentAlerts,
  listIncidentEvidence,
  listIncidentInvestigations,
  listIncidentRemediationPlans,
  listIncidentSignals,
  listIncidentTimeline,
  listIncidentVerifications,
  newCommandKey,
  startInvestigation,
} from "../../api/incidents";
import { invalidateQueryDomain } from "../queryCache";
import { commandFeedbackForFailure, retainCommandAttempt, type CommandAttemptIdentity, type CommandFeedback } from "../../models/commands";
import { isCurrentRequest, loadStateForStatus } from "../../models/incidents";
import type {
  CommandOutcome,
  DeliveryView,
  IncidentAlertRelationView,
  IncidentDecisionView,
  IncidentEvidenceView,
  IncidentInvestigationView,
  IncidentRealtimeEvent,
  IncidentTimelineEventView,
  IncidentView,
  LoadState,
  RemediationPlanView,
  ResolutionReportView,
  ResourceView,
  VerificationRunView,
} from "../../types/incidents";

export interface SectionError {
  message: string;
  status: number | null;
  code: string;
  httpStatus: number;
  requestID: string;
  traceID: string;
}

export interface Section<T> {
  state: LoadState;
  error: SectionError | null;
  data: T;
  nextCursor: string;
  refreshing: boolean;
  loadingMore: boolean;
  lastUpdatedAt: string;
}

export type { CommandFeedback, CommandFeedbackState } from "../../models/commands";

type Identified = { id: string };
type CollectionLoader<T extends Identified> = (cursor: string, signal?: AbortSignal) => Promise<{ items: T[]; next_cursor?: string }>;
type CollectionLoadMode = "replace" | "prepend" | "append";

interface CollectionLoadOptions {
  mode: CollectionLoadMode;
  preserve: boolean;
  signal: AbortSignal;
  cursor?: string;
  loadingMore?: boolean;
}

interface RetryableCommand {
  identity: CommandAttemptIdentity;
  request: (idempotencyKey: string) => Promise<CommandOutcome>;
}

export function useIncidentDetail(incidentID: string) {
  const incident = ref<IncidentView | null>(null);
  const pageState = ref<LoadState>("loading");
  const pageError = ref<SectionError | null>(null);
  const commandPending = ref(false);
  const commandFeedback = ref<CommandFeedback | null>(null);
  const canRetryCommand = ref(false);
  const refreshing = ref(false);
  const lastUpdatedAt = ref("");
  const alerts = collectionSection<IncidentAlertRelationView>();
  const signals = collectionSection<ResourceView>();
  const timeline = collectionSection<IncidentTimelineEventView>();
  const evidence = collectionSection<IncidentEvidenceView>();
  const investigations = collectionSection<IncidentInvestigationView>();
  const decision = resourceSection<IncidentDecisionView>();
  const remediationPlans = collectionSection<RemediationPlanView>();
  const delivery = resourceSection<DeliveryView>();
  const verifications = collectionSection<VerificationRunView>();
  const resolutionReport = resourceSection<ResolutionReportView>();
  const backgroundControllers = new Set<AbortController>();
  const sectionRequestIdentities = new WeakMap<object, number>();
  let requestIdentity = 0;
  let controller: AbortController | null = null;
  let retryableCommand: RetryableCommand | null = null;

  async function load(options: { preserve?: boolean } = {}) {
    const preserve = options.preserve ?? incident.value !== null;
    if (preserve) invalidateQueryDomain("incidents");
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    const signal = controller.signal;
    refreshing.value = preserve;
    pageError.value = null;
    if (!preserve) pageState.value = "loading";

    try {
      const nextIncident = await getIncident(incidentID, signal);
      if (!isCurrentRequest(identity, requestIdentity)) return;
      incident.value = nextIncident;
      pageState.value = "ready";
      await Promise.all([
        loadCollection(identity, alerts, (cursor, requestSignal) => listIncidentAlerts(incidentID, cursor, requestSignal), refreshOptions(alerts, preserve, signal)),
        loadCollection(identity, signals, (cursor, requestSignal) => listIncidentSignals(incidentID, cursor, requestSignal), refreshOptions(signals, preserve, signal)),
        loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(preserve, signal)),
        loadCollection(identity, evidence, (cursor, requestSignal) => listIncidentEvidence(incidentID, cursor, requestSignal), refreshOptions(evidence, preserve, signal)),
        loadCollection(identity, investigations, (cursor, requestSignal) => listIncidentInvestigations(incidentID, cursor, requestSignal), refreshOptions(investigations, preserve, signal)),
        loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), preserve, signal),
        loadCollection(identity, remediationPlans, (cursor, requestSignal) => listIncidentRemediationPlans(incidentID, cursor, requestSignal), refreshOptions(remediationPlans, preserve, signal)),
        loadResource(identity, delivery, (requestSignal) => getIncidentDelivery(incidentID, requestSignal), preserve, signal),
        loadCollection(identity, verifications, (cursor, requestSignal) => listIncidentVerifications(incidentID, cursor, requestSignal), refreshOptions(verifications, preserve, signal)),
        loadResource(identity, resolutionReport, (requestSignal) => getIncidentResolutionReport(incidentID, requestSignal), preserve, signal),
      ]);
      if (isCurrentRequest(identity, requestIdentity)) lastUpdatedAt.value = new Date().toISOString();
    } catch (cause) {
      if (identity !== requestIdentity || signal.aborted) return;
      pageError.value = normalizeSectionError(cause, "Incident 读取失败");
      if (!preserve || !incident.value) pageState.value = loadStateForStatus(pageError.value.status);
    } finally {
      if (identity === requestIdentity) refreshing.value = false;
    }
  }

  async function refreshResource(resource: IncidentRealtimeEvent["resource"]) {
    invalidateQueryDomain("incidents");
    if (!incident.value) {
      await load();
      return;
    }
    await withBackgroundController(async (signal) => {
      const identity = requestIdentity;
      let updates: boolean[] = [];
      switch (resource) {
        case "incident":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadCollection(identity, alerts, (cursor, requestSignal) => listIncidentAlerts(incidentID, cursor, requestSignal), refreshOptions(alerts, true, signal)),
            loadCollection(identity, signals, (cursor, requestSignal) => listIncidentSignals(incidentID, cursor, requestSignal), refreshOptions(signals, true, signal)),
            loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(true, signal)),
            loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), true, signal),
            loadCollection(identity, remediationPlans, (cursor, requestSignal) => listIncidentRemediationPlans(incidentID, cursor, requestSignal), refreshOptions(remediationPlans, true, signal)),
            loadResource(identity, delivery, (requestSignal) => getIncidentDelivery(incidentID, requestSignal), true, signal),
            loadCollection(identity, verifications, (cursor, requestSignal) => listIncidentVerifications(incidentID, cursor, requestSignal), refreshOptions(verifications, true, signal)),
            loadResource(identity, resolutionReport, (requestSignal) => getIncidentResolutionReport(incidentID, requestSignal), true, signal),
          ]);
          break;
        case "signals":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadCollection(identity, signals, (cursor, requestSignal) => listIncidentSignals(incidentID, cursor, requestSignal), refreshOptions(signals, true, signal)),
            loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(true, signal)),
          ]);
          break;
        case "timeline":
          updates = [await loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(true, signal))];
          break;
        case "evidence":
          updates = [await loadCollection(identity, evidence, (cursor, requestSignal) => listIncidentEvidence(incidentID, cursor, requestSignal), refreshOptions(evidence, true, signal))];
          break;
        case "investigations":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadCollection(identity, investigations, (cursor, requestSignal) => listIncidentInvestigations(incidentID, cursor, requestSignal), refreshOptions(investigations, true, signal)),
            loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), true, signal),
          ]);
          break;
        case "remediation_plans":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), true, signal),
            loadCollection(identity, remediationPlans, (cursor, requestSignal) => listIncidentRemediationPlans(incidentID, cursor, requestSignal), refreshOptions(remediationPlans, true, signal)),
          ]);
          break;
        case "delivery":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), true, signal),
            loadResource(identity, delivery, (requestSignal) => getIncidentDelivery(incidentID, requestSignal), true, signal),
          ]);
          break;
        case "verifications":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(true, signal)),
            loadCollection(identity, verifications, (cursor, requestSignal) => listIncidentVerifications(incidentID, cursor, requestSignal), refreshOptions(verifications, true, signal)),
            loadResource(identity, decision, (requestSignal) => getIncidentDecision(incidentID, requestSignal), true, signal),
            loadResource(identity, delivery, (requestSignal) => getIncidentDelivery(incidentID, requestSignal), true, signal),
            loadResource(identity, resolutionReport, (requestSignal) => getIncidentResolutionReport(incidentID, requestSignal), true, signal),
          ]);
          break;
        case "resolution_report":
          updates = await Promise.all([
            refreshIncident(identity, signal),
            loadCollection(identity, timeline, (cursor, requestSignal) => listIncidentTimeline(incidentID, cursor, requestSignal), timelineRefreshOptions(true, signal)),
            loadCollection(identity, verifications, (cursor, requestSignal) => listIncidentVerifications(incidentID, cursor, requestSignal), refreshOptions(verifications, true, signal)),
            loadResource(identity, resolutionReport, (requestSignal) => getIncidentResolutionReport(incidentID, requestSignal), true, signal),
          ]);
          break;
      }
      if (updates.some(Boolean) && !signal.aborted && identity === requestIdentity) {
        lastUpdatedAt.value = new Date().toISOString();
        return;
      }
      if (!signal.aborted && identity === requestIdentity) throw new Error(`${resource} projection refresh failed`);
    });
  }

  async function refreshIncident(identity: number, signal: AbortSignal): Promise<boolean> {
    try {
      const nextIncident = await getIncident(incidentID, signal);
      if (identity !== requestIdentity || signal.aborted) return false;
      incident.value = nextIncident;
      pageError.value = null;
      return true;
    } catch (cause) {
      if (identity === requestIdentity && !signal.aborted) pageError.value = normalizeSectionError(cause, "Incident 刷新失败");
      return false;
    }
  }

  async function loadMore<T extends Identified>(section: Section<T[]>, loader: CollectionLoader<T>) {
    if (!section.nextCursor || section.loadingMore || commandPending.value) return;
    await withBackgroundController((signal) => loadCollection(requestIdentity, section, loader, {
      mode: "append",
      preserve: true,
      signal,
      cursor: section.nextCursor,
      loadingMore: true,
    }));
  }

  async function runCommand(
    action: string,
    resourceID: string,
    request: (idempotencyKey: string) => Promise<CommandOutcome>,
    idempotencyKey = newCommandKey(action.toLowerCase().replace(/\s+/g, "-"), resourceID),
  ): Promise<CommandOutcome> {
    if (commandPending.value) throw new Error("已有命令正在提交");
    commandPending.value = true;
    commandFeedback.value = {
      state: "submitting",
      action,
      resourceID,
      message: "正在提交精确版本命令…",
      code: "",
      httpStatus: 0,
      requestID: "",
      traceID: "",
      idempotencyKey,
      idempotentReplay: false,
      retryable: false,
    };
    const attempt: RetryableCommand = { identity: retainCommandAttempt({ action, resourceID, idempotencyKey }), request };
    try {
      const outcome = await request(idempotencyKey);
      commandFeedback.value = {
        state: "accepted",
        action,
        resourceID,
        message: outcome.idempotentReplay ? "服务端返回了先前已接受命令的幂等结果。" : "命令已持久化并进入异步执行。",
        code: outcome.result.status,
        httpStatus: outcome.httpStatus,
        requestID: outcome.requestID,
        traceID: outcome.traceID,
        idempotencyKey,
        idempotentReplay: outcome.idempotentReplay,
        retryable: false,
      };
      retryableCommand = null;
      canRetryCommand.value = false;
      await load({ preserve: true });
      return outcome;
    } catch (cause) {
      const feedback = normalizeCommandFeedback(cause, action, resourceID, idempotencyKey);
      commandFeedback.value = feedback;
      retryableCommand = feedback.retryable ? attempt : null;
      canRetryCommand.value = feedback.retryable;
      throw cause;
    } finally {
      commandPending.value = false;
    }
  }

  function retryLastCommand(): Promise<CommandOutcome | null> {
    const attempt = retryableCommand;
    if (!attempt || commandPending.value) return Promise.resolve(null);
    return runCommand(attempt.identity.action, attempt.identity.resourceID, attempt.request, attempt.identity.idempotencyKey);
  }

  function clearCommandFeedback() {
    commandFeedback.value = null;
    retryableCommand = null;
    canRetryCommand.value = false;
  }

  function investigate(reason: string) {
    if (!incident.value) throw new Error("Incident 尚未加载");
    const body = { expected_version: incident.value.version, reason: reason || undefined };
    return runCommand("Start Investigation", incidentID, (idempotencyKey) => startInvestigation(incidentID, body, { idempotencyKey }));
  }

  function close(reason: string) {
    if (!incident.value) throw new Error("Incident 尚未加载");
    if (!incident.value.recovery.can_close) throw new Error("当前恢复证明尚不允许关闭 Incident");
    const body = { expected_version: incident.value.version, reason: reason || undefined };
    return runCommand("Close Incident", incidentID, (idempotencyKey) => closeIncident(incidentID, body, { idempotencyKey }));
  }

  function decideRecovery(reason: string) {
    if (!incident.value) throw new Error("Incident 尚未加载");
    if (incident.value.status !== "investigating") throw new Error("只有 Investigate 状态可以进入恢复验证");
    const body = {
      decision: "verify_recovery" as const,
      expected_version: incident.value.version,
      reason,
    };
    return runCommand(
      "Verify Recovery",
      incidentID,
      (idempotencyKey) => decideIncidentRecovery(incidentID, body, { idempotencyKey }),
    );
  }

  function decideRemediation(plan: RemediationPlanView, decisionValue: "approved" | "rejected", reason: string) {
    if (!incident.value) throw new Error("Incident 尚未加载");
    const body = {
      decision: decisionValue,
      expected_version: plan.version,
      expected_hash: plan.canonical_plan_hash,
      reason,
    };
    return runCommand(
      decisionValue === "approved" ? "Approve Remediation Plan" : "Reject Remediation Plan",
      plan.id,
      (idempotencyKey) => submitRemediationDecision(plan.id, body, { idempotencyKey }),
    );
  }

  async function loadCollection<T extends Identified>(
    identity: number,
    section: Section<T[]>,
    loader: CollectionLoader<T>,
    options: CollectionLoadOptions,
  ): Promise<boolean> {
    const sectionIdentity = nextSectionRequest(section);
    const hadData = section.data.length > 0;
    const previousCursor = section.nextCursor;
    if (options.loadingMore) section.loadingMore = true;
    else if (options.preserve && hadData) section.refreshing = true;
    else section.state = "loading";
    section.error = null;

    try {
      const result = await loader(options.cursor ?? "", options.signal);
      if (!isCurrentSectionRequest(identity, section, sectionIdentity, options.signal)) return false;
      if (options.mode === "append") section.data = mergeAppend(section.data, result.items);
      else if (options.mode === "prepend") section.data = mergePrepend(section.data, result.items);
      else section.data = result.items;
      section.nextCursor = options.mode === "prepend" && hadData ? previousCursor : result.next_cursor ?? "";
      section.state = section.data.length === 0 ? "empty" : "ready";
      section.lastUpdatedAt = new Date().toISOString();
      return true;
    } catch (cause) {
      if (!isCurrentSectionRequest(identity, section, sectionIdentity, options.signal)) return false;
      section.error = normalizeSectionError(cause, "投影读取失败");
      if (!(options.preserve && section.data.length > 0)) section.state = loadStateForStatus(section.error.status);
      return false;
    } finally {
      if (isCurrentSectionRequest(identity, section, sectionIdentity, options.signal, true)) {
        section.loadingMore = false;
        section.refreshing = false;
      }
    }
  }

  async function loadResource<T>(
    identity: number,
    section: Section<T | null>,
    loader: (signal: AbortSignal) => Promise<T | null>,
    preserve: boolean,
    signal: AbortSignal,
  ): Promise<boolean> {
    const sectionIdentity = nextSectionRequest(section);
    if (preserve && section.data) section.refreshing = true;
    else section.state = "loading";
    section.error = null;

    try {
      const result = await loader(signal);
      if (!isCurrentSectionRequest(identity, section, sectionIdentity, signal)) return false;
      section.data = result;
      section.state = result === null ? "empty" : "ready";
      section.lastUpdatedAt = new Date().toISOString();
      return true;
    } catch (cause) {
      if (!isCurrentSectionRequest(identity, section, sectionIdentity, signal)) return false;
      section.error = normalizeSectionError(cause, "投影读取失败");
      if (!(preserve && section.data)) {
        section.data = null;
        section.state = section.error.status === 404 ? "empty" : loadStateForStatus(section.error.status);
      }
      return false;
    } finally {
      if (isCurrentSectionRequest(identity, section, sectionIdentity, signal, true)) section.refreshing = false;
    }
  }

  function refreshOptions<T extends Identified>(section: Section<T[]>, preserve: boolean, signal: AbortSignal): CollectionLoadOptions {
    return { mode: preserve && section.data.length > 0 ? "prepend" : "replace", preserve, signal };
  }

  function timelineRefreshOptions(preserve: boolean, signal: AbortSignal): CollectionLoadOptions {
    const tail = preserve ? timeline.data[timeline.data.length - 1]?.id ?? "" : "";
    return { mode: tail ? "append" : "replace", preserve, signal, cursor: tail };
  }

  function nextSectionRequest(section: object): number {
    const next = (sectionRequestIdentities.get(section) ?? 0) + 1;
    sectionRequestIdentities.set(section, next);
    return next;
  }

  function isCurrentSectionRequest(
    identity: number,
    section: object,
    sectionIdentity: number,
    signal: AbortSignal,
    allowAborted = false,
  ): boolean {
    return isCurrentRequest(identity, requestIdentity)
      && sectionRequestIdentities.get(section) === sectionIdentity
      && (allowAborted || !signal.aborted);
  }

  async function withBackgroundController<T>(task: (signal: AbortSignal) => Promise<T>): Promise<T> {
    const background = new AbortController();
    backgroundControllers.add(background);
    try {
      return await task(background.signal);
    } finally {
      backgroundControllers.delete(background);
    }
  }

  onBeforeUnmount(() => {
    controller?.abort();
    for (const background of backgroundControllers) background.abort();
    backgroundControllers.clear();
  });

  return {
    incident,
    pageState,
    pageError,
    commandPending,
    commandFeedback,
    canRetryCommand,
    refreshing,
    lastUpdatedAt,
    alerts,
    signals,
    timeline,
    evidence,
    investigations,
    decision,
    remediationPlans,
    delivery,
    verifications,
    resolutionReport,
    load,
    refreshResource,
    moreAlerts: () => loadMore(alerts, (cursor, signal) => listIncidentAlerts(incidentID, cursor, signal)),
    moreSignals: () => loadMore(signals, (cursor, signal) => listIncidentSignals(incidentID, cursor, signal)),
    moreTimeline: () => loadMore(timeline, (cursor, signal) => listIncidentTimeline(incidentID, cursor, signal)),
    moreEvidence: () => loadMore(evidence, (cursor, signal) => listIncidentEvidence(incidentID, cursor, signal)),
    moreInvestigations: () => loadMore(investigations, (cursor, signal) => listIncidentInvestigations(incidentID, cursor, signal)),
    moreRemediationPlans: () => loadMore(remediationPlans, (cursor, signal) => listIncidentRemediationPlans(incidentID, cursor, signal)),
    moreVerifications: () => loadMore(verifications, (cursor, signal) => listIncidentVerifications(incidentID, cursor, signal)),
    investigate,
    decideRecovery,
    decideRemediation,
    close,
    retryLastCommand,
    clearCommandFeedback,
  };
}

function collectionSection<T extends Identified>(): Section<T[]> {
  return reactive({
    state: "loading",
    error: null,
    data: [],
    nextCursor: "",
    refreshing: false,
    loadingMore: false,
    lastUpdatedAt: "",
  }) as Section<T[]>;
}

function resourceSection<T>(): Section<T | null> {
  return reactive({
    state: "loading",
    error: null,
    data: null,
    nextCursor: "",
    refreshing: false,
    loadingMore: false,
    lastUpdatedAt: "",
  }) as Section<T | null>;
}

function mergeAppend<T extends Identified>(current: T[], incoming: T[]): T[] {
  const merged = new Map(current.map((item) => [item.id, item]));
  for (const item of incoming) merged.set(item.id, item);
  return [...merged.values()];
}

function mergePrepend<T extends Identified>(current: T[], incoming: T[]): T[] {
  const merged = new Map(incoming.map((item) => [item.id, item]));
  for (const item of current) if (!merged.has(item.id)) merged.set(item.id, item);
  return [...merged.values()];
}

function normalizeSectionError(cause: unknown, fallback: string): SectionError {
  const apiError = isApiError(cause) ? cause : null;
  return {
    message: cause instanceof Error ? cause.message : fallback,
    status: apiError?.status ?? null,
    code: apiError?.code ?? "",
    httpStatus: apiError?.status ?? 0,
    requestID: apiError?.requestID ?? "",
    traceID: apiError?.traceID ?? "",
  };
}

function normalizeCommandFeedback(cause: unknown, action: string, resourceID: string, idempotencyKey: string): CommandFeedback {
  const apiError = isApiError(cause) ? cause : null;
  return commandFeedbackForFailure({
    status: apiError?.status ?? null,
    message: cause instanceof Error ? cause.message : "命令未完成。",
    code: apiError?.code,
    requestID: apiError?.requestID,
    traceID: apiError?.traceID,
    idempotentReplay: apiError?.idempotentReplay,
  }, action, resourceID, idempotencyKey);
}
