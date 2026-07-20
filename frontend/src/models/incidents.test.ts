import { describe, expect, it } from "vitest";

import {
  factTone,
  incidentDetailPath,
  incidentStatusLabel,
  isCurrentRequest,
  loadStateForStatus,
  normalizeListQuery,
  postmortemStateForStatus,
  serializeListQuery,
  severityLabel,
  sortTimeline,
  statusTone,
  verificationRequirementLabel,
} from "./incidents";
import type { IncidentEvidenceDTO, IncidentTimelineDTO } from "../types/incidents";

describe("Incident Workbench presentation contract", () => {
  it("uses persisted status and severity labels without deriving a verdict", () => {
    expect(incidentStatusLabel("VERIFYING")).toBe("Verifying recovery");
    expect(incidentStatusLabel("future_state")).toBe("Unknown");
    expect(severityLabel("critical")).toBe("Critical");
  });

  it("sorts timeline deterministically by timestamp then stable key", () => {
    const items: IncidentTimelineDTO[] = [
      { key: "b", event_type: "second", actor_type: "system", summary: "", occurred_at: "2026-01-01T00:00:00Z" },
      { key: "c", event_type: "later", actor_type: "system", summary: "", occurred_at: "2026-01-01T00:00:01Z" },
      { key: "a", event_type: "first", actor_type: "system", summary: "", occurred_at: "2026-01-01T00:00:00Z" },
    ];
    expect(sortTimeline(items).map((item) => item.key)).toEqual(["a", "b", "c"]);
    expect(items.map((item) => item.key)).toEqual(["b", "c", "a"]);
  });

  it("restores bounded pagination and filters from URL query", () => {
    const normalized = normalizeListQuery({ page: "3", page_size: "50", status: ["RESOLVED"], q: " checkout " });
    expect(normalized).toEqual({ page: 3, page_size: 50, status: "RESOLVED", q: "checkout" });
    expect(serializeListQuery(normalized)).toEqual({ page: "3", page_size: "50", status: "RESOLVED", q: "checkout" });
    expect(normalizeListQuery({ page: "bad", page_size: "-1" })).toEqual({ page: 1, page_size: 20 });
    expect(normalizeListQuery({ page_size: "500" }).page_size).toBe(50);
  });

  it("maps permission, absence, postmortem and provider states explicitly", () => {
    expect(loadStateForStatus(403)).toBe("forbidden");
    expect(loadStateForStatus(404)).toBe("not_found");
    expect(loadStateForStatus(503)).toBe("unavailable");
    expect(postmortemStateForStatus(404)).toBe("not_generated");
    expect(statusTone("unavailable")).toBe("warning");
    expect(statusTone("no_data")).toBe("info");
  });

  it("keeps fact, inference and unknown visually distinct", () => {
    expect(factTone("fact")).toBe("fact");
    expect(factTone("inference")).toBe("inference");
    expect(factTone("unexpected")).toBe("unknown");
  });

  it("does not combine required and optional verification checks", () => {
    expect(verificationRequirementLabel(true)).toBe("Required");
    expect(verificationRequirementLabel(false)).toBe("Optional");
  });

  it("builds a stable encoded public UUID route", () => {
    expect(incidentDetailPath("7f6c7e54-937a-4a12-9fb4-a315908fd0fd")).toBe("/incidents/7f6c7e54-937a-4a12-9fb4-a315908fd0fd");
    expect(incidentDetailPath("../private")).toBe("/incidents/..%2Fprivate");
  });

  it("rejects stale request identities", () => {
    expect(isCurrentRequest(4, 4)).toBe(true);
    expect(isCurrentRequest(3, 4)).toBe(false);
  });

  it("safe evidence DTO contains no sensitive transport fields", () => {
    const item: IncidentEvidenceDTO = { id: "uuid", type: "metrics", source: "prometheus", summary: "bounded", state: "available", data_freshness: "unknown", related_claim: "Unknown", truncated: false, collected_at: "2026-01-01T00:00:00Z" };
    const payload = JSON.stringify(item);
    for (const forbidden of ["query", "authorization", "cookie", "dsn", "raw_ref", "token", "numeric_id"]) {
      expect(payload.toLowerCase()).not.toContain(forbidden);
    }
  });
});
