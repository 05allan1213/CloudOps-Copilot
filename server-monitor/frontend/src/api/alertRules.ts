import { deleteApiData, getApiData, postApiData, putApiData } from "./client";
import type { AlertRule, AlertRuleSyncResult } from "../types";

export interface AlertRuleRequest {
  name: string;
  expr: string;
  duration: string;
  severity: string;
  summary: string;
  description: string;
  enabled: boolean;
}

export async function fetchAlertRules(): Promise<AlertRule[]> {
  return (await getApiData<AlertRule[]>("/api/v1/alert-rules")) ?? [];
}

export async function createAlertRule(request: AlertRuleRequest): Promise<AlertRule> {
  return await postApiData<AlertRule, AlertRuleRequest>("/api/v1/alert-rules", request);
}

export async function updateAlertRule(id: number, request: AlertRuleRequest): Promise<AlertRule> {
  return await putApiData<AlertRule, AlertRuleRequest>(`/api/v1/alert-rules/${id}`, request);
}

export async function deleteAlertRule(id: number): Promise<void> {
  await deleteApiData(`/api/v1/alert-rules/${id}`);
}

export async function syncAlertRules(): Promise<AlertRuleSyncResult> {
  return await postApiData<AlertRuleSyncResult>("/api/v1/alert-rules/sync");
}
