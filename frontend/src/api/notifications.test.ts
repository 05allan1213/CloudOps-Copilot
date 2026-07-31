import { beforeEach, describe, expect, it, vi } from "vitest";

import { openNotificationStream, type OwnerNotification } from "./notifications";

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  readonly url: string;
  readonly listeners = new Map<string, (event: Event) => void>();
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  closed = false;

  constructor(url: string | URL) {
    this.url = String(url);
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    this.listeners.set(type, listener as (event: Event) => void);
  }

  emit(type: string, data: string) {
    this.listeners.get(type)?.({ data } as MessageEvent<string>);
  }

  close() {
    this.closed = true;
  }
}

const notification: OwnerNotification = {
  id: "notification-1",
  source_type: "incident",
  source_id: "incident-1",
  source_state: "awaiting_approval",
  severity: "P1",
  reason: "Owner review required",
  context_link: {
    workspace: "incidents",
    path: "/incidents/incident-1",
    query: {},
    operational_scope_id: "scope-1",
    external: false,
  },
  read: false,
  created_at: "2026-07-30T00:00:00Z",
};

describe("Notification SSE lifecycle", () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  it("opens its own stream, parses owner notifications, and closes independently", () => {
    const received = vi.fn();
    const onError = vi.fn();
    const onOpen = vi.fn();
    const close = openNotificationStream(received, onError, onOpen);
    const source = FakeEventSource.instances[0];

    expect(source.url).toBe("/api/v1/notification-events");
    source.onopen?.();
    source.emit("owner_notification.created", JSON.stringify(notification));

    expect(onOpen).toHaveBeenCalledOnce();
    expect(received).toHaveBeenCalledWith(notification);
    expect(onError).not.toHaveBeenCalled();

    close();
    expect(source.closed).toBe(true);
  });

  it("reports malformed events without replacing the independent connection", () => {
    const received = vi.fn();
    const onError = vi.fn();
    openNotificationStream(received, onError);
    const source = FakeEventSource.instances[0];

    source.emit("owner_notification.created", "not-json");

    expect(received).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledOnce();
    expect(source.closed).toBe(false);
  });
});
