import { apiURL, getJSON, postJSON } from "./client";

export interface ContextLink {
  workspace: string;
  path: string;
  query: Record<string, string>;
  operational_scope_id: string;
  external: boolean;
}

export interface OwnerNotification {
  id: string;
  source_type: string;
  source_id: string;
  source_state: string;
  severity: "P1" | "P2" | "P3" | "info";
  reason: string;
  context_link: ContextLink;
  read: boolean;
  created_at: string;
}

export interface NotificationPage {
  items: OwnerNotification[];
  next_cursor?: string;
  unread_count: number;
}

export function getNotifications(cursor = "", signal?: AbortSignal): Promise<NotificationPage> {
  return getJSON("/api/v1/notifications", { params: { cursor: cursor || undefined, limit: 50 }, signal });
}

export async function markNotificationRead(id: string): Promise<void> {
  await postJSON(`/api/v1/notifications/${encodeURIComponent(id)}/read`);
}

export function markAllNotificationsRead(cursor = ""): Promise<{ updated: number }> {
  return postJSON("/api/v1/notifications/read-all", { cursor });
}

export function openNotificationStream(
  onNotification: (notification: OwnerNotification) => void,
  onError?: () => void,
  onOpen?: () => void,
): () => void {
  const source = new EventSource(apiURL("/api/v1/notification-events"));
  source.addEventListener("owner_notification.created", (event) => {
    try {
      onNotification(JSON.parse((event as MessageEvent<string>).data) as OwnerNotification);
    } catch {
      onError?.();
    }
  });
  source.onopen = () => onOpen?.();
  source.onerror = () => onError?.();
  return () => source.close();
}
