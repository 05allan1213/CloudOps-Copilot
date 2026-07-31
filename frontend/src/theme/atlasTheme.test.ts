import { describe, expect, it } from "vitest";

import { atlasSemanticTheme } from "./atlasTheme";

function styles(tokens: Record<string, string>) {
  return {
    getPropertyValue: (name: string) => tokens[name] ?? "",
  };
}

describe("Atlas semantic theme", () => {
  it("maps canonical semantic tokens to scene roles", () => {
    const theme = atlasSemanticTheme(styles({
      "--co-bg-canvas": "rgb(1, 2, 3)",
      "--co-border-strong": "#edge",
      "--co-text-primary": "#light",
      "--co-focus-ring": "#selected",
      "--co-action-primary": "#action",
      "--co-status-info-fg": "#info",
      "--co-status-success-fg": "#success",
      "--co-status-warning-fg": "#warning",
      "--co-status-inconclusive-fg": "#inconclusive",
      "--co-status-neutral-fg": "#neutral",
      "--co-status-critical-fg": "#critical",
    }));

    expect(theme.background).toBe("rgb(1, 2, 3)");
    expect(theme.edge).toBe("#edge");
    expect(theme.selection).toBe("#selected");
    expect(theme.layer).toEqual({
      namespace: "#neutral",
      service: "#info",
      workload: "#inconclusive",
      pod: "#success",
      node: "#warning",
      gateway: "#action",
    });
    expect(theme.health.critical).toBe("#critical");
  });

  it("uses readable fallbacks when tokens are unavailable", () => {
    const theme = atlasSemanticTheme(styles({}));
    expect(theme.background).toBe("#f4f6f8");
    expect(theme.selection).toBe("#0b72e7");
    expect(theme.health.healthy).toBe("#18794e");
  });
});
