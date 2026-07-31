<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, nextTick, ref, watch } from "vue";

import type { AgentStep, ConsultationMessage } from "../../api/agent";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

type ConversationRow =
  | { key: string; kind: "message"; role: "owner" | "assistant"; title: string; content: string; time?: string; message?: ConsultationMessage; footer?: string }
  | { key: string; kind: "step"; step: AgentStep };

const props = defineProps<{ compact?: boolean }>();
const store = useAgentWorkspaceStore();
const draft = ref("");
const knowledgeMessageID = ref("");
const knowledgeTitle = ref("");
const messageList = ref<HTMLDivElement | null>(null);
const copiedRowID = ref("");
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium" });

const run = computed(() => store.selectedRun);
const messages = computed(() => store.consultation?.messages ?? []);
const canCancel = computed(() => run.value?.status === "pending" || run.value?.status === "running");
const instanceID = computed(() => props.compact ? "global-agent-conversation" : "agent-conversation");
const heading = computed(() => store.selection === "consultation"
  ? store.consultation?.title || "Consultation"
  : run.value?.objective || "Investigation");
const toolHeadingID = computed(() => `${instanceID.value}-${store.selection}-tools-heading`);
const hasTools = computed(() => Boolean(run.value?.steps.length));
const rows = computed<ConversationRow[]>(() => {
  const selectedRun = run.value;
  if (store.selection === "investigation") {
    if (!selectedRun) return [];
    return [
      { key: `objective-${selectedRun.id}`, kind: "message", role: "owner", title: "Investigation Objective", content: selectedRun.objective, time: selectedRun.created_at },
      ...selectedRun.steps.map((step) => ({ key: `step-${step.id}`, kind: "step" as const, step })),
      ...(selectedRun.answer ? [{ key: `answer-${selectedRun.id}`, kind: "message" as const, role: "assistant" as const, title: "Agent Outcome", content: selectedRun.answer, time: selectedRun.completed_at, footer: `Uncertainty · ${selectedRun.uncertainty}${selectedRun.failure_code ? ` · ${selectedRun.failure_code}` : ""}` }] : []),
    ];
  }
  return [
    ...messages.value.map((message) => ({
      key: `message-${message.id}`,
      kind: "message" as const,
      role: message.role,
      title: message.role === "owner" ? "Owner" : "Agent",
      content: message.content,
      time: message.created_at,
      message,
      footer: message.role === "assistant" ? `${message.evidence_citations.length} Evidence · ${message.guidance_citations.length} Guidance` : undefined,
    })),
    ...(store.liveAnswer ? [{ key: "live-answer", kind: "message" as const, role: "assistant" as const, title: "Agent · Streaming", content: store.liveAnswer }] : []),
    ...(selectedRun?.steps.map((step) => ({ key: `step-${step.id}`, kind: "step" as const, step })) ?? []),
  ];
});
const virtualized = computed(() => rows.value.length > 80);
const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>(computed(() => ({
  count: virtualized.value ? rows.value.length : 0,
  getScrollElement: () => messageList.value,
  estimateSize: (index: number) => rows.value[index]?.kind === "step" ? 58 : 132,
  overscan: 10,
  getItemKey: (index: number) => rows.value[index]?.key ?? index,
})));
const virtualRows = computed(() => virtualizer.value.getVirtualItems().map((item) => ({
  index: item.index,
  start: item.start,
  row: rows.value[item.index],
})));
const renderedRows = computed(() => virtualized.value
  ? virtualRows.value
  : rows.value.map((row, index) => ({ index, start: 0, row })));
const totalSize = computed(() => virtualized.value ? virtualizer.value.getTotalSize() : 0);
const knowledgeMessage = computed(() => messages.value.find((message) => message.id === knowledgeMessageID.value) ?? null);
const failureDescription = computed(() => {
  const failure = store.failure;
  if (!failure) return "";
  const identity = [
    failure.status ? `HTTP ${failure.status}` : "",
    failure.requestID ? `Request ${failure.requestID}` : "",
    failure.traceID ? `Trace ${failure.traceID}` : "",
    failure.idempotentReplay === true ? "Idempotent replay" : "",
  ].filter(Boolean).join(" · ");
  const next = failure.nextSteps.length ? `下一步：${failure.nextSteps.join("；")}` : "";
  return [identity, next].filter(Boolean).join("。 ");
});

