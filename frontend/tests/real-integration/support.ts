import { expect, test, type Page, type TestInfo } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export type CapabilityStatus = "PASS" | "FAIL" | "NOT RUN" | "BACKEND_GAP";

interface CapabilityDefinition {
  capability_id: string;
  status: CapabilityStatus;
  api_operations: string[];
  [key: string]: unknown;
}

interface CapabilityAttempt {
  attempt: number;
  started_at: string;
  completed_at: string;
  source_head: string;
  ui_action: string;
  api_evidence: BrowserApiEvidence[];
  console_errors: string[];
  page_errors: string[];
  ui_result: string;
  status: CapabilityStatus;
  root_cause?: string;
  blocked_by?: string;
  commit?: string;
  evidence_file: string;
}

interface LedgerCapability extends CapabilityDefinition {
  attempts: CapabilityAttempt[];
}

interface CapabilityLedger {
  run_id: string;
  checkpoint_version: number;
  source_head: string;
  updated_at: string;
  capabilities: LedgerCapability[];
}

export interface BrowserApiEvidence {
  method: string;
  path: string;
  status: number | null;
  request_id: string;
  trace_id: string;
  idempotent_replay: string;
  failure: string;
}

export interface BrowserEvidenceTracker {
  mark: () => number;
  since: (mark: number) => BrowserApiEvidence[];
  consoleErrors: string[];
  pageErrors: string[];
}

export interface CapabilityProofOptions {
  capabilityID: string;
  uiAction: string;
  expectedOperations?: string[];
  uiResult: string;
  commit?: string;
}

export interface CapabilityDispositionOptions extends CapabilityProofOptions {
  status: "NOT RUN" | "BACKEND_GAP";
  blockedBy: string;
  rootCause?: string;
}

const repoRoot = path.resolve(fileURLToPath(new URL("../../../", import.meta.url)));
const manifestPath = path.join(repoRoot, "frontend", "tests", "real-integration", "capabilities.json");
const runID = process.env.CLOUDOPS_REAL_INTEGRATION_RUN_ID || "";
const sourceHead = process.env.CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD || "";

function requiredIdentity() {
  if (!runID) throw new Error("CLOUDOPS_REAL_INTEGRATION_RUN_ID is required");
  if (!sourceHead) throw new Error("CLOUDOPS_REAL_INTEGRATION_SOURCE_HEAD is required");
  return {
    runRoot: path.join(repoRoot, ".cloudops", "integration", runID),
    ledgerPath: path.join(repoRoot, ".cloudops", "integration", runID, "capability-ledger.json"),
  };
}

function readManifest(): CapabilityDefinition[] {
  const parsed = JSON.parse(fs.readFileSync(manifestPath, "utf8")) as { capabilities: CapabilityDefinition[] };
  return parsed.capabilities;
}

function atomicWriteJSON(file: string, value: unknown) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const temporary = `${file}.${process.pid}.${Date.now()}.tmp`;
  fs.writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, { mode: 0o600 });
  fs.renameSync(temporary, file);
}

export function recordRunArtifact(relativePath: string, value: unknown) {
  const { runRoot } = requiredIdentity();
  atomicWriteJSON(path.join(runRoot, relativePath), value);
}

export function readRunArtifact<T>(relativePath: string): T | undefined {
  const { runRoot } = requiredIdentity();
  const file = path.join(runRoot, relativePath);
  if (!fs.existsSync(file)) return undefined;
  return JSON.parse(fs.readFileSync(file, "utf8")) as T;
}

export function capabilityPassed(capabilityID: string): boolean {
  return readLedger().capabilities.some((item) => item.capability_id === capabilityID && item.status === "PASS");
}

export function capabilityAttemptCount(capabilityID: string): number {
  return readLedger().capabilities.find((item) => item.capability_id === capabilityID)?.attempts.length ?? 0;
}

