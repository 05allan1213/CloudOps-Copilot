import axios, { AxiosError, type AxiosRequestConfig } from "axios";
import { ElMessage, ElNotification } from "element-plus";

import type { ApiResponse } from "../types";
import { clearStoredAuth, getStoredExpiresAt, getStoredToken } from "./authStorage";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

const httpClient = axios.create({
  baseURL: apiBaseUrl,
  timeout: 10000,
});

export class ApiError extends Error {
  readonly status: number | null;

  constructor(message: string, status: number | null = null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

httpClient.interceptors.request.use((config) => {
  const token = getStoredToken();
  if (token) {
    const expiresAt = getStoredExpiresAt();
    if (expiresAt) {
      const expires = new Date(expiresAt).getTime();
      if (Date.now() >= expires) {
        clearStoredAuth();
        if (window.location.pathname !== "/login") {
          window.location.href = "/login";
        }
        return config;
      }
    }
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

httpClient.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse<unknown>>) => {
    if (error.response?.status === 401) {
      clearStoredAuth();
      ElMessage.warning("登录已过期");
      if (window.location.pathname !== "/login") {
        window.location.href = "/login";
      }
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
  try {
    const response = await httpClient.get<ApiResponse<T>>(url, config);
    const payload = response.data;

    if (payload.status !== "success") {
      throw new Error(payload.error ?? "Unknown API error");
    }

    if (payload.data === undefined) {
      throw new Error("API response missing data field");
    }

    return payload.data;
  } catch (err) {
    if (axios.isAxiosError(err)) {
      throw normalizeAxiosError(err);
    }
    throw err;
  }
}

export async function postApiData<T, TBody = unknown>(
  url: string,
  body?: TBody,
  config: AxiosRequestConfig = {},
): Promise<T> {
  try {
    const response = await httpClient.post<ApiResponse<T>>(url, body, config);
    const payload = response.data;

    if (payload.status !== "success") {
      throw new Error(payload.error ?? "Unknown API error");
    }

    if (payload.data === undefined) {
      throw new Error("API response missing data field");
    }

    return payload.data;
  } catch (err) {
    if (axios.isAxiosError(err)) {
      throw normalizeAxiosError(err);
    }
    throw err;
  }
}

export async function putApiData<T, TBody = unknown>(
  url: string,
  body?: TBody,
  config: AxiosRequestConfig = {},
): Promise<T> {
  try {
    const response = await httpClient.put<ApiResponse<T>>(url, body, config);
    const payload = response.data;

    if (payload.status !== "success") {
      throw new Error(payload.error ?? "Unknown API error");
    }

    if (payload.data === undefined) {
      throw new Error("API response missing data field");
    }

    return payload.data;
  } catch (err) {
    if (axios.isAxiosError(err)) {
      throw normalizeAxiosError(err);
    }
    throw err;
  }
}

export async function deleteApiData(
  url: string,
  config: AxiosRequestConfig = {},
): Promise<void> {
  try {
    await httpClient.delete(url, config);
  } catch (err) {
    if (axios.isAxiosError(err)) {
      throw normalizeAxiosError(err);
    }
    throw err;
  }
}

export async function getApiResponse<T>(
  url: string,
  config: AxiosRequestConfig = {},
): Promise<ApiResponse<T>> {
  try {
    const response = await httpClient.get<ApiResponse<T>>(url, {
      ...config,
      validateStatus: (status) => status < 600,
    });

    const payload = response.data;
    if (
      !payload ||
      typeof payload !== "object" ||
      !("status" in payload) ||
      typeof payload.status !== "string"
    ) {
      throw new Error("Invalid API response structure");
    }

    return payload;
  } catch (err) {
    if (axios.isAxiosError(err)) {
      throw normalizeAxiosError(err);
    }
    throw err;
  }
}

function normalizeAxiosError(err: AxiosError<ApiResponse<unknown>>): Error {
  const payloadError = err.response?.data?.error;
  if (payloadError) {
    return new ApiError(payloadError, err.response?.status ?? null);
  }

  if (err.response) {
    return new ApiError(`Request failed with status ${err.response.status}`, err.response.status);
  }

  if (err.code === AxiosError.ETIMEDOUT || err.code === "ECONNABORTED") {
    return new ApiError("Request timed out");
  }

  return new ApiError(err.message || "Request failed");
}
