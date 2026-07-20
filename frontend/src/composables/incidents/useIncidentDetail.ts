import { onBeforeUnmount, reactive, ref } from "vue";

import { isApiError } from "../../api/client";
import {
  closeIncident,
  decideRemediation,
  getIncident,
  getIncidentDelivery,
  getIncidentResolutionReport,
  listIncidentEvidence,
  listIncidentInvestigations,
  listIncidentRemediationPlans,
  listIncidentSignals,
  listIncidentTimeline,
  listIncidentVerifications,
  startInvestigation,
} from "../../api/incidents";
import { isCurrentRequest, loadStateForStatus } from "../../models/incidents";
import type { CommandResponse, IncidentView, LoadState, ResourceView } from "../../types/incidents";

interface Section<T> {
  state: LoadState;
  error: string;
  data: T;
  nextCursor: string;
}

type CollectionLoader = (cursor: string, signal?: AbortSignal) => Promise<{ items: ResourceView[]; next_cursor?: string }>;

export function useIncidentDetail(incidentID: string) {
  const incident = ref<IncidentView | null>(null);
  const pageState = ref<LoadState>("loading");
  const pageError = ref("");
  const commandPending = ref(false);
  const signals = collectionSection();
  const timeline = collectionSection();
  const evidence = collectionSection();
  const investigations = collectionSection();
  const remediationPlans = collectionSection();
  const verifications = collectionSection();
  const delivery = resourceSection();
  const resolutionReport = resourceSection();
  let requestIdentity = 0;
  let controller: AbortController | null = null;

  async function load() {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    pageState.value = "loading";
    pageError.value = "";
    try {
      incident.value = await getIncident(incidentID, controller.signal);
      if (!isCurrentRequest(identity, requestIdentity)) return;
      pageState.value = "ready";
      await Promise.all([
        loadCollection(identity, signals, (cursor, signal) => listIncidentSignals(incidentID, cursor, signal)),
        loadCollection(identity, timeline, (cursor, signal) => listIncidentTimeline(incidentID, cursor, signal)),
        loadCollection(identity, evidence, (cursor, signal) => listIncidentEvidence(incidentID, cursor, signal)),
        loadCollection(identity, investigations, (cursor, signal) => listIncidentInvestigations(incidentID, cursor, signal)),
        loadCollection(identity, remediationPlans, (cursor, signal) => listIncidentRemediationPlans(incidentID, cursor, signal)),
        loadCollection(identity, verifications, (cursor, signal) => listIncidentVerifications(incidentID, cursor, signal)),
        loadResource(identity, delivery, () => getIncidentDelivery(incidentID, controller?.signal)),
        loadResource(identity, resolutionReport, () => getIncidentResolutionReport(incidentID, controller?.signal)),
      ]);
    } catch (cause) {
      if (identity !== requestIdentity || controller.signal.aborted) return;
      pageError.value = cause instanceof Error ? cause.message : "Failed to load incident";
      pageState.value = loadStateForStatus(isApiError(cause) ? cause.status : null);
    }
  }

  async function loadMore(section: Section<ResourceView[]>, loader: CollectionLoader) {
    if (!section.nextCursor || commandPending.value) return;
    const identity = requestIdentity;
    await loadCollection(identity, section, loader, true);
  }

  async function runCommand(request: () => Promise<CommandResponse>): Promise<CommandResponse> {
    commandPending.value = true;
    try {
      const result = await request();
      await load();
      return result;
    } finally {
      commandPending.value = false;
    }
  }

  function investigate(reason: string, csrfToken: string) {
    if (!incident.value) throw new Error("Incident is not loaded");
    return runCommand(() => startInvestigation(incidentID, { expected_version: incident.value!.version, reason: reason || undefined }, csrfToken));
  }

  function close(reason: string, csrfToken: string) {
    if (!incident.value) throw new Error("Incident is not loaded");
    return runCommand(() => closeIncident(incidentID, { expected_version: incident.value!.version, reason: reason || undefined }, csrfToken));
  }

  function decide(plan: ResourceView, decision: "approved" | "rejected", reason: string, csrfToken: string) {
    if (!plan.version || !plan.hash) throw new Error("Plan version and canonical hash are required");
    return runCommand(() => decideRemediation(plan.id, {
      decision,
      expected_version: plan.version!,
      expected_hash: plan.hash!,
      reason: reason || undefined,
    }, csrfToken));
  }

  async function loadCollection(
    identity: number,
    section: Section<ResourceView[]>,
    loader: CollectionLoader,
    append = false,
  ) {
    section.state = "loading";
    section.error = "";
    try {
      const result = await loader(append ? section.nextCursor : "", controller?.signal);
      if (!isCurrentRequest(identity, requestIdentity)) return;
      section.data = append ? [...section.data, ...result.items] : result.items;
      section.nextCursor = result.next_cursor ?? "";
      section.state = section.data.length === 0 ? "empty" : "ready";
    } catch (cause) {
      if (!isCurrentRequest(identity, requestIdentity)) return;
      section.state = loadStateForStatus(isApiError(cause) ? cause.status : null);
      section.error = cause instanceof Error ? cause.message : "Section unavailable";
    }
  }

  async function loadResource(identity: number, section: Section<ResourceView | null>, loader: () => Promise<ResourceView>) {
    section.state = "loading";
    section.error = "";
    try {
      section.data = await loader();
      if (!isCurrentRequest(identity, requestIdentity)) return;
      section.state = "ready";
    } catch (cause) {
      if (!isCurrentRequest(identity, requestIdentity)) return;
      const status = isApiError(cause) ? cause.status : null;
      section.data = null;
      section.state = status === 404 ? "empty" : loadStateForStatus(status);
      section.error = cause instanceof Error ? cause.message : "Section unavailable";
    }
  }

  onBeforeUnmount(() => controller?.abort());

  const moreSignals = () => loadMore(signals, (cursor, signal) => listIncidentSignals(incidentID, cursor, signal));
  const moreTimeline = () => loadMore(timeline, (cursor, signal) => listIncidentTimeline(incidentID, cursor, signal));
  const moreEvidence = () => loadMore(evidence, (cursor, signal) => listIncidentEvidence(incidentID, cursor, signal));
  const moreInvestigations = () => loadMore(investigations, (cursor, signal) => listIncidentInvestigations(incidentID, cursor, signal));
  const moreRemediationPlans = () => loadMore(remediationPlans, (cursor, signal) => listIncidentRemediationPlans(incidentID, cursor, signal));
  const moreVerifications = () => loadMore(verifications, (cursor, signal) => listIncidentVerifications(incidentID, cursor, signal));

  return {
    incident,
    pageState,
    pageError,
    commandPending,
    signals,
    timeline,
    evidence,
    investigations,
    remediationPlans,
    delivery,
    verifications,
    resolutionReport,
    load,
    moreSignals,
    moreTimeline,
    moreEvidence,
    moreInvestigations,
    moreRemediationPlans,
    moreVerifications,
    investigate,
    close,
    decide,
  };
}

function collectionSection(): Section<ResourceView[]> {
  return reactive({ state: "loading", error: "", data: [], nextCursor: "" });
}

function resourceSection(): Section<ResourceView | null> {
  return reactive({ state: "loading", error: "", data: null, nextCursor: "" });
}
