import { getApiData, postApiData } from "./client";
import type { DiagnosisListResponse, DiagnosisReport, DiagnosisRequest } from "../types";

export interface DiagnosisQuery {
  status?: string;
  trigger_type?: string;
  page?: number;
  page_size?: number;
}

export async function createDiagnosis(request: DiagnosisRequest): Promise<DiagnosisReport> {
  return postApiData<DiagnosisReport, DiagnosisRequest>("/api/v1/diagnosis", request);
}

export async function fetchDiagnosisList(
  query: DiagnosisQuery = {},
): Promise<DiagnosisListResponse> {
  const params: Record<string, string> = {};
  Object.entries(query).forEach(([key, value]) => {
    if (value !== undefined && value !== "") {
      params[key] = String(value);
    }
  });

  return getApiData<DiagnosisListResponse>("/api/v1/diagnosis", { params });
}

export async function fetchDiagnosis(id: number | string): Promise<DiagnosisReport> {
  return getApiData<DiagnosisReport>(`/api/v1/diagnosis/${id}`);
}

export interface FeedbackRequest {
  rating: "useful" | "not_useful";
  comment?: string;
}

export interface FeedbackResponse {
  id: number;
  diagnosis_id: number;
  rating: string;
  comment?: string;
  created_by: number;
  created_at: string;
}

export async function submitDiagnosisFeedback(
  id: number | string,
  request: FeedbackRequest,
): Promise<FeedbackResponse> {
  return postApiData<FeedbackResponse, FeedbackRequest>(
    `/api/v1/diagnosis/${id}/feedback`,
    request,
  );
}