function formatTime(value?: string): string {
  if (!value) return "时间未知";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : dateFormatter.format(date);
}

function statusLabel(value?: string): string {
  return ({
    pending: "等待 Worker",
    running: "执行中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    diagnosed: "已诊断",
    insufficient: "证据不足",
  } as Record<string, string>)[value || ""] || value || "无运行";
}

function statusColor(value?: string): "success" | "info" | "warning" | "error" | "neutral" {
  if (value === "completed" || value === "diagnosed") return "success";
  if (value === "pending" || value === "running") return "info";
  if (value === "insufficient") return "warning";
  if (value === "failed" || value === "cancelled") return "error";
  return "neutral";
}

function streamColor(): "success" | "info" | "warning" | "error" | "neutral" {
  if (store.streamState === "connected") return "success";
  if (store.streamState === "connecting") return "info";
  if (store.streamState === "reconnecting") return "warning";
  if (store.streamState === "disconnected") return "error";
  return "neutral";
}

function streamLabel(): string {
  return ({
    connecting: "Connecting",
    connected: "Live",
    reconnecting: "Reconnecting",
    disconnected: "Disconnected",
    stopped: "Stopped",
  })[store.streamState];
}

function measureRow(element: Element | ComponentPublicInstance | null) {
  const resolved = element instanceof Element ? element : element?.$el;
  if (resolved instanceof HTMLDivElement && virtualized.value) virtualizer.value.measureElement(resolved);
}

function rowStyle(start: number) {
  return virtualized.value ? { transform: `translateY(${start}px)` } : undefined;
}

async function submitMessage() {
  const content = draft.value.trim();
  if (!content || store.sending) return;
  await store.sendMessage(content);
  if (!store.pendingMessageContent) draft.value = "";
}

function openKnowledgeConfirmation(message?: ConsultationMessage) {
  if (!message) return;
  knowledgeMessageID.value = message.id;
  knowledgeTitle.value = store.consultation?.title || "Agent 结论";
}

async function confirmKnowledge() {
  if (!knowledgeMessage.value || knowledgeTitle.value.trim().length < 2) return;
  await store.saveKnowledgeFromMessage(knowledgeMessage.value, knowledgeTitle.value);
  if (!store.error) {
    knowledgeMessageID.value = "";
    knowledgeTitle.value = "";
  }
}

function updateKnowledgeOpen(value: boolean) {
  if (!value && !store.mutating) knowledgeMessageID.value = "";
}

async function copyText(value: string, key: string) {
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const textarea = document.createElement("textarea");
    textarea.value = value;
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.append(textarea);
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
  }
  copiedRowID.value = key;
  window.setTimeout(() => { if (copiedRowID.value === key) copiedRowID.value = ""; }, 1200);
}

watch(
  () => [messages.value.length, Boolean(store.liveAnswer)],
  () => void nextTick(() => {
    if (!rows.value.length) return;
    if (virtualized.value) virtualizer.value.scrollToIndex(rows.value.length - 1, { align: "end" });
    else messageList.value?.scrollTo({ top: messageList.value.scrollHeight, behavior: "smooth" });
  }),
);
</script>

