import { deleteApiData, getApiData, postApiData } from "./client";
import type {
  CopilotChatRequest,
  CopilotChatResponse,
  CopilotMessage,
  CopilotSession,
} from "../types";

export async function sendCopilotMessage(
  body: CopilotChatRequest,
): Promise<CopilotChatResponse> {
  return postApiData<CopilotChatResponse, CopilotChatRequest>(
    "/api/v1/copilot/chat",
    body,
  );
}

export async function listCopilotSessions(): Promise<CopilotSession[]> {
  return (await getApiData<CopilotSession[]>("/api/v1/copilot/sessions")) ?? [];
}

export async function listCopilotMessages(
  sessionId: string,
): Promise<CopilotMessage[]> {
  return (
    (await getApiData<CopilotMessage[]>(
      `/api/v1/copilot/sessions/${encodeURIComponent(sessionId)}/messages`,
    )) ?? []
  );
}

export async function deleteCopilotSession(sessionId: string): Promise<void> {
  await deleteApiData(`/api/v1/copilot/sessions/${encodeURIComponent(sessionId)}`);
}
