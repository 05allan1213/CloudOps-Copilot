import { deleteApiData, getApiData, postApiData } from "./client";
import { getStoredToken } from "./authStorage";
import type {
  ApiResponse,
  CopilotChatRequest,
  CopilotChatResponse,
  CopilotMessage,
  CopilotSession,
} from "../types";

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? "";

export async function sendCopilotMessage(
  body: CopilotChatRequest,
): Promise<CopilotChatResponse> {
  return postApiData<CopilotChatResponse, CopilotChatRequest>(
    "/api/v1/copilot/chat",
    body,
  );
}

export async function streamCopilotMessage(
  body: CopilotChatRequest,
  onDelta?: (delta: string) => void,
): Promise<CopilotChatResponse> {
  const headers: Record<string, string> = {
    Accept: "text/event-stream",
    "Content-Type": "application/json",
  };
  const token = getStoredToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(`${apiBaseUrl}/api/v1/copilot/chat`, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw new Error(`Request failed with status ${response.status}`);
  }
  if (!response.body) {
    throw new Error("Stream response missing body");
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let result: CopilotChatResponse | null = null;

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const { value, done } = await reader.read();
    buffer += decoder.decode(value ?? new Uint8Array(), { stream: !done });
    let separatorIndex = buffer.indexOf("\n\n");
    while (separatorIndex >= 0) {
      const block = buffer.slice(0, separatorIndex);
      buffer = buffer.slice(separatorIndex + 2);
      const event = parseSSEBlock(block);
      if (event.name === "reply_delta" && event.data && onDelta) {
        try {
          const parsed = JSON.parse(event.data) as { delta?: string };
          if (parsed.delta) {
            onDelta(parsed.delta);
          }
        } catch {
          // ignore malformed delta
        }
      } else if (event.name === "response" && event.data) {
        const payload = JSON.parse(event.data) as ApiResponse<CopilotChatResponse>;
        if (payload.status !== "success" || !payload.data) {
          throw new Error(payload.error ?? "Stream response failed");
        }
        result = payload.data;
      } else if (event.name === "error" && event.data) {
        try {
          const payload = JSON.parse(event.data) as ApiResponse<unknown>;
          throw new Error(payload.error ?? "Stream response failed");
        } catch (err) {
          if (err instanceof Error) throw err;
          throw new Error("Stream response failed");
        }
      }
      separatorIndex = buffer.indexOf("\n\n");
    }
    if (done) {
      break;
    }
  }

  if (!result) {
    throw new Error("Stream response missing data");
  }
  return result;
}

function parseSSEBlock(block: string): { name: string; data: string } {
  let name = "message";
  const dataLines: string[] = [];
  for (const rawLine of block.split("\n")) {
    const line = rawLine.trimEnd();
    if (line.startsWith("event:")) {
      name = line.slice("event:".length).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice("data:".length).trimStart());
    }
  }
  return { name, data: dataLines.join("\n") };
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