<template>
  <section
    class="agent-conversation"
    :class="{ 'is-compact': compact }"
    :aria-labelledby="`${instanceID}-heading`"
  >
    <header class="conversation-header">
      <div class="conversation-title">
        <span class="section-kicker">{{ store.selection === "consultation" ? "Consultation" : "Investigation" }}</span>
        <h2 :id="`${instanceID}-heading`">
          {{ heading }}
        </h2>
        <span
          v-if="run"
          class="run-identity"
          translate="no"
        >{{ run.id }}</span>
      </div>
      <div class="run-actions">
        <UBadge
          v-if="store.selection === 'consultation'"
          :color="streamColor()"
          variant="subtle"
          :icon="store.streamState === 'connected' ? 'i-lucide-radio' : store.streamState === 'disconnected' ? 'i-lucide-cloud-off' : 'i-lucide-refresh-cw'"
          :label="streamLabel()"
          data-testid="agent-stream-state"
        />
        <UBadge
          :color="statusColor(run?.outcome || run?.status)"
          variant="subtle"
          :label="statusLabel(run?.outcome || run?.status)"
        />
        <UTooltip
          v-if="canCancel"
          text="取消当前 Agent 运行"
        >
          <UButton
            color="error"
            variant="ghost"
            icon="i-lucide-square"
            square
            :loading="store.mutating"
            aria-label="取消当前 Agent 运行"
            @click="store.cancelRun"
          />
        </UTooltip>
      </div>
    </header>

    <UAlert
      v-if="store.failure"
      class="conversation-alert"
      color="error"
      variant="soft"
      icon="i-lucide-circle-alert"
      :title="store.error"
      :description="failureDescription"
      close
      @update:open="store.clearFailure"
    />
    <UAlert
      v-else-if="store.notice"
      class="conversation-alert"
      color="success"
      variant="soft"
      icon="i-lucide-circle-check"
      title="请求状态"
      :description="store.notice"
    />
    <UAlert
      v-if="store.streamState === 'reconnecting' || store.streamState === 'disconnected'"
      class="conversation-alert"
      :color="store.streamState === 'disconnected' ? 'error' : 'warning'"
      variant="soft"
      :icon="store.streamState === 'disconnected' ? 'i-lucide-cloud-off' : 'i-lucide-refresh-cw'"
      :title="store.streamState === 'disconnected' ? 'Agent Stream 已断开' : 'Agent Stream 正在重连'"
      :description="`当前持久化内容保持原位；未确认游标连续前不声明新内容已同步。游标 ${store.streamCursor || '尚未收到'}，忽略重复事件 ${store.duplicateEvents}。`"
    />

    <h3
      v-if="hasTools"
      :id="toolHeadingID"
      class="visually-hidden"
    >
      {{ store.selection === "consultation" ? "当前 Tool Progress" : "Bounded Tools" }}
    </h3>
    <div
      ref="messageList"
      class="message-list"
      :class="{ 'is-virtualized': virtualized }"
      aria-live="polite"
      :aria-busy="store.loading"
    >
      <div
        v-if="store.loading && !run"
        class="conversation-empty"
      >
        <UIcon
          class="spinning"
          name="i-lucide-loader-circle"
          aria-hidden="true"
        />
        <strong>正在读取 Agent 记录…</strong>
      </div>
      <div
        v-else-if="!store.selectedID"
        class="conversation-empty"
      >
        <UIcon
          name="i-lucide-bot"
          aria-hidden="true"
        />
        <strong>选择一条 Agent 记录</strong>
      </div>

      <div
        v-else
        class="conversation-rows"
        :style="virtualized ? { height: `${totalSize}px` } : undefined"
        role="feed"
        :aria-label="store.selection === 'consultation' ? 'Consultation 消息与 Tool 进度' : 'Investigation 过程'"
      >
        <div
          v-for="item in renderedRows"
          :key="item.row.key"
          :ref="measureRow"
          class="conversation-row"
          :class="{ 'is-tool-row': item.row.kind === 'step' }"
          :data-index="item.index"
          :style="rowStyle(item.start)"
          :aria-posinset="item.index + 1"
          :aria-setsize="rows.length"
        >
          <article
            v-if="item.row.kind === 'message'"
            class="message"
            :class="`${item.row.role}-message`"
          >
            <span class="message-avatar">
              <UIcon
                :name="item.row.role === 'owner' ? 'i-lucide-user-round' : 'i-lucide-bot'"
                aria-hidden="true"
              />
            </span>
            <div class="message-body">
              <header>
                <strong>{{ item.row.title }}</strong>
                <time
                  v-if="item.row.time"
                  :datetime="item.row.time"
                >{{ formatTime(item.row.time) }}</time>
              </header>
              <p>{{ item.row.content }}</p>
              <footer>
                <span v-if="item.row.footer"><UIcon
                  name="i-lucide-database"
                  aria-hidden="true"
                />{{ item.row.footer }}</span>
                <UTooltip text="复制完整内容">
                  <UButton
                    color="neutral"
                    variant="ghost"
                    :icon="copiedRowID === item.row.key ? 'i-lucide-copy-check' : 'i-lucide-copy'"
                    square
                    size="xs"
                    :aria-label="`复制 ${item.row.title} 完整内容`"
                    @click="copyText(item.row.content, item.row.key)"
                  />
                </UTooltip>
                <UTooltip
                  v-if="item.row.message?.role === 'assistant'"
                  text="确认为 Owner Knowledge"
                >
                  <UButton
                    color="neutral"
                    variant="ghost"
                    icon="i-lucide-save"
                    square
                    size="xs"
                    aria-label="将这条 Agent 回复确认为 Knowledge"
                    @click="openKnowledgeConfirmation(item.row.message)"
                  />
                </UTooltip>
              </footer>
            </div>
          </article>

          <article
            v-else
            class="tool-row"
            :data-status="item.row.step.status"
            :aria-labelledby="toolHeadingID"
          >
            <span class="tool-state">
              <UIcon
                :class="{ spinning: item.row.step.status === 'running' }"
                :name="item.row.step.status === 'running' ? 'i-lucide-loader-circle' : item.row.step.status === 'completed' ? 'i-lucide-circle-check' : item.row.step.status === 'failed' ? 'i-lucide-circle-x' : 'i-lucide-clock-3'"
                aria-hidden="true"
              />
            </span>
            <div>
              <strong translate="no">{{ item.row.step.tool }}</strong>
              <small>{{ item.row.step.result_summary || item.row.step.error_code || item.row.step.status }}</small>
              <code
                v-if="item.row.step.evidence_id"
                translate="no"
              >Evidence {{ item.row.step.evidence_id }}</code>
            </div>
            <span class="tool-duration">{{ item.row.step.duration_ms.toLocaleString("zh-CN") }} ms</span>
            <UTooltip text="复制完整 Tool 状态">
              <UButton
                color="neutral"
                variant="ghost"
                :icon="copiedRowID === item.row.key ? 'i-lucide-copy-check' : 'i-lucide-copy'"
                square
                size="xs"
                :aria-label="`复制 ${item.row.step.tool} 完整状态`"
                @click="copyText(JSON.stringify(item.row.step, null, 2), item.row.key)"
              />
            </UTooltip>
          </article>
        </div>
      </div>
    </div>

    <form
      v-if="store.selection === 'consultation' && store.consultation"
      class="composer"
      @submit.prevent="submitMessage"
    >
      <UFormField
        :label="compact ? undefined : '发送给 Agent'"
        :name="`${instanceID}-message`"
        class="composer-field"
      >
        <UTextarea
          :id="`${instanceID}-message`"
          v-model="draft"
          name="agent_message"
          autocomplete="off"
          :maxlength="16000"
          :rows="compact ? 2 : 3"
          placeholder="询问当前 Snapshot 中可由 Evidence 支持的问题…"
          autoresize
          @keydown.ctrl.enter.prevent="submitMessage"
          @keydown.meta.enter.prevent="submitMessage"
        />
      </UFormField>
      <div class="composer-actions">
        <span>{{ draft.length.toLocaleString("zh-CN") }} / 16,000</span>
        <UButton
          type="submit"
          color="primary"
          icon="i-lucide-send"
          label="发送消息"
          :loading="store.sending"
          :disabled="store.sending || !draft.trim()"
        />
      </div>
    </form>

    <UModal
      :open="Boolean(knowledgeMessageID)"
      title="保存 Owner Knowledge"
      description="只保存选中的完整 Agent 回复，并保留 Consultation、Message、Scope 与 Revision 来源。"
      :close="false"
      :dismissible="!store.mutating"
      @update:open="updateKnowledgeOpen"
    >
      <template #body>
        <UFormField
          label="Knowledge 标题"
          :name="`${instanceID}-knowledge-title`"
          required
        >
          <UInput
            :id="`${instanceID}-knowledge-title`"
            v-model="knowledgeTitle"
            name="knowledge_title"
            autocomplete="off"
            :maxlength="128"
            autofocus
          />
        </UFormField>
      </template>
      <template #footer>
        <div class="modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-x"
            label="取消"
            :disabled="store.mutating"
            @click="knowledgeMessageID = ''"
          />
          <UButton
            color="primary"
            icon="i-lucide-save"
            label="Owner 确认保存"
            :loading="store.mutating"
            :disabled="knowledgeTitle.trim().length < 2"
            @click="confirmKnowledge"
          />
        </div>
      </template>
    </UModal>

    <p
      class="copy-status"
      aria-live="polite"
    >
      {{ copiedRowID ? "完整内容已复制" : "" }}
    </p>
  </section>
