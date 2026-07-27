<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import {
  Bot,
  CheckCircle2,
  CircleAlert,
  Clock3,
  Database,
  LoaderCircle,
  Save,
  Send,
  Square,
  UserRound,
  Wrench,
  XCircle,
} from "lucide-vue-next";

import type { ConsultationMessage } from "../../api/agent";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

const props = defineProps<{ compact?: boolean }>();
const store = useAgentWorkspaceStore();
const draft = ref("");
const knowledgeMessageID = ref("");
const knowledgeTitle = ref("");
const messageList = ref<HTMLElement | null>(null);
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium" });

const run = computed(() => store.selectedRun);
const messages = computed(() => store.consultation?.messages ?? []);
const canCancel = computed(() => run.value?.status === "pending" || run.value?.status === "running");
const instanceID = computed(() => props.compact ? "global-agent-conversation" : "agent-conversation");
const heading = computed(() => store.selection === "consultation"
  ? store.consultation?.title || "Consultation"
  : run.value?.objective || "Investigation");

function formatTime(value?: string): string {
  if (!value) return "时间未知";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : dateFormatter.format(date);
}

function statusLabel(value?: string): string {
  const labels: Record<string, string> = {
    pending: "等待 Worker",
    running: "执行中",
    completed: "已完成",
    failed: "失败",
    cancelled: "已取消",
    diagnosed: "已诊断",
    insufficient: "证据不足",
  };
  return labels[value || ""] || value || "无运行";
}

function knowledgeInputID(messageID: string): string {
  return `${instanceID.value}-knowledge-title-${messageID}`;
}

async function submitMessage() {
  const content = draft.value.trim();
  if (!content || store.sending) return;
  await store.sendMessage(content);
  if (!store.error) draft.value = "";
}

function openKnowledgeConfirmation(message: ConsultationMessage) {
  knowledgeMessageID.value = message.id;
  knowledgeTitle.value = store.consultation?.title || "Agent 结论";
}

async function confirmKnowledge(message: ConsultationMessage) {
  if (knowledgeTitle.value.trim().length < 2) return;
  await store.saveKnowledgeFromMessage(message, knowledgeTitle.value);
  if (!store.error) {
    knowledgeMessageID.value = "";
    knowledgeTitle.value = "";
  }
}

watch(
  () => [messages.value.length, store.liveAnswer.length],
  () => void nextTick(() => messageList.value?.scrollTo({ top: messageList.value.scrollHeight, behavior: "smooth" })),
);
</script>

