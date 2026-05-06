import { deleteApiData, getApiData, postApiData, putApiData } from "./client";
import type { NotificationChannel, NotificationChannelTestResult } from "../types";

export interface NotificationChannelRequest {
  name: string;
  type: string;
  url: string;
  enabled: boolean;
}

export async function fetchNotificationChannels(): Promise<NotificationChannel[]> {
  return (await getApiData<NotificationChannel[]>("/api/v1/channels")) ?? [];
}

export async function createNotificationChannel(
  request: NotificationChannelRequest,
): Promise<NotificationChannel> {
  return await postApiData<NotificationChannel, NotificationChannelRequest>("/api/v1/channels", request);
}

export async function updateNotificationChannel(
  id: number,
  request: NotificationChannelRequest,
): Promise<NotificationChannel> {
  return await putApiData<NotificationChannel, NotificationChannelRequest>(`/api/v1/channels/${id}`, request);
}

export async function deleteNotificationChannel(id: number): Promise<void> {
  await deleteApiData(`/api/v1/channels/${id}`);
}

export async function testNotificationChannel(id: number): Promise<NotificationChannelTestResult> {
  return await postApiData<NotificationChannelTestResult>(`/api/v1/channels/${id}/test`);
}
