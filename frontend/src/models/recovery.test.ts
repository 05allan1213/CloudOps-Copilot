import { describe, expect, it } from "vitest";

import type { RemediationPlanView, ResolutionReportView, VerificationRunView } from "../types/incidents";
import {
  canExposeResolutionReport,
  latestVerificationRun,
  planDecisionAvailability,
  resolutionReportIsConsistent,
  stabilityProgress,
  verificationDisplayState,
  verificationStateLabel,
} from "./recovery";

function plan(overrides: Partial<RemediationPlanView> = {}): RemediationPlanView {
  return {
    incident_version: 6,
    status: "awaiting_approval",
    expires_at: new Date(Date.now() + 60_000).toISOString(),
    ...overrides,
  } as RemediationPlanView;
}

function run(attempt: number, status: string, updatedAt: string): VerificationRunView {
  return {
    id: `run-${attempt}`,
    attempt,
    status,
    updated_at: updatedAt,
    created_at: updatedAt,
    common_window: { stability_window_ms: 60_000 },
  } as VerificationRunView;
}

describe("approval fail-closed presentation", () => {
  it("only enables the local Owner on the current, unexpired awaiting-approval Plan", () => {
    expect(planDecisionAvailability(plan(), {
      incidentVersion: 7,
      incidentStatus: "awaiting_approval",
      nowMs: Date.now(),
    }).available).toBe(true);

    expect(planDecisionAvailability(plan({ expires_at: new Date(Date.now() - 1).toISOString() }), {
      incidentVersion: 7,
      incidentStatus: "awaiting_approval",
    })).toMatchObject({ available: false, expired: true });

    expect(planDecisionAvailability(plan(), {
      incidentVersion: 8,
      incidentStatus: "awaiting_approval",
    })).toMatchObject({ available: false, stale: true });

    expect(planDecisionAvailability(plan(), {
      incidentVersion: 7,
      incidentStatus: "awaiting_approval",
      conflict: true,
    })).toMatchObject({ available: false, reason: expect.stringContaining("Refresh") });
  });

  it("fails closed when expiry cannot be parsed or a Decision already exists", () => {
    expect(planDecisionAvailability(plan({ expires_at: "not-a-date" }), {
      incidentVersion: 7,
      incidentStatus: "awaiting_approval",
    }).expired).toBe(true);
    expect(planDecisionAvailability(plan({ decision: {} as RemediationPlanView["decision"] }), {
      incidentVersion: 7,
      incidentStatus: "awaiting_approval",
    }).available).toBe(false);

    expect(planDecisionAvailability(plan(), {
      incidentVersion: 7,
      incidentStatus: "delivering",
    })).toMatchObject({ available: false, reason: expect.stringContaining("not awaiting approval") });
  });
});

describe("verification and ResolutionReport gating", () => {
  it.each([
    [undefined, "not_run", "NOT RUN"],
    ["pending", "pending", "等待执行"],
    ["running", "running", "执行中"],
    ["passed", "passed", "已通过"],
    ["failed", "failed", "未通过"],
    ["timed_out", "timed_out", "已超时"],
    ["inconclusive", "inconclusive", "无明确结论"],
    ["cancelled", "cancelled", "已取消"],
    ["unexpected", "not_run", "NOT RUN"],
  ] as const)("keeps %s distinct", (status, state, label) => {
    expect(verificationDisplayState(status)).toBe(state);
    expect(verificationStateLabel(status)).toBe(label);
  });

  it("uses only the latest attempt to expose ResolutionReport", () => {
    const olderPass = run(1, "passed", "2026-07-20T00:00:00Z");
    const latestFailure = run(2, "failed", "2026-07-20T00:02:00Z");
    expect(latestVerificationRun([olderPass, latestFailure])?.id).toBe(latestFailure.id);
    expect(canExposeResolutionReport([olderPass, latestFailure])).toBe(false);
    expect(canExposeResolutionReport([latestFailure, run(3, "passed", "2026-07-20T00:03:00Z")])).toBe(true);
    expect(canExposeResolutionReport([])).toBe(false);
  });

  it("keeps the common window at zero without success_since and uses persisted completion for 100 percent", () => {
    const running = run(1, "running", "2026-07-20T00:00:00Z");
    expect(stabilityProgress(running, Date.parse("2026-07-20T00:01:00Z"))).toMatchObject({
      elapsedMs: 0,
      percent: 0,
      source: "not_projected",
    });

    const completed = {
      ...running,
      common_window: {
        stability_window_ms: 60_000,
        success_since: "2026-07-20T00:00:00Z",
        completed_at: "2026-07-20T00:01:00Z",
      },
    };
    expect(stabilityProgress(completed, Date.parse("2026-07-20T00:01:00Z"))).toMatchObject({
      elapsedMs: 60_000,
      percent: 100,
      source: "persisted_completion",
    });

    const terminalWithoutCompletedWindow = {
      ...run(2, "failed", "2026-07-20T00:02:00Z"),
      completed_at: "2026-07-20T00:02:00Z",
    };
    expect(stabilityProgress(terminalWithoutCompletedWindow, Date.parse("2026-07-20T00:02:00Z"))).toMatchObject({
      elapsedMs: 0,
      percent: 0,
      source: "not_projected",
    });
  });

  it("requires both a report and a latest passed run", () => {
    const report = {} as ResolutionReportView;
    expect(resolutionReportIsConsistent([run(1, "passed", "2026-07-20T00:00:00Z")], report)).toBe(true);
    expect(resolutionReportIsConsistent([run(1, "inconclusive", "2026-07-20T00:00:00Z")], report)).toBe(false);
    expect(resolutionReportIsConsistent([run(1, "passed", "2026-07-20T00:00:00Z")], null)).toBe(false);
  });
});
