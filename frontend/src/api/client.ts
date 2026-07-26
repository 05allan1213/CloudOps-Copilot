import axios, { AxiosError, type AxiosRequestConfig } from "axios";

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

export async function getApiData<T>(
  url: string,
  config: AxiosRequestConfig = {},
): Promise<T> {
  return getJSON<T>(url, config);
}

export async function getJSON<T>(
  url: string,
  config: AxiosRequestConfig = {},
): Promise<T> {
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
    return (await httpClient.post<T>(url, body, config)).data;
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
    const response = await httpClient.post<T>(url, body, config);
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

export function apiURL(path: string): string {
  const base = apiBaseUrl.replace(/\/$/, "");
  return `${base}${path}`;
}

function responseHeader(headers: unknown, name: string): string {
  if (!headers || typeof headers !== "object") return "";
  const maybeHeaders = headers as { get?: (key: string) => unknown; [key: string]: unknown };
  const value = typeof maybeHeaders.get === "function" ? maybeHeaders.get(name) : maybeHeaders[name];
  return typeof value === "string" ? value : "";
}
