import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  acknowledgeAlert,
  alertContextRouteLinks,
  alertInspectorHistory,
  alertListRouteQuery,
  attachAlertToIncident,
  canonicalAlertResourceQuery,
  createAlertSilence,
  createIncidentFromAlert,
  expireAlertSilence,
  isAlertPublicID,
  parseAlertListRouteQuery,
  reconcileAlertProbe,
  startAlertInvestigation,
  type AlertView,
} from "./alerts";
import { getJSON, postJSONWithMeta } from "./client";

vi.mock("./client", async (importOriginal) => ({
  ...await importOriginal<typeof import("./client")>(),
  getJSON: vi.fn(),
  postJSONWithMeta: vi.fn(),
}));

const alertID = "11111111-1111-4111-8111-111111111111";
const incidentID = "22222222-2222-4222-8222-222222222222";
const silenceID = "33333333-3333-4333-8333-333333333333";

function alertFixture(overrides: Partial<AlertView> = {}): AlertView {
  return {
    id: alertID,
    status: "firing",
    severity: "critical",
    summary: "API error rate above threshold",
    category: "availability",
    source: "alertmanager",
    fingerprint: "fingerprint-1",
    correlation_key: "a".repeat(64),
    cluster: "cloudops-local",
    environment: "local",
    namespace: "demo",
    service_name: "cloudops-api",
    target_kind: "Deployment",
    target_name: "cloudops-api",
    first_seen_at: "2026-07-31T00:00:00Z",
    last_seen_at: "2026-07-31T00:05:00Z",
    starts_at: "2026-07-31T00:00:00Z",
    recurrence_count: 1,
    signal_count: 2,
    version: 3,
    incident_links: [],
    investigations: [],
    context_link: {
      workspace: "alerts",
      path: `/alerts/${alertID}`,
      query: { cluster_id: "cloudops-local", namespace: "demo" },
      operational_scope_id: "44444444-4444-4444-8444-444444444444",
      external: false,
    },
    migrated_legacy: false,
    migrated_legacy_context: false,
    created_at: "2026-07-31T00:00:00Z",
    updated_at: "2026-07-31T00:05:00Z",
    ...overrides,
  };
}

describe("Alert route query contract", () => {
  it("normalizes legacy workload input and emits canonical resource state", () => {
    expect(canonicalAlertResourceQuery({
      workload: "cloudops-api",
      namespace: "demo",
    })).toEqual({
      resource: "cloudops-api",
      namespace: "demo",
    });
    expect(canonicalAlertResourceQuery({
      workload: "legacy-api",
      resource: "canonical-api",
    })).toEqual({ resource: "canonical-api" });
  });

  it("round-trips filters, cursor, limit, and Inspector selection", () => {
    const state = parseAlertListRouteQuery({
      status: "firing",
      severity: "critical",
      namespace: "demo",
      search: "latency",
      incident: incidentID,
      cursor: "cursor-2",
      limit: "100",
      selected: alertID,
      workload: "cloudops-api",
    });

    expect(state).toEqual({
      status: "firing",
      severity: "critical",
      namespace: "demo",
      search: "latency",
      incident: incidentID,
      cursor: "cursor-2",
      limit: 100,
      selected: alertID,
    });
    expect(alertListRouteQuery(state, { workload: "cloudops-api", stable: "kept" })).toEqual({
      resource: "cloudops-api",
      stable: "kept",
      status: "firing",
      severity: "critical",
      namespace: "demo",
      search: "latency",
      incident: incidentID,
      cursor: "cursor-2",
      limit: "100",
      selected: alertID,
    });
  });

  it("keeps invalid Inspector IDs explicit while rejecting them locally", () => {
    expect(parseAlertListRouteQuery({ selected: "not-a-public-id" }).selected).toBe("not-a-public-id");
    expect(isAlertPublicID("not-a-public-id")).toBe(false);
    expect(isAlertPublicID(alertID)).toBe(true);
  });

  it("builds only canonical resource context links for every downstream workspace", () => {
    const links = alertContextRouteLinks(alertFixture(), "2026-07-31T01:00:00Z");

    expect(links.map((link) => link.path)).toEqual([
      "/infrastructure",
      "/monitoring",
      "/logs",
      "/traces",
    ]);
    expect(links.every((link) => "resource" in link.query && !("workload" in link.query))).toBe(true);
    expect(links[0]?.query).toMatchObject({
      cluster: "cloudops-local",
      namespace: "demo",
      from: "2026-07-31T00:00:00Z",
      to: "2026-07-31T01:00:00Z",
      resource: "Deployment/demo/cloudops-api",
    });
    expect(links[2]?.query.resource).toBe("cloudops-api");
  });

  it("keeps backend event order and bounds the Inspector history preview", () => {
    const events = Array.from({ length: 10 }, (_, index) => ({
      id: `event-${index}`,
      type: `alert.event.${index}`,
      actor_type: "user",
      actor_id: "owner",
      summary: `event ${index}`,
      metadata: {},
      occurred_at: `2026-07-31T00:${String(index).padStart(2, "0")}:00Z`,
    }));

    expect(alertInspectorHistory(events).map((event) => event.id)).toEqual([
      "event-0",
      "event-1",
      "event-2",
      "event-3",
      "event-4",
      "event-5",
      "event-6",
      "event-7",
    ]);
    expect(alertInspectorHistory(events, 0)).toEqual([]);
  });
});

