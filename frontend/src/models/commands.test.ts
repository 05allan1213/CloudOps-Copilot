import { describe, expect, it } from "vitest";

import { commandHeaders, newCommandKey } from "../api/incidents";
import {
  commandFailureState,
  commandFeedbackForFailure,
  retainCommandAttempt,
} from "./commands";

describe("command presentation and retry identity", () => {
  it.each([
    [403, "forbidden"],
    [409, "conflict"],
    [400, "invalid"],
    [413, "invalid"],
    [422, "invalid"],
    [501, "unavailable"],
    [503, "unavailable"],
    [500, "error"],
    [null, "error"],
  ] as const)("maps HTTP %s to %s", (status, state) => {
    expect(commandFailureState(status)).toBe(state);
  });

  it("preserves request metadata and only retries transient failures", () => {
    const transient = commandFeedbackForFailure({
      status: 503,
      message: "command service unavailable",
      code: "COMMAND_UNAVAILABLE",
      requestID: "request-503",
      traceID: "trace-503",
      idempotentReplay: true,
    }, "Approve Plan", "plan-1", "stable-key");
    expect(transient).toMatchObject({
      state: "unavailable",
      httpStatus: 503,
      requestID: "request-503",
      traceID: "trace-503",
      idempotencyKey: "stable-key",
      idempotentReplay: true,
      retryable: true,
    });

    const conflict = commandFeedbackForFailure({ status: 409, message: "stale" }, "Approve Plan", "plan-1", "stable-key");
    expect(conflict.retryable).toBe(false);
    expect(conflict.state).toBe("conflict");
  });

  it("reuses the exact idempotency key for a retained retry attempt", () => {
    const idempotencyKey = newCommandKey("approved", "11111111-1111-4111-8111-111111111111");
    const attempt = retainCommandAttempt({
      action: "Approve Plan",
      resourceID: "11111111-1111-4111-8111-111111111111",
      idempotencyKey,
    });
    const retry = retainCommandAttempt(attempt);
    expect(retry.idempotencyKey).toBe(attempt.idempotencyKey);
    expect(commandHeaders("csrf-token", attempt.idempotencyKey)).toEqual(commandHeaders("csrf-token", retry.idempotencyKey));
    expect(idempotencyKey.length).toBeLessThanOrEqual(128);
  });
});
