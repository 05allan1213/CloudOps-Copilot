import { describe, expect, it } from "vitest";

import {
  acceptRealtimeEvent,
  createProjectionRefreshQueue,
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

  it("coalesces replay bursts by resource and bounds trailing refreshes", async () => {
    const calls: string[] = [];
    let releaseTimeline: () => void = () => {};
    const timelineBlocked = new Promise<void>((resolve) => { releaseTimeline = resolve; });
    const queue = createProjectionRefreshQueue(async (resource) => {
      calls.push(resource);
      if (resource === "timeline" && calls.length === 1) await timelineBlocked;
    });

    for (let index = 0; index < 500; index += 1) queue.enqueue("timeline");
    queue.enqueue("evidence");
    queue.enqueue("delivery");
    await Promise.resolve();
    expect(calls).toEqual(["timeline"]);
    expect(queue.pendingCount()).toBeLessThanOrEqual(4);

    for (let index = 0; index < 500; index += 1) queue.enqueue("timeline");
    releaseTimeline();
    await queue.whenIdle();
    expect(calls).toEqual(["timeline", "evidence", "delivery", "timeline"]);
    expect(queue.pendingCount()).toBe(0);
  });

  it("reports resync failure and stops queued work during teardown", async () => {
    const failures: string[] = [];
    const calls: string[] = [];
    const queue = createProjectionRefreshQueue(async (resource) => {
      calls.push(resource);
      throw new Error("projection unavailable");
    }, {
      onFailure: (resource) => failures.push(resource),
    });
    queue.enqueue("verifications");
    await queue.whenIdle();
    expect(failures).toEqual(["verifications"]);

    const stopped = createProjectionRefreshQueue(async (resource) => { calls.push(resource); });
    stopped.enqueue("timeline");
    stopped.enqueue("evidence");
    stopped.stop();
    await stopped.whenIdle();
    await Promise.resolve();
    expect(calls).toEqual(["verifications"]);
  });
});
