<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { Refresh } from "@element-plus/icons-vue";

import MessageBubble from "./MessageBubble.vue";
import type { CopilotLocalMessage, CopilotSession, CopilotSuggestion } from "../../types";

const props = defineProps<{
  messages: CopilotLocalMessage[];
  activeSession: CopilotSession | undefined;
  loadingMessages: boolean;
  sending: boolean;
  error: string;
}>();

const emit = defineEmits<{
  send: [content: string];
  refresh: [];
  applySuggestion: [suggestion: CopilotSuggestion];
}>();

const draft = ref("");
const messagesEl = ref<HTMLElement | null>(null);

const messageLength = computed(() => [...draft.value.trim()].length);
const canSend = computed(
  () => messageLength.value > 0 && messageLength.value <= 2000 && !props.sending,
);

function handleSubmit() {
  const content = draft.value.trim();
  if (!canSend.value || content === "") return;
  draft.value = "";
  emit("send", content);
}

function handleDraftKeydown(event: KeyboardEvent) {
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    handleSubmit();
  }
}

async function scrollToBottom() {
  await nextTick();
  if (messagesEl.value) {
    messagesEl.value.scrollTop = messagesEl.value.scrollHeight;
  }
}

watch(
  () => props.messages.length,
  () => scrollToBottom(),
);
</script>

<template>
  <div class="chat-panel">
    <header class="chat-header">
      <div>
        <h2>{{ activeSession?.title || "新会话" }}</h2>
        <p>{{ activeSession ? activeSession.id : "发送第一条消息后创建" }}</p>
      </div>
      <el-button
        :icon="Refresh"
        :loading="loadingMessages"
        size="small"
        @click="emit('refresh')"
      >
        刷新
      </el-button>
    </header>

    <el-alert
      v-if="error"
      :title="error"
      type="warning"
      show-icon
      closable
      class="chat-alert"
    />

    <section
      ref="messagesEl"
      class="message-list"
    >
      <el-skeleton
        v-if="loadingMessages"
        :rows="5"
        animated
      />

      <el-empty
        v-else-if="messages.length === 0"
        description="发送「当前有哪些告警？」开始查询"
        :image-size="64"
      />

      <template v-else>
        <MessageBubble
          v-for="(message, index) in messages"
          :key="`${message.created_at}-${index}`"
          :message="message"
          @apply-suggestion="emit('applySuggestion', $event)"
        />
      </template>
    </section>

    <footer class="composer">
      <el-input
        v-model="draft"
        type="textarea"
        maxlength="2000"
        :rows="3"
        placeholder="输入运维查询"
        :disabled="sending"
        resize="vertical"
        @keydown="handleDraftKeydown"
      />
      <div class="composer-actions">
        <span
          :class="{ over: messageLength > 2000 }"
          class="char-count"
        >{{ messageLength }}/2000</span>
        <el-button
          type="primary"
          :disabled="!canSend"
          :loading="sending"
          @click="handleSubmit"
        >
          {{ sending ? "发送中" : "发送" }}
        </el-button>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.chat-panel {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto;
  min-height: 520px;
  border: 1px solid var(--cloudops-border-color);
  border-radius: var(--cloudops-radius-md);
  background: var(--cloudops-bg-card);
}

.chat-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px;
  border-bottom: 1px solid var(--cloudops-border-color);
}

.chat-header h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.chat-header p {
  margin: 4px 0 0;
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.message-list {
  overflow: auto;
  padding: 16px;
}

.composer {
  border-top: 1px solid var(--cloudops-border-color);
  padding: 16px;
}

.composer-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 8px;
}

.char-count {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

.char-count.over {
  color: var(--el-color-danger);
}

.chat-alert {
  margin: 12px 16px 0;
}

@media (max-width: 860px) {
  .chat-panel {
    min-height: auto;
  }
}
</style>
