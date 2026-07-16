import { describe, expect, it } from "vitest";

import { webSocketBearerSubprotocol, webSocketConnectionOptions } from "./websocketAuth";

describe("legacy websocket authentication", () => {
  it("keeps bearer credentials out of browser URLs", () => {
    const token = "header.payload.signature";
    const options = webSocketConnectionOptions("wss://cloudops.example/ws/alerts", token);

    expect(options.url).toBe("wss://cloudops.example/ws/alerts");
    expect(options.url).not.toContain(token);
    expect(options.protocols).toEqual([webSocketBearerSubprotocol, token]);
  });

  it("does not invent an authentication protocol without a token", () => {
    expect(webSocketConnectionOptions("ws://localhost/ws/alerts", null)).toEqual({
      url: "ws://localhost/ws/alerts",
    });
  });
});