</template>

<style scoped>
.agent-conversation { position: relative; display: grid; min-width: 0; min-height: 0; grid-template-rows: auto auto auto auto minmax(0, 1fr) auto; background: var(--co-bg-canvas); }
.conversation-header { display: flex; min-width: 0; min-height: 60px; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-4); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.conversation-title { display: grid; min-width: 0; gap: 1px; }
.section-kicker { color: var(--co-text-muted); font-size: 9px; font-weight: 700; text-transform: uppercase; }
.conversation-title h2 { max-width: 720px; margin: 0; overflow: hidden; font-size: 14px; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.run-identity { overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.run-actions { display: flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-2); }
.conversation-alert { border-radius: 0; }

.message-list { min-height: 0; padding: var(--co-space-4); overflow-y: auto; overscroll-behavior: contain; scroll-behavior: smooth; }
.conversation-empty { display: grid; min-height: 280px; place-items: center; align-content: center; gap: var(--co-space-3); color: var(--co-text-muted); font-size: 12px; }
.conversation-empty :deep(svg) { width: 28px; height: 28px; }
.conversation-rows { position: relative; width: min(100%, 860px); margin-inline: auto; }
.conversation-row { width: 100%; padding-bottom: var(--co-space-4); }
.is-virtualized .conversation-row { position: absolute; top: 0; left: 0; }

.message { display: grid; width: 100%; grid-template-columns: 32px minmax(0, 1fr); gap: var(--co-space-3); }
.message-avatar { display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); }
.message-avatar :deep(svg) { width: 16px; height: 16px; }
.assistant-message .message-avatar { border-color: var(--co-status-info-border); color: var(--co-action-primary); background: var(--co-status-info-bg); }
.message-body { min-width: 0; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.message-body > header, .message-body > footer { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.message-body > header { justify-content: space-between; }
.message-body > header strong { font-size: 11px; }
.message-body time { color: var(--co-text-muted); font-size: 9px; }
.message-body p { margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); font-size: 13px; line-height: 1.65; overflow-wrap: anywhere; white-space: pre-wrap; }
.message-body > footer { justify-content: flex-end; margin-top: var(--co-space-3); padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); color: var(--co-text-muted); font-size: 9px; }
.message-body > footer > span { display: inline-flex; min-width: 0; align-items: center; gap: 4px; margin-right: auto; overflow-wrap: anywhere; }
.message-body > footer :deep(svg) { width: 13px; height: 13px; }