function readLedger(): CapabilityLedger {
  const { ledgerPath } = requiredIdentity();
  if (fs.existsSync(ledgerPath)) {
    const parsed = JSON.parse(fs.readFileSync(ledgerPath, "utf8")) as Partial<CapabilityLedger>;
    if (parsed.run_id && parsed.run_id !== runID) throw new Error(`Ledger run_id mismatch: ${parsed.run_id}`);
    const existing = new Map((parsed.capabilities ?? []).map((item) => [item.capability_id, item]));
    return {
      run_id: runID,
      checkpoint_version: 1,
      source_head: sourceHead,
      updated_at: new Date().toISOString(),
      capabilities: readManifest().map((definition) => {
        const prior = existing.get(definition.capability_id);
        return {
          ...prior,
          ...definition,
          status: prior?.status ?? definition.status,
          attempts: prior?.attempts ?? [],
        };
      }),
    };
  }
  return {
    run_id: runID,
    checkpoint_version: 1,
    source_head: sourceHead,
    updated_at: new Date().toISOString(),
    capabilities: readManifest().map((definition) => ({ ...definition, attempts: [] })),
  };
}

function operationPattern(operation: string): { method: string; path: RegExp } {
  const separator = operation.indexOf(" ");
  const method = operation.slice(0, separator).toUpperCase();
  const template = operation.slice(separator + 1);
  const escaped = template.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replace(/\\\{[^}]+\\\}/g, "[^/?]+");
  return { method, path: new RegExp(`^${escaped}$`) };
}

function assertOperations(records: BrowserApiEvidence[], operations: string[]) {
  for (const operation of operations) {
    const expected = operationPattern(operation);
    const match = records.find((record) => (
      record.method === expected.method
      && expected.path.test(record.path)
      && record.status !== null
      && record.status >= 200
      && record.status < 300
    ));
    expect(match, `browser Network did not contain successful ${operation}`).toBeTruthy();
  }
}

async function recordAttempt(
  options: CapabilityProofOptions,
  startedAt: string,
  status: CapabilityStatus,
  apiEvidence: BrowserApiEvidence[],
  tracker: BrowserEvidenceTracker,
  cause = "",
  blockedBy = "",
) {
  const { runRoot, ledgerPath } = requiredIdentity();
  const ledger = readLedger();
  const capability = ledger.capabilities.find((item) => item.capability_id === options.capabilityID);
  if (!capability) throw new Error(`Unknown capability ${options.capabilityID}`);
  const attemptNumber = capability.attempts.length + 1;
  const evidenceRelative = path.join("attempts", options.capabilityID, `attempt-${String(attemptNumber).padStart(2, "0")}.json`);
  const attempt: CapabilityAttempt = {
    attempt: attemptNumber,
    started_at: startedAt,
    completed_at: new Date().toISOString(),
    source_head: sourceHead,
    ui_action: options.uiAction,
    api_evidence: apiEvidence,
    console_errors: [...tracker.consoleErrors],
    page_errors: [...tracker.pageErrors],
    ui_result: options.uiResult,
    status,
    ...(cause ? { root_cause: cause } : {}),
    ...(blockedBy ? { blocked_by: blockedBy } : {}),
    ...(options.commit ? { commit: options.commit } : {}),
    evidence_file: evidenceRelative.split(path.sep).join("/"),
  };
  atomicWriteJSON(path.join(runRoot, evidenceRelative), attempt);
  capability.attempts.push(attempt);
  capability.status = status;
  if (cause) capability.root_cause = cause;
  else delete capability.root_cause;
  if (blockedBy) capability.blocked_by = blockedBy;
  else delete capability.blocked_by;
  ledger.source_head = sourceHead;
  ledger.updated_at = attempt.completed_at;
  atomicWriteJSON(ledgerPath, ledger);
}

