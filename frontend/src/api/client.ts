import axios, { AxiosError, type AxiosRequestConfig } from "axios";
import { ElMessage, ElNotification } from "element-plus";

import type { ProblemDetails } from "../types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

const httpClient = axios.create({
  baseURL: apiBaseUrl,
  timeout: 10000,
  withCredentials: true,
  headers: { Accept: "application/json" },
});

export class ApiError extends Error {
  readonly status: number | null;
  readonly code: string;
  readonly requestID: string;
  readonly traceID: string;

  constructor(message: string, status: number | null = null, code = "", requestID = "", traceID = "") {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
    this.requestID = requestID;
    this.traceID = traceID;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

httpClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ProblemDetails>) => {
    if (error.response?.status === 401) {
      ElMessage.warning("GitHub 会话已过期，正在重新认证");
      redirectToOAuth();
    } else if (error.response?.status === 403) {
      ElNotification({
        title: "权限不足",
        message: "您没有权限执行此操作",
        type: "warning",
        duration: 4000,
      });
    } else if (error.response?.status === 500) {
      ElNotification({
        title: "服务器错误",
        message: "服务器内部错误，请稍后重试",
        type: "error",
        duration: 5000,
      });
    } else if (!error.response) {
      ElNotification({
        title: "网络错误",
        message: "网络连接失败，请检查网络设置",
        type: "error",
        duration: 5000,
      });
    }
    return Promise.reject(error);
  },
);

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

function normalizeAxiosError(err: AxiosError<ProblemDetails>): Error {
  const problem = err.response?.data;
  if (problem && typeof problem === "object" && typeof problem.detail === "string") {
    return new ApiError(problem.detail, err.response?.status ?? problem.status ?? null, problem.code, problem.request_id, problem.trace_id);
  }

  if (err.response) {
    return new ApiError(`Request failed with status ${err.response.status}`, err.response.status);
  }

  if (err.code === AxiosError.ETIMEDOUT || err.code === "ECONNABORTED") {
    return new ApiError("Request timed out");
  }

  return new ApiError(err.message || "Request failed");
}

export function redirectToOAuth(): void {
  if (typeof window === "undefined" || window.location.pathname.startsWith("/oauth2/")) return;
  const returnTo = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  window.location.assign(`/oauth2/start?rd=${encodeURIComponent(returnTo)}`);
}
