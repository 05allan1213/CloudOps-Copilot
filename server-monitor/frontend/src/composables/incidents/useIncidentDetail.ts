import { onBeforeUnmount, reactive, ref } from "vue";

import { isApiError } from "../../api/client";
import {
  getIncident,
  getIncidentDelivery,
  getIncidentInvestigation,
  getIncidentResources,
  getIncidentPostmortem,
  getIncidentRemediation,
  getIncidentVerification,
  listIncidentEvidence,
  listIncidentSignals,
  listIncidentTimeline,
  listIncidentVerifications,
} from "../../api/incidents";
import { isCurrentRequest, loadStateForStatus, postmortemStateForStatus, sortTimeline } from "../../models/incidents";
import type {
  DeliveryDTO,
  IncidentDTO,
  IncidentEvidenceDTO,
  IncidentSignalDTO,
  IncidentTimelineDTO,
  IncidentResourcesDTO,
  InvestigationDTO,
  LoadState,
  PostmortemDTO,
  RemediationDTO,
  VerificationDetailDTO,
  VerificationRunDTO,
} from "../../types/incidents";

interface Section<T> {
  state: LoadState;
  error: string;
  data: T;
}

export function useIncidentDetail(incidentID: string, canViewApproval: boolean) {
  const incident = ref<IncidentDTO | null>(null);
  const pageState = ref<LoadState>("loading");
  const pageError = ref("");
  const signals = reactive<Section<IncidentSignalDTO[]>>({ state: "loading", error: "", data: [] });
  const timeline = reactive<Section<IncidentTimelineDTO[]>>({ state: "loading", error: "", data: [] });
  const timelineTotal = ref(0);
  const timelinePage = ref(1);
  const evidence = reactive<Section<IncidentEvidenceDTO[]>>({ state: "loading", error: "", data: [] });
  const investigation = reactive<Section<InvestigationDTO | null>>({ state: "loading", error: "", data: null });
  const remediation = reactive<Section<RemediationDTO | null>>({ state: canViewApproval ? "loading" : "forbidden", error: "", data: null });
  const delivery = reactive<Section<DeliveryDTO | null>>({ state: "loading", error: "", data: null });
  const verificationRuns = reactive<Section<VerificationRunDTO[]>>({ state: "loading", error: "", data: [] });
  const verification = reactive<Section<VerificationDetailDTO | null>>({ state: "loading", error: "", data: null });
  const postmortem = reactive<Section<PostmortemDTO | null>>({ state: "loading", error: "", data: null });
  const resources = reactive<Section<IncidentResourcesDTO>>({ state: "loading", error: "", data: { cluster: "", namespace: "", deployments: [], pods: [], services: [], events: [] } });
  let requestIdentity = 0;
  let controller: AbortController | null = null;

  async function load() {
    const identity = ++requestIdentity;
    controller?.abort();
    controller = new AbortController();
    pageState.value = "loading";
    pageError.value = "";
    try {
      const item = await getIncident(incidentID, controller.signal);
      if (!isCurrentRequest(identity, requestIdentity)) return;
      incident.value = item;
      pageState.value = "ready";
      await Promise.all([
        loadPageSection(identity, signals, () => listIncidentSignals(incidentID, 1, 50, controller?.signal), (result) => result.items),
        loadTimeline(identity, 1, false),
        loadPageSection(identity, evidence, () => listIncidentEvidence(incidentID, 1, 50, controller?.signal), (result) => result.items),
        loadSimpleSection(identity, investigation, () => getIncidentInvestigation(incidentID, controller?.signal)),
        canViewApproval ? loadSimpleSection(identity, remediation, () => getIncidentRemediation(incidentID, controller?.signal)) : Promise.resolve(),
        loadSimpleSection(identity, delivery, () => getIncidentDelivery(incidentID, controller?.signal)),
        loadVerificationSections(identity),
        loadPostmortem(identity),
        loadResources(identity),
      ]);
    } catch (cause) {
      if (identity !== requestIdentity || controller.signal.aborted) return;
      pageError.value = cause instanceof Error ? cause.message : "Failed to load incident";
      pageState.value = loadStateForStatus(isApiError(cause) ? cause.status : null);
    }
  }

  async function loadVerificationSections(identity: number) {
    await loadPageSection(identity, verificationRuns, () => listIncidentVerifications(incidentID, 1, 20, controller?.signal), (result) => result.items);
    if (identity !== requestIdentity) return;
    const latest = verificationRuns.data[0];
    if (!latest) {
      verification.state = verificationRuns.state === "unavailable" ? "unavailable" : "empty";
      verification.data = null;
      return;
    }
    await loadSimpleSection(identity, verification, () => getIncidentVerification(incidentID, latest.id, controller?.signal));
  }

  async function loadPostmortem(identity: number) {
    postmortem.state = "loading";
    try {
      const result = await getIncidentPostmortem(incidentID, controller?.signal);
      if (identity !== requestIdentity) return;
      postmortem.data = result;
      postmortem.state = "ready";
    } catch (cause) {
      if (identity !== requestIdentity) return;
      const status = isApiError(cause) ? cause.status : null;
      postmortem.data = null;
      postmortem.state = postmortemStateForStatus(status);
      postmortem.error = cause instanceof Error ? cause.message : "Failed to load postmortem";
    }
  }

  async function loadTimeline(identity = requestIdentity, page = timelinePage.value + 1, append = true) {
    timeline.state = "loading";
    timeline.error = "";
    try {
      const result = await listIncidentTimeline(incidentID, page, 50, controller?.signal);
      if (!isCurrentRequest(identity, requestIdentity)) return;
      timelinePage.value = page;
      timelineTotal.value = result.total;
      const combined = append ? [...timeline.data, ...result.items] : result.items;
      timeline.data = sortTimeline(Array.from(new Map(combined.map((item) => [item.key, item])).values()));
      timeline.state = timeline.data.length === 0 ? "empty" : "ready";
    } catch (cause) {
      if (!isCurrentRequest(identity, requestIdentity)) return;
      timeline.state = loadStateForStatus(isApiError(cause) ? cause.status : null);
      timeline.error = cause instanceof Error ? cause.message : "Timeline unavailable";
    }
  }

  function loadMoreTimeline() {
    return loadTimeline(requestIdentity, timelinePage.value + 1, true);
  }

  async function loadResources(identity: number) {
    resources.state = "loading";
    resources.error = "";
    try {
      const result = await getIncidentResources(incidentID, controller?.signal);
      if (identity !== requestIdentity) return;
      resources.data = result;
      resources.state = [result.deployments, result.pods, result.services, result.events].every((items) => items.length === 0) ? "empty" : "ready";
    } catch (cause) {
      if (identity !== requestIdentity) return;
      resources.state = loadStateForStatus(isApiError(cause) ? cause.status : null, "unavailable");
      resources.error = cause instanceof Error ? cause.message : "Kubernetes context unavailable";
    }
  }

  async function loadSimpleSection<T>(identity: number, section: Section<T | null>, request: () => Promise<T>) {
    section.state = "loading";
    section.error = "";
    try {
      const result = await request();
      if (identity !== requestIdentity) return;
      section.data = result;
      section.state = "ready";
    } catch (cause) {
      if (identity !== requestIdentity) return;
      const status = isApiError(cause) ? cause.status : null;
      section.data = null;
      section.state = status === 404 ? "empty" : loadStateForStatus(status);
      section.error = cause instanceof Error ? cause.message : "Section unavailable";
    }
  }

  async function loadPageSection<T, R>(identity: number, section: Section<T[]>, request: () => Promise<R>, select: (result: R) => T[]) {
    section.state = "loading";
    section.error = "";
    try {
      const result = select(await request());
      if (identity !== requestIdentity) return;
      section.data = result;
      section.state = section.data.length === 0 ? "empty" : "ready";
    } catch (cause) {
      if (identity !== requestIdentity) return;
      const status = isApiError(cause) ? cause.status : null;
      section.data = [];
      section.state = loadStateForStatus(status);
      section.error = cause instanceof Error ? cause.message : "Section unavailable";
    }
  }

  onBeforeUnmount(() => controller?.abort());
  return { incident, pageState, pageError, signals, timeline, timelineTotal, evidence, investigation, remediation, delivery, verificationRuns, verification, postmortem, resources, load, loadMoreTimeline };
}
