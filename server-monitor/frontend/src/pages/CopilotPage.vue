<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";

import {
  deleteCopilotSession,
  listCopilotMessages,
  listCopilotSessions,
  sendCopilotMessage,
} from "../api/copilot";
import type {
  CopilotChatResponse,
  CopilotMessage,
  CopilotSession,
  CopilotToolCall,
} from "../types";

type LocalMessage = CopilotMessage & {
  intent?: string;
  confidence?: number;
  tool_calls?: CopilotToolCall[];
  suggestions?: string[];
};

const sessions = ref<CopilotSession[]>([]);
const messages = ref<LocalMessage[]>([]);
const activeSessionId = ref("");
const draft = ref("");
const loadingSessions = ref(false);
const loadingMessages = ref(false);
const sending = ref(false);
const error = ref("");
const messagesEl = ref<HTMLElement | null>(null);

const messageLength = computed(() => [...draft.value.trim()].length);
const canSend = computed(
  () => messageLength.value > 0 && messageLength.value <= 2000 && !sending.value,
);
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
    await scrollToBottom();
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

async function submitMessage() {
  const content = draft.value.trim();
  if (!canSend.value || content === "") {
    return;
  }

  const sessionId = activeSessionId.value;
  draft.value = "";
  sending.value = true;
  error.value = "";
  messages.value.push({
    role: "user",
    content,
    created_at: new Date().toISOString(),
  });
  await scrollToBottom();

  try {
    const response = await sendCopilotMessage({
      message: content,
      session_id: sessionId || undefined,
    });
    activeSessionId.value = response.session_id;
    messages.value.push(toAssistantMessage(response));
    await refreshSessions();
    await scrollToBottom();
  } catch (err) {
    error.value = normalizeError(err);
    messages.value.push({
      role: "assistant",
      content: error.value,
      created_at: new Date().toISOString(),
      intent: "error",
      confidence: 0,
      tool_calls: [],
      suggestions: [],
    });
    await scrollToBottom();
  } finally {
    sending.value = false;
  }
}

function onDraftKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    submitMessage();
  }
}

