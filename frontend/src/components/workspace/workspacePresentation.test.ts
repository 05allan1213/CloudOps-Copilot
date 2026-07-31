import { describe, expect, it } from "vitest";

import {
  missingRiskConfirmationFacts,
  realtimeTrustDefinition,
  riskConfirmationDefinition,
  workspaceStateDefinition,
} from "./workspacePresentation";

describe("Workspace presentation contracts", () => {
  it("keeps exceptional target states distinct", () => {
    expect(workspaceStateDefinition("permission-denied").title).toContain("无权");
    expect(workspaceStateDefinition("expired").title).toContain("过期");
    expect(workspaceStateDefinition("deleted").title).toContain("删除");
    expect(workspaceStateDefinition("invalid").description).toContain("不会自动选择第一行");
  });

  it("only claims live when cursor continuity is trustworthy", () => {
    expect(realtimeTrustDefinition("live").live).toBe(true);
    for (const state of ["connecting", "reconnecting", "disconnected", "stale", "cursor-expired", "resyncing", "resync-failed", "stopped"] as const) {
      expect(realtimeTrustDefinition(state).live, state).toBe(false);
    }
  });

  it("uses different facts and commands for each risk class", () => {
    expect(riskConfirmationDefinition("acknowledgement").confirmLabel).not.toBe(
      riskConfirmationDefinition("forced-termination").confirmLabel,
    );
    expect(missingRiskConfirmationFacts("approval", {
      target: "deployment/cloudops-api",
      effect: "批准候选版本",
    })).toEqual(["authority", "exactHash"]);
    expect(missingRiskConfirmationFacts("approval", {
      target: "deployment/cloudops-api",
      effect: "批准候选版本",
      authority: "owner-local",
      exactHash: "sha256:abc",
    })).toEqual([]);
  });
});
