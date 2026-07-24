import { describe, expect, it } from "vitest";

import config from "../vite.config";

describe("development API proxy", () => {
  it("preserves the browser Host for same-origin API validation", () => {
    expect(config.server?.proxy?.["/api"]).toMatchObject({
      changeOrigin: false,
    });
  });
});
