import { describe, expect, it } from "vitest";

import {
  incidentDetailPath,
  incidentStatusLabel,
  humanizeCode,
  incidentInspectorFailureKind,
  isCurrentRequest,
  loadStateForStatus,
  normalizeListQuery,
  serializeListQuery,
  severityLabel,
  statusTone,
  toIncidentListAPIQuery,
} from "./incidents";

describe("Incident presentation contract", () => {
  it("uses the frozen seven-state lifecycle without deriving a verdict", () => {
    expect(incidentStatusLabel("verifying")).toBe("恢复验证中");
    expect(incidentStatusLabel("future_state")).toBe("future state");
    expect(severityLabel("critical")).toBe("严重");
  });

  it("restores only API-supported filters from the URL", () => {
    const normalized = normalizeListQuery({ limit: "100", status: ["resolved"], severity: "critical", service: " checkout ", q: "ignored" });
    expect(normalized).toEqual({ limit: 100, status: "resolved", severity: "critical", service: "checkout" });
    expect(serializeListQuery(normalized)).toEqual({ limit: "100", status: "resolved", severity: "critical", service: "checkout" });
    expect(normalizeListQuery({ status: "legacy", limit: "500" })).toEqual({ limit: 100 });
    expect(normalizeListQuery({ cursor: "page-2" })).toEqual({ limit: 50, cursor: "page-2" });
    expect(serializeListQuery({ limit: 50, cursor: "page-2" })).toEqual({ cursor: "page-2" });
  });

  it("keeps client-owned sort and Inspector state in the URL contract", () => {
    const selected = "00000000-0000-4000-8000-000000000001";
    const normalized = normalizeListQuery({ sort: "severity", direction: "asc", selected });
    expect(normalized).toMatchObject({ sort: "severity", direction: "asc", selected });
    expect(serializeListQuery(normalized)).toMatchObject({ sort: "severity", direction: "asc", selected });
    expect(normalizeListQuery({ sort: "created", direction: "sideways", selected: "internal-id" })).toEqual({ limit: 50 });
  });

  it("separates API filters from URL-owned presentation state", () => {
    const selected = "00000000-0000-4000-8000-000000000001";
    expect(toIncidentListAPIQuery({
      limit: 50,
      status: "investigating",
      sort: "severity",
      direction: "asc",
      selected,
    })).toEqual({ limit: 50, status: "investigating" });
  });

  it("classifies Inspector targets without discarding transport identity", () => {
    const incidentID = "00000000-0000-4000-8000-000000000001";
    expect(incidentInspectorFailureKind("not-a-public-id")).toBe("invalid");
    expect(incidentInspectorFailureKind(incidentID)).toBe("ready");
    expect(incidentInspectorFailureKind(incidentID, null, "REQUEST_FAILED")).toBe("error");
    expect(incidentInspectorFailureKind(incidentID, 401, "UNAUTHENTICATED")).toBe("permission-denied");
    expect(incidentInspectorFailureKind(incidentID, 403, "FORBIDDEN")).toBe("permission-denied");
    expect(incidentInspectorFailureKind(incidentID, 404, "RESOURCE_NOT_FOUND")).toBe("deleted");
    expect(incidentInspectorFailureKind(incidentID, 410, "GONE")).toBe("expired");
    expect(incidentInspectorFailureKind(incidentID, 409, "AUTHORIZATION_EXPIRED")).toBe("expired");
    expect(incidentInspectorFailureKind(incidentID, 503, "PROVIDER_UNAVAILABLE")).toBe("error");
  });

  it("round-trips current-cycle coordination filters", () => {
    const alert = "7f6c7e54-937a-4a12-9fb4-a315908fd0fd";
    const normalized = normalizeListQuery({
      attention: "false",
      resource: " deployment/demo-api ",
      alert,
      from: "2026-07-27T01:00:00Z",
      to: "2026-07-27T02:00:00+00:00",
    });
    expect(normalized).toEqual({
      limit: 50,
      attention: false,
      resource: "deployment/demo-api",
      alert,
      from: "2026-07-27T01:00:00Z",
      to: "2026-07-27T02:00:00+00:00",
    });
    expect(serializeListQuery(normalized)).toMatchObject({ attention: "false", resource: "deployment/demo-api", alert });
    expect(normalizeListQuery({ alert: "internal-id", from: "yesterday" })).toEqual({ limit: 50 });
  });

  it("maps transport and persisted resource states explicitly", () => {
    expect(loadStateForStatus(403)).toBe("forbidden");
    expect(loadStateForStatus(404)).toBe("not_found");
    expect(loadStateForStatus(503)).toBe("unavailable");
    expect(statusTone("approved")).toBe("success");
    expect(statusTone("succeeded")).toBe("success");
    expect(statusTone("expired")).toBe("warning");
    expect(statusTone("verification_failed")).toBe("danger");
    expect(statusTone("inconclusive")).toBe("inconclusive");
    expect(statusTone("unavailable")).toBe("warning");
    expect(statusTone("partial")).toBe("warning");
    expect(statusTone("no_data")).toBe("inconclusive");
    expect(statusTone("superseded")).toBe("neutral");
    expect(statusTone("NOT RUN")).toBe("neutral");
    expect(statusTone("detected")).toBe("danger");
    expect(humanizeCode("evidence_dependency_unavailable")).toBe("Evidence Dependency Unavailable");
  });

  it("builds a stable encoded public UUID route", () => {
    expect(incidentDetailPath("7f6c7e54-937a-4a12-9fb4-a315908fd0fd")).toBe("/incidents/7f6c7e54-937a-4a12-9fb4-a315908fd0fd");
    expect(incidentDetailPath("../private")).toBe("/incidents/..%2Fprivate");
  });

  it("rejects stale request identities", () => {
    expect(isCurrentRequest(4, 4)).toBe(true);
    expect(isCurrentRequest(3, 4)).toBe(false);
  });
});
