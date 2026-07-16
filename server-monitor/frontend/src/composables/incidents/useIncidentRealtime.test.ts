import { describe, expect, it } from "vitest";

import {
  acceptRealtimeSequence,
  maximumReconnectAttempts,
  parseRefreshEvent,
  reconnectDelayForAttempt,
} from "./useIncidentRealtime";

describe("incident realtime refresh hints", () => {
  it("suppresses duplicates, old sequences and foreign incident events", () => {
    const incidentID = "7f6c7e54-937a-4a12-9fb4-a315908fd0fd";
    expect(acceptRealtimeSequence(4, { incident_id: incidentID, sequence: 5, kind: "refresh" }, incidentID)).toBe(5);
    expect(acceptRealtimeSequence(5, { incident_id: incidentID, sequence: 5, kind: "refresh" }, incidentID)).toBeNull();
    expect(acceptRealtimeSequence(5, { incident_id: "foreign", sequence: 6, kind: "refresh" }, incidentID)).toBeNull();
  });

  it("parses only incident-scoped refresh events", () => {
    const event = parseRefreshEvent('event: incident_refresh\ndata: {"incident_id":"incident-1","sequence":7,"kind":"refresh"}');
    expect(event).toEqual({ incident_id: "incident-1", sequence: 7, kind: "refresh" });
    expect(parseRefreshEvent('event: verdict\ndata: {"status":"passed"}')).toBeNull();
    expect(parseRefreshEvent("event: incident_refresh\ndata: not-json")).toBeNull();
  });

  it("caps disconnect recovery instead of resetting after every connection", () => {
    const delays = Array.from({ length: maximumReconnectAttempts }, (_, attempt) => reconnectDelayForAttempt(attempt));
    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
    expect(reconnectDelayForAttempt(maximumReconnectAttempts)).toBeNull();
  });
});
