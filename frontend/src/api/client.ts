import axios, { AxiosError, type AxiosRequestConfig } from "axios";

import {
  invalidateQueryDomain,
  queryCache,
  queryIdentityFor,
  stableQueryIdentity,
  type QueryCacheLoadResult,
  type QueryCachePolicyName,
} from "../composables/queryCache";
import type { ProblemDetails } from "../types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

const httpClient = axios.create({
  baseURL: apiBaseUrl,
  timeout: 10000,
  headers: { Accept: "application/json" },
});

export class ApiError extends Error {
  readonly status: number | null;
  readonly code: string;
  readonly requestID: string;
  readonly traceID: string;
  readonly idempotentReplay: boolean;
  readonly nextSteps: readonly string[];

  constructor(
    message: string,
    status: number | null = null,
    code = "",
    requestID = "",
    traceID = "",
    idempotentReplay = false,
    nextSteps: readonly string[] = [],
  ) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestID = requestID;
    this.traceID = traceID;
    this.idempotentReplay = idempotentReplay;
    this.nextSteps = nextSteps;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

export interface ApiErrorDetails {
  message: string;
  status: number | null;
  code: string;
  requestID: string;
  traceID: string;
  idempotentReplay: boolean | null;
  nextSteps: readonly string[];
}

export interface ApiReadCacheOptions {
  domain?: string;
  policy?: QueryCachePolicyName;
  force?: boolean;
  staleWhileRevalidate?: boolean;
  pinned?: boolean;
}

export interface ApiReadConfig extends AxiosRequestConfig {
  signal?: AbortSignal;
  cache?: false | ApiReadCacheOptions;
}

export function apiErrorDetails(error: unknown, fallback: string): ApiErrorDetails {
  if (isApiError(error)) {
    return {
      message: error.message,
      status: error.status,
      code: error.code,
      requestID: error.requestID,
      traceID: error.traceID,
      idempotentReplay: error.idempotentReplay,
      nextSteps: error.nextSteps,
    };
  }
  return {
    message: error instanceof Error && error.message ? error.message : fallback,
    status: null,
    code: "REQUEST_FAILED",
    requestID: "",
    traceID: "",
    idempotentReplay: null,
    nextSteps: [],
  };
}

export async function getApiData<T>(
  url: string,
  config: ApiReadConfig = {},
): Promise<T> {
  return getJSON<T>(url, config);
}

export async function getJSON<T>(
  url: string,
  config: ApiReadConfig = {},
): Promise<T> {
  return (await getJSONWithCache<T>(url, config)).data;
}

export async function getJSONWithCache<T>(
  url: string,
  config: ApiReadConfig = {},
): Promise<QueryCacheLoadResult<T>> {
  const { cache = {}, signal, ...requestConfig } = config;
  if (cache === false || requestConfig.adapter || !url.startsWith("/api/v1/")) {
    const data = await rawGetJSON<T>(url, { ...requestConfig, signal });
    const identity = queryIdentityFor(apiDomain(url), { url, params: requestConfig.params });
    const now = Date.now();
    return {
      data,
      source: "network",
      revalidating: false,
      entry: {
        key: stableQueryIdentity(identity),
        identity,
        policy: "realtime",
        data,
        updatedAt: now,
        freshUntil: now,
        staleUntil: now,
        staleReason: "",
        requestIdentity: "uncached-read",
        pinned: false,
        lastAccessedAt: now,
      },
    };
  }
  const options = cache satisfies ApiReadCacheOptions;
  const domain = options.domain || apiDomain(url);
  const policy = options.policy || apiPolicy(url);
  return queryCache.load<T>(
    queryIdentityFor(domain, { url, params: requestConfig.params }),
    policy,
    (requestSignal) => rawGetJSON<T>(url, { ...requestConfig, signal: requestSignal }),
    {
      signal: signal ?? undefined,
      force: options.force,
      staleWhileRevalidate: options.staleWhileRevalidate,
      pinned: options.pinned,
    },
  );
}

async function rawGetJSON<T>(url: string, config: AxiosRequestConfig): Promise<T> {
  try {
    return (await httpClient.get<T>(url, config)).data;
  } catch (err) {
    if (axios.isAxiosError(err)) throw normalizeAxiosError(err);
    throw err;
  }
}

export async function postJSON<T, TBody = unknown>(
  url: string,
  body?: TBody,
  config: AxiosRequestConfig = {},
): Promise<T> {
  try {
    assertOnlineWriteAllowed();
    const data = (await httpClient.post<T>(url, body, config)).data;
    invalidateAfterMutation(url);
    return data;
  } catch (err) {
    if (axios.isAxiosError(err)) throw normalizeAxiosError(err);
    throw err;
  }
}

export async function patchJSON<T, TBody = unknown>(
  url: string,
  body?: TBody,
  config: AxiosRequestConfig = {},
): Promise<T> {
  try {
    assertOnlineWriteAllowed();
    const data = (await httpClient.patch<T>(url, body, config)).data;
    invalidateAfterMutation(url);
    return data;
  } catch (err) {
    if (axios.isAxiosError(err)) throw normalizeAxiosError(err);
    throw err;
  }
}

export async function deleteJSON<T = void>(
  url: string,
  config: AxiosRequestConfig = {},
): Promise<T> {
  try {
    assertOnlineWriteAllowed();
    const data = (await httpClient.delete<T>(url, config)).data;
    invalidateAfterMutation(url);
    return data;
  } catch (err) {
    if (axios.isAxiosError(err)) throw normalizeAxiosError(err);
    throw err;
  }
}

export interface JSONResponse<T> {
  data: T;
  status: number;
  requestID: string;
  traceID: string;
  idempotentReplay: boolean;
}

export async function postJSONWithMeta<T, TBody = unknown>(
  url: string,
  body?: TBody,
  config: AxiosRequestConfig = {},
): Promise<JSONResponse<T>> {
  try {
    assertOnlineWriteAllowed();
    const response = await httpClient.post<T>(url, body, config);
    invalidateAfterMutation(url);
    return {
      data: response.data,
      status: response.status,
      requestID: responseHeader(response.headers, "x-request-id"),
      traceID: responseHeader(response.headers, "x-trace-id"),
      idempotentReplay: responseHeader(response.headers, "idempotent-replay") === "true",
    };
  } catch (err) {
    if (axios.isAxiosError(err)) throw normalizeAxiosError(err);
    throw err;
  }
}

function normalizeAxiosError(err: AxiosError<ProblemDetails>): ApiError {
  const problem = err.response?.data;
  const requestID = err.response ? responseHeader(err.response.headers, "x-request-id") : "";
  const traceID = err.response ? responseHeader(err.response.headers, "x-trace-id") : "";
  const replayed = err.response ? responseHeader(err.response.headers, "idempotent-replay") === "true" : false;
  if (problem && typeof problem === "object" && typeof problem.detail === "string") {
    return new ApiError(
      problem.detail,
      err.response?.status ?? problem.status ?? null,
      problem.code,
      problem.request_id || requestID,
      problem.trace_id || traceID,
      replayed,
      Array.isArray(problem.next_steps) ? problem.next_steps.filter((step): step is string => typeof step === "string") : [],
    );
  }

  if (err.response) {
    return new ApiError(`Request failed with status ${err.response.status}`, err.response.status, "", requestID, traceID, replayed);
  }

  if (err.code === AxiosError.ETIMEDOUT || err.code === "ECONNABORTED") {
    return new ApiError("Request timed out");
  }

  return new ApiError(err.message || "Request failed");
}

function assertOnlineWriteAllowed() {
  if (typeof navigator === "undefined" || navigator.onLine !== false) return;
  throw new ApiError(
    "浏览器当前离线；写操作保持阻止，恢复网络后请重新确认服务端状态。",
    null,
    "OFFLINE_WRITE_BLOCKED",
    "",
    "",
    false,
    ["恢复网络连接", "重新读取当前 Scope 与服务端状态", "再次确认目标后手动提交"],
  );
}

export function apiURL(path: string): string {
  const base = apiBaseUrl.replace(/\/$/, "");
  return `${base}${path}`;
}

function apiDomain(url: string): string {
  const path = url.split("?", 1)[0] ?? url;
  if (/\/api\/v1\/(bootstrap|overview|settings|storage-status|scopes|configuration-revisions|providers|secrets)/.test(path)) return "platform";
  if (/\/api\/v1\/(topology|resources)/.test(path)) return "infrastructure";
  if (/\/api\/v1\/monitoring/.test(path)) return "monitoring";
  if (/\/api\/v1\/logs/.test(path)) return "logs";
  if (/\/api\/v1\/traces/.test(path)) return "traces";
  if (/\/api\/v1\/alerts/.test(path)) return "alerts";
  if (/\/api\/v1\/incidents/.test(path)) return "incidents";
  if (/\/api\/v1\/(agent|knowledge-items)/.test(path)) return "agent";
  if (/\/api\/v1\/(devops|operation-plans)/.test(path)) return "devops";
  if (/\/api\/v1\/notifications/.test(path)) return "notifications";
  return "api";
}

function apiPolicy(url: string): QueryCachePolicyName {
  const domain = apiDomain(url);
  if (/[?&](from|to)=/.test(url) || /\/(?:history|queries)(?:\?|$)/.test(url)) return "history";
  if (domain === "notifications") return "realtime";
  if (["monitoring", "logs", "traces", "alerts"].includes(domain)) return "telemetry";
  if (domain === "platform") return "metadata";
  return "operational";
}

function invalidateAfterMutation(url: string) {
  const domain = apiDomain(url);
  if (domain === "platform") {
    queryCache.invalidate(() => true, "mutation");
    return;
  }
  invalidateQueryDomain([domain, `${domain}-workspace`, "platform", "notifications"], "mutation");
}

function responseHeader(headers: unknown, name: string): string {
  if (!headers || typeof headers !== "object") return "";
  const maybeHeaders = headers as { get?: (key: string) => unknown; [key: string]: unknown };
  const value = typeof maybeHeaders.get === "function" ? maybeHeaders.get(name) : maybeHeaders[name];
  return typeof value === "string" ? value : "";
}
