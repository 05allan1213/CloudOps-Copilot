import { describe, expect, it } from "vitest";

import {
  agentContextHasEvidence,
  freeQueryContext,
  fullAgentWorkspacePath,
  readAgentRouteSelection,
  shouldClearAgentContextOnUnmount,
  shouldStopGlobalAgent,
  type AgentPageContext,
} from "./agentContext";

describe("global Agent lifecycle", () => {
  it("stays idle when the overlay is closed outside the Agent Workspace", () => {
    expect(shouldStopGlobalAgent(false, "/incidents")).toBe(true);
    expect(shouldStopGlobalAgent(false, "/overview")).toBe(true);
  });

  it("keeps the shared stream owner alive for an open overlay or the full Workspace", () => {
    expect(shouldStopGlobalAgent(true, "/incidents")).toBe(false);
    expect(shouldStopGlobalAgent(false, "/agent")).toBe(false);
  });

  it("retains a telemetry Context while entering the full Agent Workspace", () => {
    expect(shouldClearAgentContextOnUnmount("/agent")).toBe(false);
    expect(shouldClearAgentContextOnUnmount("/overview")).toBe(true);
  });

  it("restores canonical Consultation and legacy run route identities", () => {
    expect(readAgentRouteSelection({ consultation: "consultation-1", investigation: "run-1" })).toEqual({
      consultationID: "consultation-1",
      investigationID: "run-1",
    });
    expect(readAgentRouteSelection({ run: ["legacy-run", "ignored"] })).toEqual({
      consultationID: "",
      investigationID: "legacy-run",
    });
  });

  it("carries the exact selected record into the full Agent Workspace URL", () => {
    expect(fullAgentWorkspacePath("consultation", "consultation 1")).toBe("/agent?consultation=consultation%201");
    expect(fullAgentWorkspacePath("investigation", "run-1")).toBe("/agent?investigation=run-1");
    expect(fullAgentWorkspacePath("consultation", "")).toBe("/agent");
  });

  it("marks free queries as unassociated without weakening the real Evidence boundary", () => {
    const context: AgentPageContext = {
      route: "/logs?query=query-1",
      input: {
        title: "Logs Context",
        cluster_id: "cloudops-local",
        environment: "local",
        namespaces: ["cloudops-system"],
        resource_refs: [{ id: "resource-1", kind: "Deployment", namespace: "cloudops-system", name: "cloudops-api" }],
        filters: { level: "error" },
        from: "2026-07-31T01:00:00Z",
        to: "2026-07-31T02:00:00Z",
        query_definition_refs: [],
        query_execution_refs: ["query-1"],
        evidence_refs: ["evidence-1"],
      },
    };

    const free = freeQueryContext(context);

    expect(agentContextHasEvidence(free.input)).toBe(true);
    expect(free.input.query_execution_refs).toEqual(["query-1"]);
    expect(free.input.evidence_refs).toEqual(["evidence-1"]);
    expect(free.input.filters).toMatchObject({ agent_entry: "free_query", unassociated_event: true });
    expect(context.input.filters).toEqual({ level: "error" });
  });
});