function applySuggestion(value: string) {
  draft.value = value;
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

async function scrollToBottom() {
  await nextTick();
  if (messagesEl.value) {
    messagesEl.value.scrollTop = messagesEl.value.scrollHeight;
  }
}

function formatTime(value: string): string {
  if (!value) {
    return "--";
  }
  return new Date(value).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatConfidence(value: number | undefined): string {
  if (value === undefined) {
    return "--";
  }
  return `${Math.round(value * 100)}%`;
}

function toolResultPreview(value: unknown): string {
  if (value === undefined || value === null) {
    return "";
  }
  const text = JSON.stringify(value, null, 2);
  return text.length > 420 ? `${text.slice(0, 420)}...` : text;
}

function normalizeError(err: unknown): string {
  if (err instanceof Error) {
    return err.message;
  }
  return "请求失败";
}
</script>

<template>
  <section class="copilot-shell">
    <aside class="session-panel">
      <div class="session-panel-header">
        <div>
          <h2>Copilot</h2>
          <span>{{ loadingSessions ? "同步中" : `${sessions.length} 个会话` }}</span>
        </div>
        <button type="button" class="icon-btn" title="新建会话" @click="startNewSession">
          +
        </button>
      </div>

      <div class="session-list">
        <button
          v-for="session in sessions"
          :key="session.id"
          type="button"
          class="session-item"
          :class="{ active: session.id === activeSessionId }"
          @click="selectSession(session.id)"
        >
          <span>{{ session.title || session.id }}</span>
          <small>{{ formatTime(session.updated_at) }}</small>
        </button>
        <div v-if="!loadingSessions && sessions.length === 0" class="empty-block">
          暂无会话
        </div>
      </div>

      <button
        v-if="activeSessionId"
        type="button"
        class="delete-session"
        @click="removeSession(activeSessionId)"
      >
        删除当前会话
      </button>
    </aside>

    <main class="chat-panel">
      <header class="chat-header">
        <div>
          <h2>{{ activeSession?.title || "新会话" }}</h2>
          <p>{{ activeSessionId || "发送第一条消息后创建" }}</p>
        </div>
        <button type="button" class="refresh-btn" :disabled="loadingMessages" @click="refreshSessions">
          刷新
        </button>
      </header>

      <div v-if="error" class="copilot-error">{{ error }}</div>

      <section ref="messagesEl" class="message-list">
        <div v-if="loadingMessages" class="empty-block">加载消息中</div>
        <div v-else-if="messages.length === 0" class="empty-block">
          发送“当前有哪些告警？”开始查询
        </div>

        <article
          v-for="(message, index) in messages"
          :key="`${message.created_at}-${index}`"
          class="message-row"
          :class="message.role === 'user' ? 'from-user' : 'from-assistant'"
        >
          <div class="message-bubble">
            <div class="message-meta">
              <span>{{ message.role === "user" ? "你" : "Copilot" }}</span>
              <time>{{ formatTime(message.created_at) }}</time>
            </div>
            <p>{{ message.content }}</p>

            <div v-if="message.intent" class="intent-row">
              <span>{{ message.intent }}</span>
              <span>{{ formatConfidence(message.confidence) }}</span>
            </div>

            <div v-if="message.tool_calls?.length" class="tool-list">
              <details
                v-for="tool in message.tool_calls"
                :key="`${tool.name}-${tool.status}`"
                class="tool-item"
                :class="tool.status"
              >
                <summary>
                  <span>{{ tool.name }}</span>
                  <strong>{{ tool.status }}</strong>
                </summary>
                <pre v-if="tool.error">{{ tool.error }}</pre>
                <pre v-else>{{ toolResultPreview(tool.result) }}</pre>
              </details>
            </div>

            <div v-if="message.suggestions?.length" class="suggestion-row">
              <button
                v-for="suggestion in message.suggestions"
                :key="suggestion"
                type="button"
                @click="applySuggestion(suggestion)"
              >
                {{ suggestion }}
              </button>
            </div>
          </div>
        </article>
      </section>

      <footer class="composer">
        <textarea
          v-model="draft"
          maxlength="2000"
          rows="3"
          placeholder="输入运维查询"
          :disabled="sending"
          @keydown="onDraftKeydown"
        ></textarea>
        <div class="composer-actions">
          <span :class="{ over: messageLength > 2000 }">{{ messageLength }}/2000</span>
          <button type="button" :disabled="!canSend" @click="submitMessage">
            {{ sending ? "发送中" : "发送" }}
          </button>
        </div>
      </footer>
    </main>
  </section>
</template>

<style scoped>
.copilot-shell {
  display: grid;
  grid-template-columns: minmax(220px, 280px) minmax(0, 1fr);
  gap: 1rem;
  min-height: calc(100vh - 180px);
}

.session-panel,
.chat-panel {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--bg-card);
  min-width: 0;
}

.session-panel {
  display: flex;
  flex-direction: column;
  min-height: 520px;
}

.session-panel-header,
.chat-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem;
  border-bottom: 1px solid var(--border-color);
}

.session-panel-header h2,
.chat-header h2 {
  margin: 0;
  font-size: 1rem;
}

.session-panel-header span,
.chat-header p {
  display: block;
  margin-top: 0.25rem;
  color: var(--text-muted);
  font-size: 0.75rem;
  overflow-wrap: anywhere;
}

.icon-btn {
  width: 32px;
  height: 32px;
  border-radius: var(--radius-sm);
  color: var(--accent);
  background: var(--accent-soft);
  font-size: 1.2rem;
  font-weight: 700;
}

.session-list {
  flex: 1;
  overflow: auto;
  padding: 0.65rem;
}

.session-item {
  display: grid;
  width: 100%;
  gap: 0.25rem;
  padding: 0.7rem;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  text-align: left;
}

.session-item:hover,
.session-item.active {
  color: var(--text-primary);
  background: var(--bg-hover);
}

