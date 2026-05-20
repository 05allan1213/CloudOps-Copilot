<script setup lang="ts">
import { computed } from "vue";
import { RouterLink } from "vue-router";

import ToolCallDisplay from "./ToolCallDisplay.vue";
import type { CopilotLocalMessage, CopilotSuggestion } from "../../types";
import { formatTimeShort } from "../../utils/format";
import { renderMarkdown } from "../../utils/markdown";

const props = defineProps<{
  message: CopilotLocalMessage;
}>();

const emit = defineEmits<{
  applySuggestion: [suggestion: CopilotSuggestion];
}>();

function formatConfidence(value: number | undefined): string {
  if (value === undefined) return "--";
  return `${Math.round(value * 100)}%`;
}

function diagnosisReportId(message: CopilotLocalMessage): number | null {
  const result = message.tool_calls?.find(
    (tool) => tool.name === "diagnosis.trigger" && tool.status === "success",
  )?.result;
  if (result && typeof result === "object" && "id" in result) {
    const id = Number((result as { id: unknown }).id);
    return Number.isFinite(id) ? id : null;
  }
  return null;
}

const renderedContent = computed(() => {
  if (props.message.role === "user") return "";
  return renderMarkdown(props.message.content);
});
</script>

<template>
  <div
    class="message-row"
    :class="message.role === 'user' ? 'from-user' : 'from-assistant'"
  >
    <div class="message-bubble">
      <div class="message-meta">
        <span class="message-author">{{ message.role === 'user' ? "你" : "Copilot" }}</span>
        <time class="message-time">{{ formatTimeShort(message.created_at) }}</time>
      </div>

      <p v-if="message.role === 'user'">
        {{ message.content }}
      </p>
      <!-- eslint-disable vue/no-v-html -->
      <div
        v-else
        class="markdown-body"
        v-html="renderedContent"
      />
      <!-- eslint-enable vue/no-v-html -->

      <div
        v-if="message.intent"
        class="intent-row"
      >
        <span>{{ message.intent }}</span>
        <span>{{ formatConfidence(message.confidence) }}</span>
      </div>

      <ToolCallDisplay
        v-if="message.tool_calls?.length"
        :tool-calls="message.tool_calls"
      />

      <el-button
        v-if="diagnosisReportId(message)"
        type="primary"
        link
        size="small"
        class="diagnosis-link"
      >
        <RouterLink
          :to="`/diagnosis/${diagnosisReportId(message)}`"
          class="link-text"
        >
          查看诊断报告 #{{ diagnosisReportId(message) }}
        </RouterLink>
      </el-button>

      <div
        v-if="message.suggestions?.length"
        class="suggestion-row"
      >
        <el-button
          v-for="suggestion in message.suggestions"
          :key="suggestion.action || suggestion.text"
          size="small"
          round
          @click="emit('applySuggestion', suggestion)"
        >
          {{ suggestion.text }}
        </el-button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.message-row {
  display: flex;
  margin-bottom: 24px;
}

.from-user {
  justify-content: flex-end;
}

.from-assistant {
  justify-content: flex-start;
}

.message-bubble {
  max-width: min(768px, 90%);
  min-width: 0;
  padding: 0;
  background: transparent;
  border: none;
}

.from-user .message-bubble {
  background: #ffffff;
  border: 1px solid #e8e6e1;
  border-radius: 16px 16px 4px 16px;
  padding: 12px 16px;
}

.from-assistant .message-bubble {
  background: transparent;
  border: none;
  padding: 0;
}

.message-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}

.from-user .message-meta {
  display: none;
}

.message-author {
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 600;
}

.message-time {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}

.message-bubble p {
  color: var(--el-text-color-primary);
  font-size: 15px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  margin: 0;
}

.markdown-body {
  color: var(--el-text-color-primary);
  font-size: 15px;
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.markdown-body :deep(p) {
  margin: 0 0 10px;
}

.markdown-body :deep(p:last-child) {
  margin-bottom: 0;
}

.markdown-body :deep(code) {
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
  font-size: 13px;
  background: var(--el-fill-color);
  padding: 2px 6px;
  border-radius: 4px;
}

.markdown-body :deep(pre) {
  margin: 10px 0;
  padding: 14px;
  background: var(--el-fill-color);
  border-radius: var(--cloudops-radius-sm);
  overflow-x: auto;
}

.markdown-body :deep(pre code) {
  background: none;
  padding: 0;
  font-size: 13px;
  line-height: 1.5;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  margin: 6px 0;
  padding-left: 22px;
}

.markdown-body :deep(li) {
  margin-bottom: 6px;
}

.markdown-body :deep(strong) {
  font-weight: 600;
}

.markdown-body :deep(blockquote) {
  margin: 10px 0;
  padding: 6px 14px;
  border-left: 3px solid var(--cloudops-border-color);
  color: var(--el-text-color-secondary);
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3) {
  margin: 14px 0 8px;
  font-weight: 600;
}

.markdown-body :deep(h1) { font-size: 18px; }
.markdown-body :deep(h2) { font-size: 16px; }
.markdown-body :deep(h3) { font-size: 15px; }

.markdown-body :deep(table) {
  width: 100%;
  border-collapse: collapse;
  margin: 10px 0;
  font-size: 13px;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--cloudops-border-color);
  padding: 6px 10px;
  text-align: left;
}

.markdown-body :deep(th) {
  background: var(--el-fill-color);
  font-weight: 600;
}

.intent-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 10px;
  color: var(--el-color-info);
  background: var(--el-color-info-light-9);
  border-radius: var(--cloudops-radius-sm);
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
  margin-top: 12px;
}

@media (max-width: 860px) {
  .message-bubble {
    max-width: 94%;
  }
}
</style>
