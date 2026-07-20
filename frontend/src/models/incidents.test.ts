import { describe, expect, it } from "vitest";

import {
  incidentDetailPath,
  incidentStatusLabel,
  isCurrentRequest,
  loadStateForStatus,
  normalizeListQuery,
  serializeListQuery,
  severityLabel,
  statusTone,
} from "./incidents";

describe("V3 Incident Workbench presentation contract", () => {
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
  });

  it("maps transport and persisted resource states explicitly", () => {
    expect(loadStateForStatus(403)).toBe("forbidden");
    expect(loadStateForStatus(404)).toBe("not_found");
    expect(loadStateForStatus(503)).toBe("unavailable");
    expect(statusTone("approved")).toBe("success");
    expect(statusTone("inconclusive")).toBe("warning");
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
