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

    <section
      v-if="store.failure"
      class="conversation-error"
      role="alert"
      aria-live="assertive"
    >
      <UIcon name="i-lucide-circle-alert" aria-hidden="true" />
      <div>
        <strong>{{ store.error }}</strong>
        <p v-if="failureDescription">{{ failureDescription }}</p>
      </div>
      <UTooltip text="关闭错误">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-x"
          square
          aria-label="关闭错误"
          @click="store.clearFailure"
        />
      </UTooltip>
    </section>
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
      variant="outline"
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
      :class="{ 'is-virtualized': virtualized, 'is-idle': !store.selectedID && !store.loading }"
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
        <span class="conversation-empty-visual" aria-hidden="true">
          <span><UIcon name="i-lucide-bot" /></span>
        </span>
        <span class="empty-kicker">Agent / standby</span>
        <strong>选择一条 Agent 记录</strong>
        <small>等待持久化 Consultation 或 Investigation</small>
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
.agent-conversation {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  background: var(--co-bg-canvas);
}
.conversation-header {
  position: relative;
  z-index: 2;
  display: flex;
  min-width: 0;
  min-height: 70px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-3);
  padding: 10px 18px;
  border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  background: color-mix(in srgb, var(--co-bg-floating) 78%, transparent);
  backdrop-filter: blur(var(--co-glass-blur));
}
.conversation-title { display: grid; min-width: 0; gap: 2px; }
.section-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }
.conversation-title h2 { max-width: 720px; margin: 0; overflow: hidden; color: var(--co-text-primary); font-size: 15px; font-weight: 720; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.run-identity { max-width: 560px; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.run-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 6px; }
.run-actions :deep(button) { width: 30px; min-width: 30px; height: 30px; border-radius: 8px; }
.conversation-error {
  display: grid;
  min-width: 0;
  max-height: 112px;
  flex: 0 0 auto;
  grid-template-columns: 22px minmax(0, 1fr) 30px;
  align-items: start;
  gap: 10px;
  margin: 8px 14px 0;
  padding: 11px 10px 11px 12px;
  overflow: hidden;
  border: 1px solid var(--co-status-critical-border);
  border-radius: 9px;
  color: var(--co-status-critical-fg);
  background: color-mix(in srgb, var(--co-status-critical-bg) 28%, var(--co-bg-canvas));
}
.conversation-error > :deep(svg) { width: 17px; height: 17px; margin-top: 1px; }
.conversation-error > div { min-width: 0; overflow-y: auto; }
.conversation-error strong,
.conversation-error p { display: block; max-width: 100%; overflow-wrap: anywhere; white-space: normal; word-break: break-word; }
.conversation-error strong { color: var(--co-status-critical-fg); font-size: 11px; line-height: 1.45; }
.conversation-error p { margin: 3px 0 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; line-height: 1.45; }
.conversation-error :deep(button) { width: 28px; min-width: 28px; height: 28px; border-radius: 8px; }
.conversation-alert { min-width: 0; max-width: calc(100% - 28px); flex: 0 0 auto; margin: 8px 14px 0; overflow: hidden; border-radius: 9px; }
.conversation-alert :deep(*) { min-width: 0; }
.conversation-alert :deep(p) { overflow-wrap: anywhere; white-space: normal; word-break: break-word; }

.message-list { min-height: 0; flex: 1 1 auto; padding: 24px clamp(18px, 3vw, 42px) 18px; overflow-y: auto; overscroll-behavior: contain; scroll-behavior: smooth; }
.message-list.is-idle {
  background-image:
    linear-gradient(to right, color-mix(in srgb, var(--co-border-default) 48%, transparent) 1px, transparent 1px),
    linear-gradient(to bottom, color-mix(in srgb, var(--co-border-default) 48%, transparent) 1px, transparent 1px);
  background-position: center;
  background-size: 52px 52px;
  box-shadow: inset 0 90px 90px -70px var(--co-bg-canvas), inset 0 -90px 90px -70px var(--co-bg-canvas);
}
.conversation-empty { display: grid; min-height: 100%; place-items: center; align-content: center; gap: 7px; color: var(--co-text-muted); font-size: 12px; text-align: center; }
.conversation-empty > :deep(svg) { width: 36px; height: 36px; padding: 8px; border: 1px solid var(--co-border-default); border-radius: 12px; color: var(--co-text-secondary); background: var(--co-bg-floating); box-shadow: var(--co-shadow-panel); }
.conversation-empty-visual {
  position: relative;
  display: grid;
  width: 116px;
  height: 92px;
  margin-bottom: 9px;
  place-items: center;
}
.conversation-empty-visual::before,
.conversation-empty-visual::after { position: absolute; content: ""; }
.conversation-empty-visual::before { right: 0; left: 0; height: 1px; background: linear-gradient(90deg, transparent, var(--co-border-strong), transparent); }
.conversation-empty-visual::after { top: 0; bottom: 0; width: 1px; background: linear-gradient(180deg, transparent, var(--co-border-strong), transparent); }
.conversation-empty-visual > span {
  position: relative;
  z-index: 1;
  display: grid;
  width: 54px;
  height: 54px;
  place-items: center;
  border: 1px solid color-mix(in srgb, var(--co-ink-action) 32%, var(--co-border-default));
  border-radius: 15px;
  color: var(--co-bg-canvas);
  background: var(--co-ink-action);
  box-shadow: 0 16px 34px color-mix(in srgb, var(--co-ink-action) 20%, transparent), 0 0 0 8px color-mix(in srgb, var(--co-bg-canvas) 78%, transparent);
}
.conversation-empty-visual > span::after { position: absolute; right: 5px; bottom: 5px; width: 6px; height: 6px; border: 1px solid var(--co-ink-action); border-radius: 50%; background: var(--co-viz-live); box-shadow: 0 0 0 3px var(--co-viz-live-soft); content: ""; }
.conversation-empty-visual :deep(svg) { width: 23px; height: 23px; }
.conversation-empty .empty-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
.conversation-empty strong { color: var(--co-text-primary); font-size: 14px; font-weight: 720; }
.conversation-empty small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.conversation-rows { position: relative; width: min(100%, 900px); margin-inline: auto; }
.conversation-row { width: 100%; padding-bottom: 18px; }
.is-virtualized .conversation-row { position: absolute; top: 0; left: 0; }

.message { display: grid; width: 100%; grid-template-columns: 34px minmax(0, 1fr); gap: 12px; }
.message-avatar { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 10px; color: var(--co-text-secondary); background: var(--co-bg-floating); box-shadow: 0 7px 16px rgb(52 46 39 / 6%); }
.message-avatar :deep(svg) { width: 15px; height: 15px; }
.assistant-message .message-avatar { border-color: color-mix(in srgb, var(--co-ink-action) 34%, var(--co-border-default)); color: var(--co-bg-canvas); background: var(--co-ink-action); }
.message-body { min-width: 0; padding: 15px 16px; border: 1px solid var(--co-border-default); border-radius: 12px; background: var(--co-bg-floating); box-shadow: 0 10px 30px rgb(52 46 39 / 6%); }
.message-body > header, .message-body > footer { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.message-body > header { justify-content: space-between; }
.message-body > header strong { font-size: 10px; font-weight: 760; }
.message-body time { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; }
.message-body p { margin: 8px 0 0; color: var(--co-text-secondary); font-size: 13px; line-height: 1.72; overflow-wrap: anywhere; white-space: pre-wrap; }
.message-body > footer { justify-content: flex-end; margin-top: 12px; padding-top: 8px; border-top: 1px solid color-mix(in srgb, var(--co-border-default) 74%, transparent); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; }
.message-body > footer > span { display: inline-flex; min-width: 0; align-items: center; gap: 4px; margin-right: auto; overflow-wrap: anywhere; }
.message-body > footer :deep(svg) { width: 13px; height: 13px; }
.owner-message { width: min(82%, 720px); grid-template-columns: minmax(0, 1fr) 34px; margin-left: auto; }
.owner-message .message-avatar { grid-column: 2; grid-row: 1; color: var(--co-bg-canvas); background: var(--co-ink-action); }
.owner-message .message-body { grid-column: 1; grid-row: 1; border-color: var(--co-ink-action); color: var(--co-text-on-action); background: var(--co-ink-action); box-shadow: 0 12px 32px color-mix(in srgb, var(--co-ink-action) 18%, transparent); }
.owner-message .message-body p,
.owner-message .message-body time,
.owner-message .message-body > footer { color: color-mix(in srgb, var(--co-text-on-action) 78%, transparent); }
.owner-message .message-body > footer { border-top-color: color-mix(in srgb, var(--co-text-on-action) 15%, transparent); }
.owner-message .message-body :deep(button) { color: var(--co-text-on-action); }

.tool-row { display: grid; width: calc(100% - 46px); min-width: 0; min-height: 48px; grid-template-columns: 25px minmax(0, 1fr) auto 28px; align-items: center; gap: 9px; margin-left: 46px; padding: 7px 9px; border: 1px solid var(--co-border-default); border-radius: 10px; background: color-mix(in srgb, var(--co-bg-surface) 74%, transparent); }
.tool-state { display: grid; width: 25px; height: 25px; place-items: center; border-radius: 8px; color: var(--co-text-muted); background: var(--co-bg-floating); }
.tool-state :deep(svg) { width: 15px; height: 15px; }
.tool-row[data-status="completed"] .tool-state { color: var(--co-status-success-fg); }
.tool-row[data-status="failed"] .tool-state { color: var(--co-status-critical-fg); }
.tool-row > div { display: grid; min-width: 0; gap: 2px; }
.tool-row strong, .tool-row small, .tool-row code { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tool-row strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 9px; }
.tool-row small, .tool-duration, .tool-row code { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; }
.tool-duration { font-variant-numeric: tabular-nums; }
.tool-row code { color: var(--co-status-info-fg); }

.composer { display: grid; min-width: 0; flex: 0 0 auto; gap: 8px; margin: 0 clamp(14px, 2.4vw, 28px) 16px; padding: 10px 11px 9px; border: 1px solid var(--co-border-strong); border-radius: 14px; background: color-mix(in srgb, var(--co-bg-floating) 90%, transparent); box-shadow: 0 16px 42px rgb(52 46 39 / 11%); backdrop-filter: blur(var(--co-glass-blur)); }
.composer-field { min-width: 0; }
.composer-field :deep(label) { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; }
.composer-field :deep(textarea) { width: 100%; max-height: 180px; border: 0; color: var(--co-text-primary); background: transparent; box-shadow: none; resize: vertical; }
.composer-actions, .modal-actions { display: flex; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.composer-actions > span { margin-right: auto; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-variant-numeric: tabular-nums; }
.composer-actions :deep(button) { min-height: 34px; border-radius: 9px; }
.modal-actions { width: 100%; }
.copy-status { position: absolute; right: var(--co-space-3); bottom: var(--co-space-2); margin: 0; pointer-events: none; color: var(--co-status-success-fg); font-size: 9px; }
.is-compact .conversation-header { min-height: 58px; padding-inline: 12px; }
.is-compact .conversation-title h2 { max-width: 250px; font-size: 13px; }
.is-compact .message-list { padding: 15px 12px 10px; }
.is-compact .conversation-row { padding-bottom: 12px; }
.is-compact .message { grid-template-columns: 29px minmax(0, 1fr); gap: 8px; }
.is-compact .message-avatar { width: 29px; height: 29px; border-radius: 8px; }
.is-compact .message-body { padding: 11px 12px; border-radius: 10px; }
.is-compact .message-body p { font-size: 12px; line-height: 1.62; }
.is-compact .owner-message { width: 90%; grid-template-columns: minmax(0, 1fr) 29px; }
.is-compact .tool-row { width: calc(100% - 37px); margin-left: 37px; }
.is-compact .composer { margin: 0 10px 10px; padding: 8px 9px; border-radius: 11px; }
.is-compact .run-identity { max-width: 220px; }

.spinning { animation: agent-spin 900ms linear infinite; }
@keyframes agent-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spinning { animation-duration: 0.00001ms; } .message-list { scroll-behavior: auto; } }
</style>
