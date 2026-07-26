import { describe, expect, it } from "vitest";

import {
  incidentDetailPath,
  incidentStatusLabel,
  humanizeCode,
  isCurrentRequest,
  loadStateForStatus,
  normalizeListQuery,
  serializeListQuery,
  severityLabel,
  statusTone,
} from "./incidents";

describe("Incident Workbench presentation contract", () => {
  it("uses the frozen seven-state lifecycle without deriving a verdict", () => {
    expect(incidentStatusLabel("verifying")).toBe("Verifying recovery");
    expect(incidentStatusLabel("future_state")).toBe("future state");
    expect(severityLabel("critical")).toBe("Critical");
  });

  it("restores only API-supported filters from the URL", () => {
    const normalized = normalizeListQuery({ limit: "100", status: ["resolved"], severity: "critical", service: " checkout ", q: "ignored" });
    expect(normalized).toEqual({ limit: 100, status: "resolved", severity: "critical", service: "checkout" });
    expect(serializeListQuery(normalized)).toEqual({ limit: "100", status: "resolved", severity: "critical", service: "checkout" });
    expect(normalizeListQuery({ status: "legacy", limit: "500" })).toEqual({ limit: 100 });
    expect(normalizeListQuery({ cursor: "page-2" })).toEqual({ limit: 50, cursor: "page-2" });
    expect(serializeListQuery({ limit: 50, cursor: "page-2" })).toEqual({ cursor: "page-2" });
  });

  it("maps transport and persisted resource states explicitly", () => {
    expect(loadStateForStatus(403)).toBe("forbidden");
    expect(loadStateForStatus(404)).toBe("not_found");
    expect(loadStateForStatus(503)).toBe("unavailable");
    expect(statusTone("approved")).toBe("success");
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
