import { onBeforeUnmount, ref } from "vue";

import type { K8sEventSummary } from "../types";
import { getStoredToken } from "../api/authStorage";

type ConnectionState = "connecting" | "connected" | "disconnected";

const websocketBaseUrl = import.meta.env.VITE_WS_BASE_URL ?? "";

function buildWebSocketUrl() {
  let base: string;
  if (websocketBaseUrl) {
    base = `${websocketBaseUrl}/ws/alerts`;
  } else {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    base = `${protocol}//${window.location.host}/ws/alerts`;
  }

  const token = getStoredToken();
  if (token) {
    return `${base}?token=${encodeURIComponent(token)}`;
  }
  return base;
}

function isValidK8sEvent(data: unknown): data is K8sEventSummary {
  if (!data || typeof data !== "object") return false;
  const record = data as Record<string, unknown>;
  return (
    typeof record.name === "string" &&
    (typeof record.type === "string" || record.type === undefined) &&
    (typeof record.namespace === "string" || record.namespace === undefined)
  );
}

function isValidK8sEventMessage(data: unknown): data is { type: "k8s_event"; data: K8sEventSummary } {
  if (!data || typeof data !== "object") return false;
  const msg = data as Record<string, unknown>;
  return msg.type === "k8s_event" && isValidK8sEvent(msg.data);
}

export function useK8sEventsWebSocket(onEvent: (event: K8sEventSummary) => void) {
  const connectionState = ref<ConnectionState>("disconnected");
  let socket: WebSocket | null = null;
  let reconnectTimer: number | null = null;
  let manuallyClosed = false;
  let reconnectDelay = 1000;
  const maxReconnectDelay = 30000;

  function clearReconnectTimer() {
    if (reconnectTimer !== null) {
      window.clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function scheduleReconnect() {
    if (manuallyClosed) {
      return;
    }

    clearReconnectTimer();
    reconnectTimer = window.setTimeout(() => {
      connect();
    }, reconnectDelay);

    reconnectDelay = Math.min(reconnectDelay * 2, maxReconnectDelay);
  }

  function connect() {
    clearReconnectTimer();
    manuallyClosed = false;

    if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
      return;
    }

    connectionState.value = "connecting";
    socket = new WebSocket(buildWebSocketUrl());

    socket.onopen = () => {
      reconnectDelay = 1000;
      connectionState.value = "connected";
    };

    socket.onmessage = (event) => {
      try {
        const payload = JSON.parse(event.data);
        if (isValidK8sEventMessage(payload)) {
          onEvent(payload.data);
        }
      } catch {
        // ignore parse errors
      }
    };

    socket.onclose = () => {
      connectionState.value = "disconnected";
      socket = null;
      scheduleReconnect();
    };

    socket.onerror = () => {
      // onclose will fire after onerror, handle reconnection there
    };
  }

  function disconnect() {
    manuallyClosed = true;
    clearReconnectTimer();

    if (socket) {
      socket.close();
      socket = null;
    }

    connectionState.value = "disconnected";
  }

  onBeforeUnmount(() => {
    disconnect();
  });

  return {
    connectionState,
    connect,
    disconnect,
  };
}
