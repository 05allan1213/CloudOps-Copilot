import { getApiData, postApiData } from "./client";
import type { DiagnosisListResponse, DiagnosisReport, DiagnosisRequest } from "../types";

export interface DiagnosisQuery {
  status?: string;
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