describe("Alert command contract", () => {
  const postMock = vi.mocked(postJSONWithMeta);
  const getMock = vi.mocked(getJSON);

  beforeEach(() => {
    postMock.mockReset();
    getMock.mockReset();
  });

  it("preserves expected version, a caller-owned idempotency key, and response identity", async () => {
    const alert = alertFixture();
    postMock.mockResolvedValue({
      data: alert,
      status: 200,
      requestID: "request-alert-1",
      traceID: "trace-alert-1",
      idempotentReplay: true,
    });

    const result = await acknowledgeAlert(alertID, 3, "Owner triage", {
      idempotencyKey: "alert-command-retry-1",
    });

    expect(postMock).toHaveBeenCalledWith(
      `/api/v1/alerts/${alertID}/acknowledgements`,
      { expected_version: 3, reason: "Owner triage" },
      { headers: { "Content-Type": "application/json", "Idempotency-Key": "alert-command-retry-1" } },
    );
    expect(result).toMatchObject({
      data: alert,
      httpStatus: 200,
      requestID: "request-alert-1",
      traceID: "trace-alert-1",
      idempotentReplay: true,
      idempotencyKey: "alert-command-retry-1",
      expectedVersion: 3,
      operation: "acknowledge",
    });
  });

  it("uses the exact backend status contract for every Alert command family", async () => {
    const alert = alertFixture();
    const silence = {
      id: silenceID,
      status: "active" as const,
      matchers: [],
      reason: "bounded triage",
      configuration_revision_id: "revision-1",
      starts_at: "2026-07-31T00:00:00Z",
      ends_at: "2026-07-31T00:30:00Z",
      created_at: "2026-07-31T00:00:00Z",
    };
    const cases = [
      {
        status: 201,
        data: silence,
        invoke: () => createAlertSilence(alertID, 3, 1800, "bounded triage", { idempotencyKey: "key-silence" }),
        path: `/api/v1/alerts/${alertID}/silences`,
      },
      {
        status: 200,
        data: silence,
        invoke: () => expireAlertSilence(silenceID, 3, { idempotencyKey: "key-expire" }),
        path: `/api/v1/silences/${silenceID}/expire`,
      },
      {
        status: 201,
        data: alert,
        invoke: () => createIncidentFromAlert(alertID, 3, { idempotencyKey: "key-create-incident" }),
        path: `/api/v1/alerts/${alertID}/incident-links`,
      },
      {
        status: 201,
        data: alert,
        invoke: () => attachAlertToIncident(alertID, incidentID, 3, { idempotencyKey: "key-attach-incident" }),
        path: `/api/v1/alerts/${alertID}/incident-links`,
      },
      {
        status: 202,
        data: alert,
        invoke: () => startAlertInvestigation(alertID, 3, "investigate", { idempotencyKey: "key-investigate" }),
        path: `/api/v1/alerts/${alertID}/investigations`,
      },
    ];

    for (const item of cases) {
      postMock.mockResolvedValueOnce({
        data: item.data,
        status: item.status,
        requestID: `request-${item.status}`,
        traceID: `trace-${item.status}`,
        idempotentReplay: false,
      });
      await expect(item.invoke()).resolves.toMatchObject({ httpStatus: item.status });
      expect(postMock.mock.calls.at(-1)?.[0]).toBe(item.path);
    }
  });

  it("fails closed when the backend returns an undocumented command status", async () => {
    postMock.mockResolvedValue({
      data: alertFixture(),
      status: 202,
      requestID: "request-mismatch",
      traceID: "trace-mismatch",
      idempotentReplay: false,
    });

    await expect(acknowledgeAlert(alertID, 3, "Owner triage")).rejects.toEqual(expect.objectContaining({
      status: 202,
      code: "UNEXPECTED_COMMAND_STATUS",
      requestID: "request-mismatch",
      traceID: "trace-mismatch",
    }));
  });
});

describe("Alert new-row probe", () => {
  it("updates existing rows in place and holds new rows for user-controlled loading", () => {
    const existing = alertFixture({ version: 3, summary: "old projection" });
    const updated = alertFixture({ version: 4, summary: "updated projection" });
    const newAlert = alertFixture({
      id: "55555555-5555-4555-8555-555555555555",
      version: 1,
      summary: "new row",
    });

    const result = reconcileAlertProbe([existing], [newAlert, updated]);

    expect(result.items).toEqual([updated]);
    expect(result.pendingItems).toEqual([newAlert]);
  });
});
