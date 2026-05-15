<script setup lang="ts">
import { RouterLink } from "vue-router";

import ToolCallDisplay from "./ToolCallDisplay.vue";
import type { CopilotToolCall } from "../../types";

type LocalMessage = {
  role: string;
  content: string;
  created_at: string;
  intent?: string;
  confidence?: number;
  tool_calls?: CopilotToolCall[];
  suggestions?: string[];
};

defineProps<{
  message: LocalMessage;
}>();

const emit = defineEmits<{
  applySuggestion: [value: string];
}>();

function formatTime(value: string): string {
  if (!value) return "--";
  return new Date(value).toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatConfidence(value: number | undefined): string {
  if (value === undefined) return "--";
  return `${Math.round(value * 100)}%`;
}

function diagnosisReportId(message: LocalMessage): number | null {
  const result = message.tool_calls?.find(
    (tool) => tool.name === "diagnosis.trigger" && tool.status === "success",
  )?.result;
  if (result && typeof result === "object" && "id" in result) {
    const id = Number((result as { id: unknown }).id);
    return Number.isFinite(id) ? id : null;
  }
  return null;
}
</script>

<template>
  <div class="message-row" :class="message.role === 'user' ? 'from-user' : 'from-assistant'">
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

      <ToolCallDisplay v-if="message.tool_calls?.length" :tool-calls="message.tool_calls" />

      <el-button
        v-if="diagnosisReportId(message)"
        type="primary"
        link
        size="small"
        class="diagnosis-link"
      >
        <RouterLink :to="`/diagnosis/${diagnosisReportId(message)}`" class="link-text">
          查看诊断报告 #{{ diagnosisReportId(message) }}
        </RouterLink>
      </el-button>

      <div v-if="message.suggestions?.length" class="suggestion-row">
        <el-button
          v-for="suggestion in message.suggestions"
          :key="suggestion"
          size="small"
          round
          @click="emit('applySuggestion', suggestion)"
        >
          {{ suggestion }}
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.message-row {
  display: flex;
  margin-bottom: 14px;
}

.from-user {
  justify-content: flex-end;
}

.message-bubble {
  max-width: min(760px, 86%);
  min-width: 0;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: var(--cloudops-radius-md);
  padding: 12px;
  background: var(--el-fill-color-lighter);
}

.from-user .message-bubble {
  background: var(--el-color-primary-light-9);
  border-color: var(--el-color-primary-light-7);
}

.message-meta {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--el-text-color-placeholder);
  font-size: 12px;
  margin-bottom: 8px;
}

.message-meta span {
  color: var(--el-text-color-secondary);
  font-weight: 600;
}

.message-bubble p {
  color: var(--el-text-color-primary);
  font-size: 14px;
  line-height: 1.55;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}

.intent-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 10px;
  color: var(--el-color-info);
  background: var(--el-color-info-light-9);
  border-radius: 4px;
  padding: 6px 10px;
  font-size: 12px;
  font-weight: 600;
}

.diagnosis-link {
  margin-top: 10px;
}

.link-text {
  color: inherit;
  text-decoration: none;
}

.suggestion-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 10px;
}

@media (max-width: 860px) {
  .message-bubble {
    max-width: 94%;
  }
}
</style>