.tool-row { display: grid; min-width: 0; grid-template-columns: 24px minmax(0, 1fr) auto 28px; align-items: center; gap: var(--co-space-2); min-height: 50px; padding: var(--co-space-2) var(--co-space-3); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.tool-state { display: grid; place-items: center; color: var(--co-text-muted); }
.tool-state :deep(svg) { width: 15px; height: 15px; }
.tool-row[data-status="completed"] .tool-state { color: var(--co-status-success-fg); }
.tool-row[data-status="failed"] .tool-state { color: var(--co-status-critical-fg); }
.tool-row > div { display: grid; min-width: 0; gap: 2px; }
.tool-row strong, .tool-row small, .tool-row code { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tool-row strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 10px; }
.tool-row small, .tool-duration, .tool-row code { color: var(--co-text-muted); font-size: 9px; }
.tool-duration { font-variant-numeric: tabular-nums; }
.tool-row code { color: var(--co-status-info-fg); }

.composer { display: grid; min-width: 0; gap: var(--co-space-2); padding: var(--co-space-3) var(--co-space-4); border-top: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.composer-field { min-width: 0; }
.composer-field :deep(textarea) { width: 100%; max-height: 180px; resize: vertical; }
.composer-actions, .modal-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.composer-actions > span { margin-right: auto; color: var(--co-text-muted); font-size: 9px; font-variant-numeric: tabular-nums; }
.modal-actions { width: 100%; }
.copy-status { position: absolute; right: var(--co-space-3); bottom: var(--co-space-2); margin: 0; pointer-events: none; color: var(--co-status-success-fg); font-size: 9px; }
.is-compact .conversation-header { min-height: 54px; padding-inline: var(--co-space-3); }
.is-compact .message-list { padding: var(--co-space-3); }
.is-compact .composer { padding: var(--co-space-2) var(--co-space-3); }
.is-compact .run-identity { max-width: 220px; }

.spinning { animation: agent-spin 900ms linear infinite; }
@keyframes agent-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spinning { animation-duration: 0.00001ms; } .message-list { scroll-behavior: auto; } }
@media (max-width: 900px) { .conversation-header { align-items: flex-start; } .run-actions { flex-wrap: wrap; justify-content: flex-end; } }
</style>
