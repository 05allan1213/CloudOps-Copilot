import { onBeforeUnmount, ref } from "vue";

import type { AlertEvent, AlertRecord, Host } from "../types";
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

function isValidAlertEvent(data: unknown): data is AlertEvent {
  if (!data || typeof data !== "object") return false;
  const record = data as Record<string, unknown>;
  return (
    typeof record.fingerprint === "string" &&
    typeof record.status === "string" &&
    (record.status === "firing" || record.status === "resolved") &&
    typeof record.receivedAt === "string" &&
    typeof record.labels === "object" && record.labels !== null &&
    typeof record.annotations === "object" && record.annotations !== null
  );
}

function isValidHost(data: unknown): data is Host {
  if (!data || typeof data !== "object") return false;
  const h = data as Record<string, unknown>;
  return (
    typeof h.instance === "string" &&
    typeof h.cpu === "number" &&
    typeof h.memory === "number" &&
    typeof h.status === "string"
  );
}

function isValidHostsMessage(data: unknown): data is { type: "hosts"; data: Host[] } {
  if (!data || typeof data !== "object") return false;
  const msg = data as Record<string, unknown>;
  if (msg.type !== "hosts" || !Array.isArray(msg.data)) return false;
  return msg.data.every(isValidHost);
}

export function useAlertsWebSocket(
  onAlert: (alert: AlertEvent) => void,
  onHosts?: (hosts: Host[]) => void,
) {
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
        if (isValidAlertEvent(payload)) {
          onAlert(payload);
        } else if (isValidHostsMessage(payload) && onHosts) {
          onHosts(payload.data);
        } else {
          console.warn("Unknown websocket message", payload);
        }
      } catch (error) {
        console.error("Failed to parse websocket message", error);
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
