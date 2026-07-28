export type CommandFeedbackState =
  | "submitting"
  | "accepted"
  | "forbidden"
  | "conflict"
  | "invalid"
  | "unavailable"
  | "error";

export interface CommandFeedback {
  state: CommandFeedbackState;
  action: string;
  resourceID: string;
  message: string;
  code: string;
  httpStatus: number;
  requestID: string;
  traceID: string;
  idempotencyKey: string;
  idempotentReplay: boolean;
  retryable: boolean;
}

export interface CommandFailureDetails {
  status: number | null;
  message: string;
  code?: string;
  requestID?: string;
  traceID?: string;
  idempotentReplay?: boolean;
}

export function commandFeedbackForFailure(
  details: CommandFailureDetails,
  action: string,
  resourceID: string,
  idempotencyKey: string,
): CommandFeedback {
  const state = commandFailureState(details.status);
  const retryable = details.status === null || [500, 502, 503, 504].includes(details.status);
  return {
    state,
    action,
    resourceID,
    message: details.message || "The command could not be completed.",
    code: details.code ?? "",
    httpStatus: details.status ?? 0,
    requestID: details.requestID ?? "",
    traceID: details.traceID ?? "",
    idempotencyKey,
    idempotentReplay: details.idempotentReplay ?? false,
    retryable,
  };
}

export function commandFailureState(status: number | null): Exclude<CommandFeedbackState, "submitting" | "accepted"> {
  if (status === 403) return "forbidden";
  if (status === 409) return "conflict";
  if (status === 400 || status === 413 || status === 422) return "invalid";
  if (status === 501 || status === 503) return "unavailable";
  return "error";
}

export interface CommandAttemptIdentity {
  action: string;
  resourceID: string;
  idempotencyKey: string;
}

export function retainCommandAttempt(attempt: CommandAttemptIdentity): CommandAttemptIdentity {
  return { ...attempt };
}
