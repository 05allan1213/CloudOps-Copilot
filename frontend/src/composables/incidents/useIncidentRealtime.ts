import { onBeforeUnmount, ref } from "vue";

import { incidentRealtimeURL } from "../../api/incidents";
import type { IncidentRealtimeEvent } from "../../types/incidents";

export type RealtimeState = "connecting" | "connected" | "reconnecting" | "disconnected";

export const maximumReconnectAttempts = 8;
export const successfulStreamPollDelay = 2000;

const refreshResources = new Set([
  "incident",
  "signals",
  "timeline",
  "evidence",
  "investigations",
  "remediation_plans",
  "delivery",
  "verifications",
  "resolution_report",
]);

export function reconnectDelayForAttempt(attempt: number): number | null {
  if (!Number.isInteger(attempt) || attempt < 0 || attempt >= maximumReconnectAttempts) return null;
  return Math.min(1000 * 2 ** attempt, 30000);
}

export function acceptRealtimeEvent(lastCursor: string, event: IncidentRealtimeEvent, incidentID: string): string | null {
  if (event.incident_id !== incidentID || event.cursor === "" || event.cursor === lastCursor || !refreshResources.has(event.resource)) {
    return null;
  }
  return event.cursor;
}

export function useIncidentRealtime(incidentID: string, resync: (resource: IncidentRealtimeEvent["resource"]) => Promise<void>) {
  const state = ref<RealtimeState>("disconnected");
  const lastCursor = ref("");
  const notice = ref("");
  const lastEventAt = ref("");
  const seenCursors = new Set<string>();
  const cursorOrder: string[] = [];
  let controller: AbortController | null = null;
  let reconnectTimer: number | null = null;
  let reconnectAttempts = 0;
  let stopped = false;
  let hasConnected = false;

  function clearTimer() {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  async function connect() {
    if (stopped || controller) return;
    clearTimer();
    if (!hasConnected) state.value = reconnectAttempts === 0 ? "connecting" : "reconnecting";
    controller = new AbortController();
    let failed = false;
    try {
      const headers: Record<string, string> = { Accept: "text/event-stream" };
      if (lastCursor.value) headers["Last-Event-ID"] = lastCursor.value;
      const response = await fetch(incidentRealtimeURL(incidentID), {
        headers,
        signal: controller.signal,
      });
      if (!response.ok || !response.body) throw new Error(`Realtime request failed with status ${response.status}`);
      const wasConnected = hasConnected;
      const restored = wasConnected && reconnectAttempts > 0;
      reconnectAttempts = 0;
      hasConnected = true;
      state.value = "connected";
      if (restored) notice.value = "Realtime connection restored.";
      else if (!wasConnected) notice.value = "Realtime connection established.";
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (!stopped) {
        const { done, value } = await reader.read();
        buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done }).replace(/\r\n/g, "\n");
        let separator = buffer.indexOf("\n\n");
        while (separator >= 0) {
          const block = buffer.slice(0, separator);
          buffer = buffer.slice(separator + 2);
          const event = parseRefreshEvent(block);
          if (event) {
            const accepted = acceptRealtimeEvent(lastCursor.value, event, incidentID);
            if (accepted !== null && !seenCursors.has(accepted)) {
              lastCursor.value = accepted;
              rememberCursor(accepted);
              lastEventAt.value = new Date().toISOString();
              try {
                await resync(event.resource);
                notice.value = `${event.resource.replace(/_/g, " ")} projection updated.`;
              } catch {
                notice.value = "An update arrived, but the latest projection could not be refreshed.";
              }
            }
          }
          separator = buffer.indexOf("\n\n");
        }
        if (done) break;
      }
    } catch (cause) {
      if (!stopped && !(cause instanceof DOMException && cause.name === "AbortError")) {
        failed = true;
      }
    } finally {
      controller = null;
      if (!stopped) {
        if (failed) scheduleReconnect();
        else scheduleSuccessfulPoll();
      }
    }
  }

  function scheduleReconnect() {
    const delay = reconnectDelayForAttempt(reconnectAttempts);
    if (stopped || delay === null) {
      state.value = "disconnected";
      return;
    }
    state.value = "reconnecting";
    reconnectAttempts += 1;
    reconnectTimer = window.setTimeout(() => void connect(), delay);
  }

  function scheduleSuccessfulPoll() {
    if (stopped) return;
    state.value = "connected";
    reconnectTimer = window.setTimeout(() => void connect(), successfulStreamPollDelay);
  }

  function start() {
    stopped = false;
    reconnectAttempts = 0;
    hasConnected = false;
    void connect();
  }

  function stop() {
    stopped = true;
    clearTimer();
    controller?.abort();
    controller = null;
    state.value = "disconnected";
    notice.value = "Realtime updates stopped.";
  }

  function rememberCursor(cursor: string) {
    if (seenCursors.has(cursor)) return;
    seenCursors.add(cursor);
    cursorOrder.push(cursor);
    if (cursorOrder.length <= 256) return;
    const oldest = cursorOrder.shift();
    if (oldest) seenCursors.delete(oldest);
  }

  onBeforeUnmount(stop);
  return { state, lastCursor, notice, lastEventAt, start, stop };
}

export function parseRefreshEvent(block: string): IncidentRealtimeEvent | null {
  let eventType = "message";
  let cursor = "";
  const data: string[] = [];
  for (const line of block.replace(/\r\n/g, "\n").split("\n")) {
    if (line.startsWith("event:")) eventType = line.slice(6).trim();
    if (line.startsWith("id:")) cursor = line.slice(3).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (eventType !== "incident.refresh" || !cursor || data.length === 0) return null;
  try {
    const payload = JSON.parse(data.join("\n")) as Partial<IncidentRealtimeEvent>;
    if (typeof payload.incident_id !== "string" || typeof payload.resource !== "string" || !refreshResources.has(payload.resource)) return null;
    return { cursor, incident_id: payload.incident_id, resource: payload.resource as IncidentRealtimeEvent["resource"] };
  } catch {
    return null;
  }
}
