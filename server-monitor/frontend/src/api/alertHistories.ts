import { getApiData } from "./client";
import type { AlertHistoryListResponse } from "../types";

export interface AlertHistoryQuery {
  status?: string;
  severity?: string;
  alert_name?: string;
  instance?: string;
  group?: number;
  page?: number;
  page_size?: number;
}

export async function fetchAlertHistories(
  query: AlertHistoryQuery = {},
): Promise<AlertHistoryListResponse> {
  const params: Record<string, string> = {};
  Object.entries(query).forEach(([key, value]) => {
    if (value !== undefined && value !== "" && value !== 0) {
      params[key] = String(value);
    }
  });

  return (
    (await getApiData<AlertHistoryListResponse>("/api/v1/alert-histories", { params })) ?? {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    }
  );
}