export async function recordCapabilityDisposition<T>(
  tracker: BrowserEvidenceTracker,
  testInfo: TestInfo,
  options: CapabilityDispositionOptions,
  execute: () => Promise<T>,
): Promise<T> {
  const mark = tracker.mark();
  const startedAt = new Date().toISOString();
  try {
    const result = await test.step(options.capabilityID, execute);
    await new Promise((resolve) => setTimeout(resolve, 100));
    const records = tracker.since(mark);
    assertOperations(records, options.expectedOperations ?? []);
    await recordAttempt(
      options,
      startedAt,
      options.status,
      records,
      tracker,
      options.rootCause,
      options.blockedBy,
    );
    return result;
  } catch (cause) {
    const records = tracker.since(mark);
    const message = cause instanceof Error ? cause.message : String(cause);
    await recordAttempt(options, startedAt, "FAIL", records, tracker, message);
    await testInfo.attach(`${options.capabilityID}-failure.json`, {
      body: Buffer.from(JSON.stringify({ capability_id: options.capabilityID, error: message, api_evidence: records }, null, 2)),
      contentType: "application/json",
    });
    throw cause;
  }
}

export function trackBrowserEvidence(...pages: Page[]): BrowserEvidenceTracker {
  const apiEvidence: BrowserApiEvidence[] = [];
  const consoleErrors: string[] = [];
  const pageErrors: string[] = [];
  for (const page of pages) {
    page.on("response", (response) => {
      const url = new URL(response.url());
      if (!url.pathname.startsWith("/api/v1/")) return;
      const headers = response.headers();
      apiEvidence.push({
        method: response.request().method(),
        path: url.pathname,
        status: response.status(),
        request_id: headers["x-request-id"] ?? "",
        trace_id: headers["x-trace-id"] ?? "",
        idempotent_replay: headers["idempotent-replay"] ?? "",
        failure: "",
      });
    });
    page.on("requestfailed", (request) => {
      const url = new URL(request.url());
      if (!url.pathname.startsWith("/api/v1/")) return;
      apiEvidence.push({
        method: request.method(),
        path: url.pathname,
        status: null,
        request_id: "",
        trace_id: "",
        idempotent_replay: "",
        failure: request.failure()?.errorText ?? "request failed",
      });
    });
    page.on("console", (message) => {
      if (message.type() === "error") consoleErrors.push(message.text());
    });
    page.on("pageerror", (error) => pageErrors.push(error.message));
  }
  return {
    mark: () => apiEvidence.length,
    since: (mark) => apiEvidence.slice(mark),
    consoleErrors,
    pageErrors,
  };
}

export async function proveCapability<T>(
  tracker: BrowserEvidenceTracker,
  testInfo: TestInfo,
  options: CapabilityProofOptions,
  execute: () => Promise<T>,
): Promise<T> {
  const mark = tracker.mark();
  const startedAt = new Date().toISOString();
  try {
    const result = await test.step(options.capabilityID, execute);
    await new Promise((resolve) => setTimeout(resolve, 100));
    const records = tracker.since(mark);
    const definition = readManifest().find((item) => item.capability_id === options.capabilityID);
    assertOperations(records, options.expectedOperations ?? definition?.api_operations ?? []);
    await recordAttempt(options, startedAt, "PASS", records, tracker);
    return result;
  } catch (cause) {
    const records = tracker.since(mark);
    const message = cause instanceof Error ? cause.message : String(cause);
    await recordAttempt(options, startedAt, "FAIL", records, tracker, message);
    await testInfo.attach(`${options.capabilityID}-failure.json`, {
      body: Buffer.from(JSON.stringify({ capability_id: options.capabilityID, error: message, api_evidence: records }, null, 2)),
      contentType: "application/json",
    });
    throw cause;
  }
}

export async function waitForApiResponse(
  page: Page,
  operation: string,
  action: () => Promise<unknown>,
  responseTimeoutMS?: number,
) {
  const response = await waitForApiResponseResult(page, operation, action, responseTimeoutMS);
  expect(response.status(), operation).toBeGreaterThanOrEqual(200);
  expect(response.status(), operation).toBeLessThan(300);
  return response;
}

export async function waitForApiResponseResult(
  page: Page,
  operation: string,
  action: () => Promise<unknown>,
  responseTimeoutMS?: number,
) {
  const expected = operationPattern(operation);
  const responsePromise = page.waitForResponse((response) => {
    const url = new URL(response.url());
    return response.request().method() === expected.method && expected.path.test(url.pathname);
  }, responseTimeoutMS === undefined ? undefined : { timeout: responseTimeoutMS });
  const [, response] = await Promise.all([action(), responsePromise]);
  return response;
}