.session-item span {
  font-size: 0.82rem;
  font-weight: 700;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-item small {
  color: var(--text-muted);
  font-size: 0.7rem;
}

.delete-session,
.refresh-btn {
  color: var(--text-secondary);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.5rem 0.75rem;
  font-size: 0.76rem;
  font-weight: 700;
}

.delete-session {
  margin: 0.65rem;
  color: var(--danger);
}

.refresh-btn:disabled {
  color: var(--text-muted);
  cursor: default;
}

.chat-panel {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto;
  min-height: 520px;
}

.copilot-error {
  margin: 0.75rem 1rem 0;
  color: var(--warning);
  background: var(--warning-soft);
  border: 1px solid rgba(245, 158, 11, 0.24);
  border-radius: var(--radius-sm);
  padding: 0.65rem 0.8rem;
  font-size: 0.8rem;
}

.message-list {
  overflow: auto;
  padding: 1rem;
}

.empty-block {
  color: var(--text-muted);
  border: 1px dashed var(--border-color);
  border-radius: var(--radius-sm);
  padding: 1rem;
  text-align: center;
  font-size: 0.82rem;
}

.message-row {
  display: flex;
  margin-bottom: 0.85rem;
}

.message-row.from-user {
  justify-content: flex-end;
}

.message-bubble {
  max-width: min(760px, 86%);
  min-width: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  padding: 0.85rem;
  background: rgba(15, 23, 42, 0.72);
}

.from-user .message-bubble {
  background: var(--accent-soft);
  border-color: rgba(59, 130, 246, 0.25);
}

.message-meta,
.intent-row,
.tool-item summary,
.composer-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.message-meta {
  color: var(--text-muted);
  font-size: 0.72rem;
  margin-bottom: 0.45rem;
}

.message-meta span {
  color: var(--text-secondary);
  font-weight: 700;
}

.message-bubble p {
  color: var(--text-primary);
  font-size: 0.9rem;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.intent-row {
  margin-top: 0.7rem;
  color: var(--info);
  background: var(--info-soft);
  border-radius: var(--radius-sm);
  padding: 0.45rem 0.6rem;
  font-size: 0.74rem;
  font-weight: 700;
}

.tool-list {
  display: grid;
  gap: 0.45rem;
  margin-top: 0.7rem;
}

.tool-item {
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: rgba(2, 6, 23, 0.35);
  overflow: hidden;
}

.tool-item.success summary strong {
  color: var(--success);
}

.tool-item.error summary strong {
  color: var(--danger);
}

.tool-item summary {
  padding: 0.5rem 0.6rem;
  cursor: pointer;
  color: var(--text-secondary);
  font-size: 0.75rem;
  font-weight: 700;
}

.tool-item pre {
  max-height: 220px;
  overflow: auto;
  border-top: 1px solid var(--border-color);
  color: var(--text-secondary);
  padding: 0.65rem;
  font-size: 0.72rem;
  line-height: 1.45;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.suggestion-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.45rem;
  margin-top: 0.7rem;
}

.suggestion-row button {
  color: var(--accent);
  background: var(--accent-soft);
  border-radius: var(--radius-sm);
  padding: 0.38rem 0.55rem;
  font-size: 0.74rem;
  font-weight: 700;
}

.composer {
  border-top: 1px solid var(--border-color);
  padding: 1rem;
}

.composer textarea {
  width: 100%;
  min-height: 76px;
  resize: vertical;
  color: var(--text-primary);
  background: rgba(2, 6, 23, 0.3);
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  padding: 0.75rem;
  cursor: text;
}

.composer textarea:disabled {
  color: var(--text-muted);
  cursor: default;
}

.composer-actions {
  margin-top: 0.65rem;
}

.composer-actions span {
  color: var(--text-muted);
  font-size: 0.75rem;
}

.composer-actions span.over {
  color: var(--danger);
}

.composer-actions button {
  color: #fff;
  background: var(--accent);
  border-radius: var(--radius-sm);
  padding: 0.55rem 1rem;
  font-size: 0.82rem;
  font-weight: 800;
}

.composer-actions button:disabled {
  color: var(--text-muted);
  background: var(--bg-hover);
  cursor: default;
}

@media (max-width: 860px) {
  .copilot-shell {
    grid-template-columns: 1fr;
  }

  .session-panel,
  .chat-panel {
    min-height: auto;
  }

  .session-list {
    max-height: 220px;
  }

  .message-bubble {
    max-width: 94%;
  }
}
</style>
