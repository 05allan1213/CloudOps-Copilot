import { describe, expect, it } from "vitest";

import type { IncidentListQuery } from "../../types/incidents";
import {
  applyIncidentListRouteQuery,
  incidentListAPIIdentity,
} from "./useIncidentList";

describe("Incident list route synchronization", () => {
  it("does not reload API data for sort or Inspector-only route changes", () => {
    const base = { limit: 50, status: "investigating" as const };
    expect(incidentListAPIIdentity({ ...base, sort: "severity", direction: "asc" }))
      .toBe(incidentListAPIIdentity({
        ...base,
        sort: "status",
        direction: "desc",
        selected: "00000000-0000-4000-8000-000000000001",
      }));
  });

  it("clears fields removed by Back or Forward while retaining canonical defaults", () => {
    const target: IncidentListQuery = {
      limit: 100,
      status: "resolved",
      severity: "critical",
      service: "checkout",
      cursor: "page-2",
      sort: "severity",
      direction: "asc",
      selected: "00000000-0000-4000-8000-000000000001",
    };

    applyIncidentListRouteQuery(target, { limit: 50, sort: "updated", direction: "desc" });

    expect(target).toEqual({
      limit: 50,
      status: undefined,
      severity: undefined,
      service: undefined,
      attention: undefined,
      resource: undefined,
      alert: undefined,
      from: undefined,
      to: undefined,
      cursor: undefined,
      sort: "updated",
      direction: "desc",
      selected: undefined,
    });
  });
});
