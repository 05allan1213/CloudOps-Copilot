import { afterEach, describe, expect, it, vi } from "vitest";

import { openTopologyEventStream } from "./infrastructure";

class EventSourceStub {
  static instance: EventSourceStub | undefined;

  readonly listeners = new Map<string, EventListener>();
  readonly url: string;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: ((event: Event) => void) | null = null;

  constructor(url: string | URL) {
    this.url = String(url);
    EventSourceStub.instance = this;
  }

  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, listener);
  }

  emit(type: string, data: string, lastEventId = "") {
    this.listeners.get(type)?.(new MessageEvent(type, { data, lastEventId }));
  }

  close() {
    this.closed = true;
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
  EventSourceStub.instance = undefined;
});

describe("Topology SSE client", () => {
  it("uses bounded scope filters, parses the durable cursor, and tears down", () => {
    vi.stubGlobal("EventSource", EventSourceStub);
    const receive = vi.fn();
    const fail = vi.fn();
    const close = openTopologyEventStream({
      cluster: "cloudops-local",
      namespace: "cloudops-system",
      kind: ["Deployment"],
      search: "api",
      cursor: "page-2",
      limit: 10,
      from: "2026-08-02T15:00:00Z",
      to: "2026-08-02T16:00:00Z",
    }, receive, fail);
    const source = EventSourceStub.instance!;

    expect(source.url).toContain("/api/v1/topology/events?");
    expect(source.url).toContain("cluster=cloudops-local");
    expect(source.url).toContain("namespace=cloudops-system");
    expect(source.url).not.toContain("kind=");
    expect(source.url).not.toContain("search=");
    expect(source.url).not.toContain("cursor=");
    source.emit("topology.refresh", JSON.stringify({
      snapshot_id: "snapshot-2",
      content_hash: "hash-2",
      provider_state: "available",
      collected_at: "2026-08-02T16:00:01Z",
    }), "hash-2");

    expect(receive).toHaveBeenCalledWith(expect.objectContaining({ cursor: "hash-2", content_hash: "hash-2" }));
    expect(fail).not.toHaveBeenCalled();
    close();
    expect(source.closed).toBe(true);
  });

  it("rejects malformed refresh events without closing native reconnection", () => {
    vi.stubGlobal("EventSource", EventSourceStub);
    const fail = vi.fn();
    openTopologyEventStream({}, vi.fn(), fail);

    EventSourceStub.instance!.emit("topology.refresh", "not-json", "hash-2");

    expect(fail).toHaveBeenCalledOnce();
    expect(EventSourceStub.instance!.closed).toBe(false);
  });
});
