import { describe, expect, it } from "vitest";

import {
  acceptRealtimeEvent,
  maximumReconnectAttempts,
  parseRefreshEvent,
  reconnectDelayForAttempt,
  successfulStreamPollDelay,
} from "./useIncidentRealtime";

describe("Incident SSE refresh hints", () => {
  it("suppresses duplicate cursors and foreign incident events", () => {
    const incidentID = "7f6c7e54-937a-4a12-9fb4-a315908fd0fd";
    const event = { cursor: "event-5", incident_id: incidentID, resource: "evidence" as const };
    expect(acceptRealtimeEvent("event-4", event, incidentID)).toBe("event-5");
    expect(acceptRealtimeEvent("event-5", event, incidentID)).toBeNull();
    expect(acceptRealtimeEvent("event-4", { ...event, incident_id: "foreign" }, incidentID)).toBeNull();
    expect(acceptRealtimeEvent("event-4", { ...event, resource: "private" as never }, incidentID)).toBeNull();
  });

  it("parses only the frozen incident.refresh contract", () => {
    const event = parseRefreshEvent('id: event-7\nevent: incident.refresh\ndata: {"incident_id":"incident-1","resource":"verification"}');
    expect(event).toBeNull();
    const valid = parseRefreshEvent('id: event-7\nevent: incident.refresh\ndata: {"incident_id":"incident-1","resource":"verifications"}');
    expect(valid).toEqual({ cursor: "event-7", incident_id: "incident-1", resource: "verifications" });
    expect(parseRefreshEvent('event: verdict\ndata: {"status":"passed"}')).toBeNull();
  });

  it("caps disconnect recovery", () => {
    const delays = Array.from({ length: maximumReconnectAttempts }, (_, attempt) => reconnectDelayForAttempt(attempt));
    expect(delays).toEqual([1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
    expect(reconnectDelayForAttempt(maximumReconnectAttempts)).toBeNull();
    expect(successfulStreamPollDelay).toBeGreaterThanOrEqual(1000);
  });
});
