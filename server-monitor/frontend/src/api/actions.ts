import { getApiData, postApiData } from "./client";
import type {
  CreatePendingActionResult,
  PendingAction,
  PendingActionListResponse,
} from "../types";

export interface ActionListParams {
  status?: string;
  risk_level?: string;
  action_type?: string;
  page?: number;
  page_size?: number;
}

export function listActions(params: ActionListParams = {}) {
  return getApiData<PendingActionListResponse>("/api/v1/actions", { params });
}

export function listPendingActions() {
  return getApiData<PendingActionListResponse>("/api/v1/actions/pending");
}

export function getAction(id: number) {
  return getApiData<PendingAction>(`/api/v1/actions/${id}`);
}

export function createActionsFromDiagnosis(
  diagnosisID: number,
  selectedActionTypes: string[] = [],
) {
  return postApiData<CreatePendingActionResult>(`/api/v1/diagnosis/${diagnosisID}/actions`, {
    source: "diagnosis",
    selected_action_types: selectedActionTypes,
  });
}

export function approveAction(id: number, comment: string) {
  return postApiData<PendingAction>(`/api/v1/actions/${id}/approve`, { comment });
}

export function rejectAction(id: number, reason: string) {
  return postApiData<PendingAction>(`/api/v1/actions/${id}/reject`, { reason });
}

export function executeAction(id: number) {
  return postApiData<PendingAction>(`/api/v1/actions/${id}/execute`, { confirm: true });
}
