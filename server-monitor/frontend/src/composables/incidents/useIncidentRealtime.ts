import { onBeforeUnmount, ref } from "vue";

import { incidentRealtimeURL } from "../../api/incidents";
import { getStoredToken } from "../../api/authStorage";
import type { IncidentRealtimeEvent } from "../../types/incidents";

export type RealtimeState = "connecting" | "connected" | "reconnecting" | "disconnected";

export const maximumReconnectAttempts = 8;

export function reconnectDelayForAttempt(attempt: number): number | null {
  if (!Number.isInteger(attempt) || attempt < 0 || attempt >= maximumReconnectAttempts) return null;
  return Math.min(1000 * 2 ** attempt, 30000);
}

export function acceptRealtimeSequence(lastSequence: number, event: IncidentRealtimeEvent, incidentID: string): number | null {
  if (event.incident_id !== incidentID || event.kind !== "refresh" || !Number.isSafeInteger(event.sequence) || event.sequence <= lastSequence) {
    return null;
  }
  return event.sequence;
}

export function useIncidentRealtime(incidentID: string, resync: () => Promise<void>) {
  const state = ref<RealtimeState>("disconnected");
  const lastSequence = ref(0);
  let controller: AbortController | null = null;
  let reconnectTimer: number | null = null;
  let reconnectAttempts = 0;
  let stopped = false;

  function clearTimer() {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  async function connect() {
    if (stopped || controller) return;
    clearTimer();
    state.value = reconnectAttempts === 0 ? "connecting" : "reconnecting";
    controller = new AbortController();
    try {
      const headers: Record<string, string> = { Accept: "text/event-stream" };
      const token = getStoredToken();
      if (token) headers.Authorization = `Bearer ${token}`;
      const response = await fetch(incidentRealtimeURL(incidentID), { headers, signal: controller.signal });
      if (!response.ok || !response.body) throw new Error(`Realtime request failed with status ${response.status}`);
      state.value = "connected";
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      while (!stopped) {
        const { done, value } = await reader.read();
        buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
        let separator = buffer.indexOf("\n\n");
        while (separator >= 0) {
          const block = buffer.slice(0, separator);
          buffer = buffer.slice(separator + 2);
          const event = parseRefreshEvent(block);
          if (event) {
            const accepted = acceptRealtimeSequence(lastSequence.value, event, incidentID);
            if (accepted !== null) {
              lastSequence.value = accepted;
              await resync();
            }
          }
          separator = buffer.indexOf("\n\n");
        }
        if (done) break;
      }
      if (!stopped) await resync().catch(() => undefined);
    } catch (cause) {
      if (!stopped && !(cause instanceof DOMException && cause.name === "AbortError")) {
        await resync().catch(() => undefined);
      }
    } finally {
      controller = null;
      if (!stopped) scheduleReconnect();
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

  function start() {
    stopped = false;
    reconnectAttempts = 0;
    void connect();
  }

  function stop() {
    stopped = true;
    clearTimer();
    controller?.abort();
    controller = null;
    state.value = "disconnected";
  }

  onBeforeUnmount(stop);
  return { state, lastSequence, start, stop };
}

export function parseRefreshEvent(block: string): IncidentRealtimeEvent | null {
  let name = "message";
  const data: string[] = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) name = line.slice(6).trim();
    if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
  }
  if (name !== "incident_refresh" || data.length === 0) return null;
  try {
    const parsed = JSON.parse(data.join("\n")) as Partial<IncidentRealtimeEvent>;
    if (typeof parsed.incident_id !== "string" || typeof parsed.sequence !== "number" || parsed.kind !== "refresh") return null;
    return parsed as IncidentRealtimeEvent;
  } catch {
    return null;
  }
}
