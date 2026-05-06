import { getApiResponse } from "./client";
import type { ApiResponse, HealthStatus, ReadyStatus } from "../types";

export async function fetchHealthz(): Promise<ApiResponse<HealthStatus>> {
  return await getApiResponse<HealthStatus>("/healthz");
}

export async function fetchReadyz(): Promise<ApiResponse<ReadyStatus>> {
  return await getApiResponse<ReadyStatus>("/readyz");
}
