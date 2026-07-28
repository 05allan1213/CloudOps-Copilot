import { afterEach, describe, expect, it, vi } from "vitest";

import { openAgentEventStream } from "./agent";

class EventSourceStub {
  static instance: EventSourceStub | undefined;

  readonly listeners = new Map<string, EventListener>();
  readonly url: string;
  closed = false;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string | URL) {
    this.url = String(url);
    EventSourceStub.instance = this;
  }

  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, listener);
  }

  emit(type: string, data: string) {
    this.listeners.get(type)?.(new MessageEvent(type, { data }));
  }

  close() {
    this.closed = true;
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
  EventSourceStub.instance = undefined;
});

describe("Agent SSE client", () => {
  it("parses named durable events and closes the stream", () => {
    vi.stubGlobal("EventSource", EventSourceStub);
    const receive = vi.fn();
    const fail = vi.fn();
    const close = openAgentEventStream("consultation 1", receive, fail);
    const source = EventSourceStub.instance!;

    source.emit("tool.completed", JSON.stringify({
      id: "event-2",
      run_id: "run-1",
      consultation_id: "consultation-1",
      sequence: 2,
      type: "tool.completed",
      payload: { tool: "logs.query", evidence_id: "evidence-1" },
      created_at: "2026-07-27T06:00:00Z",
    }));

    expect(source.url).toContain("/api/v1/agent/consultations/consultation%201/events");
    expect(receive).toHaveBeenCalledWith(expect.objectContaining({ id: "event-2", type: "tool.completed" }));
    expect(fail).not.toHaveBeenCalled();
    close();
    expect(source.closed).toBe(true);
  });

  it("reports malformed event payloads", () => {
    vi.stubGlobal("EventSource", EventSourceStub);
    const fail = vi.fn();
    openAgentEventStream("consultation-1", vi.fn(), fail);
    EventSourceStub.instance!.emit("answer.delta", "not-json");
    expect(fail).toHaveBeenCalledOnce();
  });
});
