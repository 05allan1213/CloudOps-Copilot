import { getApiData } from "./client";
import type { AuditLog, AuditLogListResponse } from "../types";

export interface AuditLogListParams {
  action?: string;
  result?: string;
  actor?: string;
  page?: number;
  page_size?: number;
}

export function listAuditLogs(params: AuditLogListParams = {}) {
  return getApiData<AuditLogListResponse>("/api/v1/audit-logs", { params });
}

export function getAuditLog(id: number) {
  return getApiData<AuditLog>(`/api/v1/audit-logs/${id}`);
}
