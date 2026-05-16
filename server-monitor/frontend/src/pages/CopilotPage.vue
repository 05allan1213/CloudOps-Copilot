<script setup lang="ts">
import { computed, onMounted, ref } from "vue";

import {
  deleteCopilotSession,
  listCopilotMessages,
  listCopilotSessions,
  streamCopilotMessage,
} from "../api/copilot";
import SessionList from "../components/copilot/SessionList.vue";
import ChatPanel from "../components/copilot/ChatPanel.vue";
import type {
  CopilotChatResponse,
  CopilotMessage,
  CopilotSuggestion,
  CopilotSession,
  CopilotToolCall,
} from "../types";

type LocalMessage = CopilotMessage & {
  intent?: string;
  confidence?: number;
  tool_calls?: CopilotToolCall[];
  suggestions?: CopilotSuggestion[];
};

const sessions = ref<CopilotSession[]>([]);
const messages = ref<LocalMessage[]>([]);
const activeSessionId = ref("");
const loadingSessions = ref(false);
const loadingMessages = ref(false);
const sending = ref(false);
const error = ref("");

const activeSession = computed(() =>
  sessions.value.find((session) => session.id === activeSessionId.value),
);

onMounted(() => {
  refreshSessions();
});

async function refreshSessions() {
  loadingSessions.value = true;
  error.value = "";
  try {
    sessions.value = await listCopilotSessions();
    if (!activeSessionId.value && sessions.value.length > 0) {
      await selectSession(sessions.value[0].id);
    }
  } catch (err) {
    error.value = normalizeError(err);
  } finally {
    loadingSessions.value = false;
  }
}

async function selectSession(sessionId: string) {
  activeSessionId.value = sessionId;
  loadingMessages.value = true;
  error.value = "";
  try {
    messages.value = await listCopilotMessages(sessionId);
  } catch (err) {
    error.value = normalizeError(err);
  } finally {
    loadingMessages.value = false;
  }
}

function startNewSession() {
  activeSessionId.value = "";
  messages.value = [];
  error.value = "";
}

async function removeSession(sessionId: string) {
  error.value = "";
  try {
    await deleteCopilotSession(sessionId);
    sessions.value = sessions.value.filter((session) => session.id !== sessionId);
    if (activeSessionId.value === sessionId) {
      startNewSession();
    }
  } catch (err) {
    error.value = normalizeError(err);
  }
}

async function submitMessage(content: string) {
  if (!content || sending.value) return;

  const sessionId = activeSessionId.value;
  sending.value = true;
  error.value = "";
  messages.value.push({
    role: "user",
    content,
    created_at: new Date().toISOString(),
  });

  try {
    const response = await streamCopilotMessage({
      message: content,
      session_id: sessionId || undefined,
    });
    activeSessionId.value = response.session_id;
    messages.value.push(toAssistantMessage(response));
    await refreshSessions();
  } catch (err) {
    messages.value.push({
      role: "assistant",
      content: normalizeError(err),
      created_at: new Date().toISOString(),
      intent: "error",
      confidence: 0,
      tool_calls: [],
      suggestions: [],
    });
  } finally {
    sending.value = false;
  }
}

function applySuggestion(suggestion: CopilotSuggestion) {
  const content = (suggestion.action || suggestion.text).trim();
  submitMessage(content);
}

function toAssistantMessage(response: CopilotChatResponse): LocalMessage {
  return {
    role: "assistant",
    content: response.reply,
    created_at: new Date().toISOString(),
    intent: response.intent,
    confidence: response.confidence,
    tool_calls: response.tool_calls,
    suggestions: response.suggestions,
  };
}

function normalizeError(err: unknown): string {
  if (err instanceof Error) return err.message;
  return "请求失败";
}
</script>

<template>
  <div class="copilot-shell">
    <SessionList
      :sessions="sessions"
      :active-session-id="activeSessionId"
      :loading-sessions="loadingSessions"
      @select="selectSession"
      @new="startNewSession"
      @delete="removeSession"
    />

    <ChatPanel
      :messages="messages"
      :active-session="activeSession"
      :loading-messages="loadingMessages"
      :sending="sending"
      :error="error"
      @send="submitMessage"
      @refresh="refreshSessions"
      @apply-suggestion="applySuggestion"
    />
  </div>
</template>

<style scoped>
.copilot-shell {
  display: grid;
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr);
  gap: 16px;
  min-height: calc(100vh - 180px);
}

@media (max-width: 860px) {
  .copilot-shell {
    grid-template-columns: 1fr;
  }
}
</style>
