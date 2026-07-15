import { describe, expect, it } from "vitest";

import { acceptRealtimeSequence } from "./useIncidentRealtime";

describe("incident realtime refresh hints", () => {
  it("suppresses duplicates, old sequences and foreign incident events", () => {
    const incidentID = "7f6c7e54-937a-4a12-9fb4-a315908fd0fd";
    expect(acceptRealtimeSequence(4, { incident_id: incidentID, sequence: 5, kind: "refresh" }, incidentID)).toBe(5);
    expect(acceptRealtimeSequence(5, { incident_id: incidentID, sequence: 5, kind: "refresh" }, incidentID)).toBeNull();
    expect(acceptRealtimeSequence(5, { incident_id: "foreign", sequence: 6, kind: "refresh" }, incidentID)).toBeNull();
  });
});