<template>
  <section class="agent-conversation" :class="{ 'is-compact': compact }" :aria-labelledby="`${instanceID}-heading`">
    <header class="conversation-header">
      <div class="conversation-title">
        <span class="section-kicker">{{ store.selection === "consultation" ? "Consultation" : "Investigation" }}</span>
        <h2 :id="`${instanceID}-heading`">{{ heading }}</h2>
        <span v-if="run" class="run-identity" translate="no">{{ run.id }}</span>
      </div>
      <div class="run-actions">
        <span class="stream-state" :data-state="store.streamState"><i aria-hidden="true"></i>{{ store.selection === "consultation" ? store.streamState : "durable" }}</span>
        <span class="run-status" :data-status="run?.status || 'idle'">{{ statusLabel(run?.outcome || run?.status) }}</span>
        <button v-if="canCancel" type="button" class="icon-command" aria-label="取消当前 Agent 运行" title="取消运行" :disabled="store.mutating" @click="store.cancelRun">
          <Square :size="15" fill="currentColor" aria-hidden="true" />
        </button>
      </div>
    </header>

    <div v-if="store.error" class="conversation-alert is-error" role="alert" aria-live="polite"><CircleAlert :size="17" aria-hidden="true" />{{ store.error }}</div>
    <div v-if="store.notice" class="conversation-alert" role="status" aria-live="polite"><CheckCircle2 :size="17" aria-hidden="true" />{{ store.notice }}</div>

    <div ref="messageList" class="message-list" aria-live="polite">
      <div v-if="store.loading && !run" class="conversation-empty"><LoaderCircle class="spinning" :size="28" aria-hidden="true" /><strong>正在读取 Agent 记录…</strong></div>
      <div v-else-if="!store.selectedID" class="conversation-empty"><Bot :size="30" aria-hidden="true" /><strong>选择一条 Agent 记录</strong></div>

      <template v-if="store.selection === 'investigation' && run">
        <article class="message owner-message">
          <span class="message-avatar"><UserRound :size="17" aria-hidden="true" /></span>
          <div class="message-body"><header><strong>Investigation Objective</strong><time :datetime="run.created_at">{{ formatTime(run.created_at) }}</time></header><p>{{ run.objective }}</p></div>
        </article>
        <section v-if="run.steps.length" class="tool-timeline" :aria-labelledby="`${instanceID}-investigation-tools-heading`">
          <header><Wrench :size="16" aria-hidden="true" /><h3 :id="`${instanceID}-investigation-tools-heading`">Bounded Tools</h3><span>{{ run.steps.length }}</span></header>
          <ol>
            <li v-for="step in run.steps" :key="step.id" :data-status="step.status">
              <span class="tool-state"><LoaderCircle v-if="step.status === 'running'" class="spinning" :size="15" aria-hidden="true" /><CheckCircle2 v-else-if="step.status === 'completed'" :size="15" aria-hidden="true" /><XCircle v-else-if="step.status === 'failed'" :size="15" aria-hidden="true" /><Clock3 v-else :size="15" aria-hidden="true" /></span>
              <div><strong translate="no">{{ step.tool }}</strong><small>{{ step.result_summary || step.error_code || step.status }}</small></div>
              <span class="tool-duration">{{ step.duration_ms }} ms</span>
              <span v-if="step.evidence_id" class="evidence-output"><Database :size="13" aria-hidden="true" />{{ step.evidence_id }}</span>
            </li>
          </ol>
        </section>
        <article v-if="run.answer" class="message assistant-message">
          <span class="message-avatar"><Bot :size="17" aria-hidden="true" /></span>
          <div class="message-body"><header><strong>Agent Outcome</strong><time :datetime="run.completed_at">{{ formatTime(run.completed_at) }}</time></header><p>{{ run.answer }}</p><footer><span>Uncertainty · {{ run.uncertainty }}</span><span v-if="run.failure_code">{{ run.failure_code }}</span></footer></div>
        </article>
      </template>

      <template v-if="store.selection === 'consultation'">
        <article v-for="message in messages" :key="message.id" class="message" :class="message.role === 'owner' ? 'owner-message' : 'assistant-message'">
          <span class="message-avatar"><UserRound v-if="message.role === 'owner'" :size="17" aria-hidden="true" /><Bot v-else :size="17" aria-hidden="true" /></span>
          <div class="message-body">
            <header><strong>{{ message.role === "owner" ? "Owner" : "Agent" }}</strong><time :datetime="message.created_at">{{ formatTime(message.created_at) }}</time></header>
            <p>{{ message.content }}</p>
            <footer v-if="message.role === 'assistant'">
              <span><Database :size="13" aria-hidden="true" />{{ message.evidence_citations.length }} Evidence</span>
              <span>{{ message.guidance_citations.length }} Guidance</span>
              <button type="button" aria-label="将这条 Agent 回复确认为 Knowledge" title="保存 Knowledge" @click="openKnowledgeConfirmation(message)"><Save :size="14" aria-hidden="true" /></button>
            </footer>
            <form v-if="knowledgeMessageID === message.id" class="knowledge-confirm" @submit.prevent="confirmKnowledge(message)">
              <label :for="knowledgeInputID(message.id)">Knowledge 标题</label>
              <input :id="knowledgeInputID(message.id)" v-model="knowledgeTitle" name="knowledge_title" type="text" autocomplete="off" minlength="2" maxlength="128" />
              <div><button type="button" @click="knowledgeMessageID = ''">取消</button><button type="submit" :disabled="store.mutating || knowledgeTitle.trim().length < 2">Owner 确认保存</button></div>
            </form>
          </div>
        </article>
        <article v-if="store.liveAnswer" class="message assistant-message is-streaming">
          <span class="message-avatar"><Bot :size="17" aria-hidden="true" /></span>
          <div class="message-body"><header><strong>Agent</strong><span>Streaming…</span></header><p>{{ store.liveAnswer }}</p></div>
        </article>
        <section v-if="run?.steps.length" class="tool-timeline" :aria-labelledby="`${instanceID}-consultation-tools-heading`">
          <header><Wrench :size="16" aria-hidden="true" /><h3 :id="`${instanceID}-consultation-tools-heading`">当前 Tool Progress</h3><span>{{ run.steps.length }}</span></header>
          <ol><li v-for="step in run.steps" :key="step.id" :data-status="step.status"><span class="tool-state"><LoaderCircle v-if="step.status === 'running'" class="spinning" :size="15" aria-hidden="true" /><CheckCircle2 v-else-if="step.status === 'completed'" :size="15" aria-hidden="true" /><XCircle v-else-if="step.status === 'failed'" :size="15" aria-hidden="true" /><Clock3 v-else :size="15" aria-hidden="true" /></span><div><strong translate="no">{{ step.tool }}</strong><small>{{ step.result_summary || step.error_code || step.status }}</small></div><span class="tool-duration">{{ step.duration_ms }} ms</span><span v-if="step.evidence_id" class="evidence-output"><Database :size="13" aria-hidden="true" />{{ step.evidence_id }}</span></li></ol>
        </section>
      </template>
    </div>

    <form v-if="store.selection === 'consultation' && store.consultation" class="composer" @submit.prevent="submitMessage">
      <label :for="`${instanceID}-message`" class="visually-hidden">发送给 Agent 的消息</label>
      <textarea :id="`${instanceID}-message`" v-model="draft" name="agent_message" autocomplete="off" maxlength="16000" rows="3" placeholder="询问当前 snapshot 中的运行状态…" @keydown.ctrl.enter.prevent="submitMessage" @keydown.meta.enter.prevent="submitMessage"></textarea>
      <div><span>{{ draft.length.toLocaleString("zh-CN") }} / 16,000</span><button type="submit" :disabled="store.sending || !draft.trim()"><LoaderCircle v-if="store.sending" class="spinning" :size="16" aria-hidden="true" /><Send v-else :size="16" aria-hidden="true" />发送消息</button></div>
    </form>
  </section>
</template>

<style scoped>
.agent-conversation { display: grid; min-width: 0; min-height: 0; grid-template-rows: auto auto auto minmax(0, 1fr) auto; background: var(--co-bg-canvas); }
.conversation-header { display: flex; min-width: 0; min-height: 66px; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3) var(--co-space-5); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.conversation-title { display: grid; min-width: 0; gap: 2px; }
.section-kicker { color: var(--co-text-secondary); font-size: 10px; font-weight: 800; text-transform: uppercase; }
.conversation-title h2 { max-width: 720px; margin: 0; overflow: hidden; font-size: 15px; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.run-identity { overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.run-actions, .stream-state, .run-status { display: flex; flex: 0 0 auto; align-items: center; }
.run-actions { gap: var(--co-space-2); }
.stream-state, .run-status { min-height: 25px; gap: 5px; padding: 0 7px; border: 1px solid var(--co-border-default); border-radius: 4px; color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 9px; font-weight: 750; text-transform: uppercase; }
.stream-state i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-text-muted); }
.stream-state[data-state="connected"] i { background: var(--co-status-success-fg); }
.stream-state[data-state="reconnecting"] i { background: var(--co-status-warning-fg); }
.run-status[data-status="running"], .run-status[data-status="pending"] { border-color: var(--co-status-info-border); color: var(--co-status-info-fg); background: var(--co-status-info-bg); }
.run-status[data-status="failed"], .run-status[data-status="cancelled"] { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.icon-command { display: grid; width: 34px; height: 34px; place-items: center; padding: 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-status-critical-fg); background: var(--co-bg-surface); cursor: pointer; }
.icon-command:hover { border-color: var(--co-status-critical-border); background: var(--co-status-critical-bg); }
.conversation-alert { display: flex; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-5); border-bottom: 1px solid var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); font-size: 11px; overflow-wrap: anywhere; }
.conversation-alert.is-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.message-list { min-height: 0; padding: var(--co-space-5); overflow-y: auto; overscroll-behavior: contain; scroll-behavior: smooth; }
.conversation-empty { display: grid; min-height: 320px; place-items: center; align-content: center; gap: var(--co-space-3); color: var(--co-text-secondary); font-size: 13px; }
.message { display: grid; width: min(100%, 820px); grid-template-columns: 34px minmax(0, 1fr); gap: var(--co-space-3); margin: 0 auto var(--co-space-5); }
.message-avatar { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); }
.assistant-message .message-avatar { border-color: var(--co-status-info-border); color: var(--co-action-primary); background: var(--co-status-info-bg); }
.message-body { min-width: 0; padding: var(--co-space-3) var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.message-body > header, .message-body > footer, .knowledge-confirm > div { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.message-body > header strong { font-size: 11px; }
.message-body time, .message-body > header span { color: var(--co-text-secondary); font-size: 9px; }
.message-body p { margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); font-size: 13px; line-height: 1.65; overflow-wrap: anywhere; white-space: pre-wrap; }
.message-body > footer { justify-content: flex-start; margin-top: var(--co-space-3); padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); color: var(--co-text-secondary); font-size: 10px; }
.message-body > footer span { display: inline-flex; align-items: center; gap: 4px; }
.message-body > footer button { display: grid; width: 30px; height: 30px; margin-left: auto; place-items: center; padding: 0; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: transparent; cursor: pointer; }
.message-body > footer button:hover { color: var(--co-action-primary); background: var(--co-bg-hover); }
.is-streaming .message-body { border-color: var(--co-status-info-border); }
.knowledge-confirm { display: grid; gap: var(--co-space-2); margin-top: var(--co-space-3); padding-top: var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.knowledge-confirm label { color: var(--co-text-secondary); font-size: 11px; font-weight: 700; }
.knowledge-confirm input { min-width: 0; height: 36px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); }
.knowledge-confirm button, .composer button { min-height: 34px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); cursor: pointer; font-weight: 700; }
.knowledge-confirm button[type="submit"], .composer button { color: var(--co-text-on-action); background: var(--co-action-primary); border-color: var(--co-action-primary); }
.tool-timeline { width: min(100%, 820px); margin: 0 auto var(--co-space-5); border-top: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.tool-timeline > header { display: flex; align-items: center; gap: var(--co-space-2); min-height: 38px; color: var(--co-text-secondary); }
.tool-timeline h3 { margin: 0; font-size: 11px; letter-spacing: 0; }
.tool-timeline > header span { margin-left: auto; color: var(--co-text-secondary); font-variant-numeric: tabular-nums; font-size: 10px; }
.tool-timeline ol { margin: 0; padding: 0; list-style: none; }
.tool-timeline li { display: grid; min-width: 0; grid-template-columns: 22px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); padding: var(--co-space-2) 0; border-top: 1px solid var(--co-border-default); }
.tool-state { display: grid; place-items: center; color: var(--co-text-secondary); }
.tool-timeline li[data-status="completed"] .tool-state { color: var(--co-status-success-fg); }
.tool-timeline li[data-status="failed"] .tool-state { color: var(--co-status-critical-fg); }
.tool-timeline li > div { display: grid; min-width: 0; gap: 2px; }
.tool-timeline strong, .tool-timeline small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tool-timeline strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 10px; }
.tool-timeline small, .tool-duration { color: var(--co-text-secondary); font-size: 9px; }
.tool-duration { font-variant-numeric: tabular-nums; }
.evidence-output { grid-column: 2 / -1; display: flex; min-width: 0; align-items: center; gap: 5px; overflow: hidden; color: var(--co-status-info-fg); font-family: var(--co-font-mono); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.composer { padding: var(--co-space-3) var(--co-space-5) max(var(--co-space-3), env(safe-area-inset-bottom)); border-top: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.composer textarea { display: block; width: 100%; min-height: 74px; resize: vertical; padding: var(--co-space-3); border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-panel); color: var(--co-text-primary); background: var(--co-bg-canvas); line-height: 1.5; }
.composer > div { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); margin-top: var(--co-space-2); }
.composer > div span { color: var(--co-text-secondary); font-size: 9px; font-variant-numeric: tabular-nums; }
.composer button { display: inline-flex; align-items: center; gap: var(--co-space-2); }
.composer button:disabled, .knowledge-confirm button:disabled, .icon-command:disabled { cursor: not-allowed; opacity: 0.55; }
.spinning { animation: agent-spin 0.8s linear infinite; }
@keyframes agent-spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spinning { animation: none; } .message-list { scroll-behavior: auto; } }
@media (max-width: 767px) { .conversation-header, .message-list, .composer { padding-inline: var(--co-space-4); } .conversation-header { align-items: flex-start; } .run-actions { flex-wrap: wrap; justify-content: flex-end; } .stream-state { display: none; } .message { grid-template-columns: 28px minmax(0, 1fr); gap: var(--co-space-2); } .message-avatar { width: 28px; height: 28px; } }
</style>
