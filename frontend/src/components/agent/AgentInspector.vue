<script setup lang="ts">
import { computed, ref } from "vue";
import {
  Archive,
  BookOpenText,
  CheckCircle2,
  Database,
  ExternalLink,
  FileKey2,
  KeyRound,
  Lightbulb,
  Link2,
  LockKeyhole,
  RefreshCw,
  ShieldCheck,
  TriangleAlert,
} from "lucide-vue-next";

import type { ActionCard, OperationPlan } from "../../api/agent";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

const props = defineProps<{ compact?: boolean }>();
const store = useAgentWorkspaceStore();
const authoritySubjectID = ref("");
const authorityReason = ref("");
const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium" });

const run = computed(() => store.selectedRun);
const evidence = computed(() => run.value?.evidence_citations ?? []);
const guidance = computed(() => run.value?.guidance_citations ?? []);
const cards = computed(() => run.value?.action_cards ?? []);
const plans = computed(() => run.value?.operation_plans ?? []);
const instanceID = computed(() => props.compact ? "global-agent-inspector" : "agent-inspector");

function formatTime(value?: string): string {
  if (!value) return "时间未知";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未知" : dateFormatter.format(date);
}

function compactJSON(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return "{}";
  }
}

function openAuthority(subject: ActionCard | OperationPlan) {
  authoritySubjectID.value = subject.id;
  authorityReason.value = "已核对 exact target、parameters、preconditions 与 content hash";
}

function authorityInputID(subjectID: string): string {
  return `${instanceID.value}-authority-${subjectID}`;
}

async function authorizeCard(card: ActionCard) {
  if (!window.confirm("确认仅授权这张 exact Action Card？本步骤不会执行操作。")) return;
  await store.authorizeCard(card, authorityReason.value.trim());
  authoritySubjectID.value = "";
}

async function authorizePlan(plan: OperationPlan) {
  if (!window.confirm("确认授权这份 exact Operation Plan？执行仍需独立受控阶段。")) return;
  await store.authorizePlan(plan, authorityReason.value.trim());
  authoritySubjectID.value = "";
}
</script>

<template>
  <aside class="agent-inspector" :class="{ 'is-compact': compact }" aria-label="Agent Context、Evidence 与 Authority">
    <section class="inspector-section context-section">
      <header><div><Archive :size="16" aria-hidden="true" /><h2>Context Snapshot</h2></div><span>{{ store.consultation?.snapshots.length || (run ? 1 : 0) }}</span></header>
      <div v-if="store.consultation && store.contextMismatch" class="context-drift" role="status">
        <TriangleAlert :size="17" aria-hidden="true" />
        <span>当前页面上下文与会话 snapshot 不同。</span>
      </div>
      <button
        v-if="store.consultation && store.contextMismatch"
        type="button"
        class="attach-context"
        :disabled="!store.currentContext || store.currentContext.route !== store.route || store.mutating"
        @click="store.attachCurrentContext"
      ><RefreshCw :size="15" aria-hidden="true" />附加当前上下文</button>
      <dl v-if="store.activeSnapshot" class="inspector-facts">
        <div><dt>Snapshot</dt><dd translate="no">{{ store.activeSnapshot.id }}</dd></div>
        <div><dt>Configuration</dt><dd translate="no">{{ store.activeSnapshot.configuration_revision_id }}</dd></div>
        <div><dt>Scope</dt><dd>{{ store.activeSnapshot.scope.cluster_id }} · {{ store.activeSnapshot.scope.namespaces.join(", ") }}</dd></div>
        <div><dt>Time</dt><dd>{{ formatTime(store.activeSnapshot.time_range.from) }} → {{ formatTime(store.activeSnapshot.time_range.to) }}</dd></div>
        <div><dt>Content hash</dt><dd translate="no">{{ store.activeSnapshot.content_hash }}</dd></div>
      </dl>
      <dl v-else-if="run" class="inspector-facts">
        <div><dt>Snapshot</dt><dd translate="no">{{ run.context_snapshot_id }}</dd></div>
        <div><dt>Configuration</dt><dd translate="no">{{ run.configuration_revision_id }}</dd></div>
      </dl>
      <div v-else class="inspector-empty">未选择 Agent 记录。</div>
    </section>

    <section class="inspector-section">
      <header><div><Database :size="16" aria-hidden="true" /><h2>Current Evidence</h2></div><span>{{ evidence.length }}</span></header>
      <div v-if="!evidence.length" class="inspector-empty">当前运行没有可引用的 Evidence。</div>
      <article v-for="item in evidence" :key="item.id" class="citation-row">
        <div><strong>{{ item.source }}</strong><span>{{ item.use }}</span></div>
        <p>{{ item.summary }}</p>
        <dl><div><dt>Evidence</dt><dd translate="no">{{ item.evidence_id }}</dd></div><div><dt>Observed</dt><dd>{{ formatTime(item.observed_at || item.collected_at) }}</dd></div><div v-if="item.query_execution_id"><dt>Query</dt><dd translate="no">{{ item.query_execution_id }}</dd></div><div><dt>Configuration</dt><dd translate="no">{{ item.configuration_revision_id }}</dd></div></dl>
      </article>
    </section>

    <section class="inspector-section guidance-section">
      <header><div><Lightbulb :size="16" aria-hidden="true" /><h2>Guidance Citations</h2></div><span>{{ guidance.length }}</span></header>
      <div v-if="!guidance.length" class="inspector-empty">本次运行未检索 Knowledge 或 Runbook。</div>
      <article v-for="item in guidance" :key="item.id" class="guidance-row">
        <div><strong>{{ item.type }}</strong><span :class="{ stale: item.stale }">{{ item.stale ? "stale" : `${item.age_seconds.toLocaleString("zh-CN")} s` }}</span></div>
        <p>{{ item.title }}</p>
        <code translate="no">revision {{ item.revision }} · {{ item.revision_id }}</code>
      </article>
    </section>

    <section class="inspector-section authority-section">
      <header><div><ShieldCheck :size="16" aria-hidden="true" /><h2>Authority</h2></div><span>3 levels</span></header>
      <ol class="authority-levels">
        <li><span><Link2 :size="15" aria-hidden="true" /></span><div><strong>Read</strong><small>bounded Provider tools</small></div><i>active</i></li>
        <li><span><KeyRound :size="15" aria-hidden="true" /></span><div><strong>Reversible</strong><small>exact Action Card</small></div><i>{{ cards.length }}</i></li>
        <li><span><LockKeyhole :size="15" aria-hidden="true" /></span><div><strong>High impact</strong><small>immutable Operation Plan</small></div><i>{{ plans.length }}</i></li>
      </ol>

      <article v-for="card in cards" :key="card.id" class="authority-record">
        <header><FileKey2 :size="15" aria-hidden="true" /><strong>{{ card.action_type }}</strong><span>{{ card.status }}</span></header>
        <p>{{ card.risk }}</p><code translate="no">{{ card.content_hash }}</code>
        <details><summary>Exact payload</summary><pre>{{ compactJSON({ target: card.target, parameters: card.parameters, preconditions: card.preconditions }) }}</pre></details>
        <button v-if="card.status === 'proposed'" type="button" @click="openAuthority(card)">审查 exact Action Card</button>
        <form v-if="authoritySubjectID === card.id" @submit.prevent="authorizeCard(card)"><label :for="authorityInputID(card.id)">授权理由</label><textarea :id="authorityInputID(card.id)" v-model="authorityReason" name="authorization_reason" autocomplete="off" maxlength="1024" rows="3"></textarea><button type="submit" :disabled="authorityReason.trim().length < 2 || store.mutating">确认 exact hash</button></form>
      </article>

      <article v-for="plan in plans" :key="plan.id" class="authority-record high-impact">
        <header><LockKeyhole :size="15" aria-hidden="true" /><strong>{{ plan.operation_type }}</strong><span>{{ plan.status }}</span></header>
        <p>{{ plan.risk }}</p><code translate="no">{{ plan.content_hash }}</code>
        <details><summary>Exact immutable plan</summary><pre>{{ compactJSON({ target: plan.target, parameters: plan.parameters, intended_state: plan.intended_state, preconditions: plan.preconditions, verification_intent: plan.verification_intent }) }}</pre></details>
        <button v-if="plan.status === 'proposed'" type="button" @click="openAuthority(plan)">审查 Operation Plan</button>
        <form v-if="authoritySubjectID === plan.id" @submit.prevent="authorizePlan(plan)"><label :for="authorityInputID(plan.id)">授权理由</label><textarea :id="authorityInputID(plan.id)" v-model="authorityReason" name="authorization_reason" autocomplete="off" maxlength="1024" rows="3"></textarea><button type="submit" :disabled="authorityReason.trim().length < 2 || store.mutating">授权 exact Plan</button></form>
      </article>
    </section>

    <section class="inspector-section">
      <header><div><CheckCircle2 :size="16" aria-hidden="true" /><h2>Owner Knowledge</h2></div><span>{{ store.knowledge.length }}</span></header>
      <div v-if="!store.knowledge.length" class="inspector-empty">尚无 Owner-confirmed Knowledge。</div>
      <article v-for="item in store.knowledge" :key="item.id" class="knowledge-row">
        <div><strong>{{ item.title }}</strong><span :data-status="item.status">{{ item.status }}</span></div>
        <p>{{ item.current_revision.content }}</p>
        <code translate="no">revision {{ item.current_revision.revision }} · {{ item.current_revision.id }}</code>
        <button type="button" :disabled="store.mutating" @click="store.setKnowledgeStatus(item, item.status === 'active' ? 'disabled' : 'active')">{{ item.status === "active" ? "禁用检索" : "启用新 revision" }}</button>
      </article>
    </section>

    <section class="inspector-section">
      <header><div><BookOpenText :size="16" aria-hidden="true" /><h2>Runbook Guidance</h2></div><span>{{ store.runbooks.length }}</span></header>
      <div v-if="!store.runbooks.length" class="inspector-empty">当前 Git source 没有可用 Runbook。</div>
      <article v-for="item in store.runbooks" :key="item.id" class="runbook-row">
        <div><strong>{{ item.title }}</strong><ExternalLink :size="13" aria-hidden="true" /></div>
        <code translate="no">{{ item.path }}</code><small translate="no">{{ item.revision }}</small>
      </article>
    </section>
  </aside>
</template>

<style scoped>
.agent-inspector { min-width: 0; min-height: 0; border-left: 1px solid var(--co-border-default); background: var(--co-bg-surface); overflow-y: auto; overscroll-behavior: contain; }
.inspector-section { padding: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.inspector-section > header, .inspector-section > header > div, .citation-row > div:first-child, .guidance-row > div:first-child, .knowledge-row > div:first-child, .runbook-row > div:first-child, .authority-record > header { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.inspector-section > header { min-height: 28px; justify-content: space-between; margin-bottom: var(--co-space-3); }
.inspector-section h2 { margin: 0; font-size: 12px; letter-spacing: 0; }
.inspector-section > header > span { color: var(--co-text-secondary); font-size: 9px; font-variant-numeric: tabular-nums; }
.context-drift { display: flex; align-items: flex-start; gap: var(--co-space-2); margin-bottom: var(--co-space-2); padding: var(--co-space-2); border: 1px solid var(--co-status-warning-border); border-radius: var(--co-radius-control); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); font-size: 10px; line-height: 1.45; }
.attach-context, .authority-record > button, .authority-record form button, .knowledge-row button { display: inline-flex; min-height: 32px; align-items: center; justify-content: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); cursor: pointer; font-size: 10px; font-weight: 750; }
.attach-context { width: 100%; margin-bottom: var(--co-space-3); color: var(--co-text-on-action); border-color: var(--co-action-primary); background: var(--co-action-primary); }
.attach-context:disabled, .authority-record button:disabled, .knowledge-row button:disabled { cursor: not-allowed; opacity: 0.55; }
.inspector-facts { display: grid; gap: var(--co-space-2); margin: 0; }
.inspector-facts div, .citation-row dl div { display: grid; min-width: 0; grid-template-columns: 84px minmax(0, 1fr); gap: var(--co-space-2); }
.inspector-facts dt, .citation-row dt { color: var(--co-text-secondary); font-size: 9px; }
.inspector-facts dd, .citation-row dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; overflow-wrap: anywhere; }
.inspector-empty { padding: var(--co-space-4) 0; color: var(--co-text-secondary); font-size: 10px; text-align: center; }
.citation-row, .guidance-row, .authority-record, .knowledge-row, .runbook-row { min-width: 0; padding: var(--co-space-3) 0; border-top: 1px solid var(--co-border-default); }
.citation-row:first-of-type, .guidance-row:first-of-type { border-top: 0; }
.citation-row > div:first-child, .guidance-row > div:first-child, .knowledge-row > div:first-child { justify-content: space-between; }
.citation-row strong, .guidance-row strong, .knowledge-row strong, .runbook-row strong, .authority-record strong { min-width: 0; overflow: hidden; color: var(--co-text-primary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.citation-row > div > span, .guidance-row > div > span, .knowledge-row > div > span, .authority-record header span { padding: 2px 5px; border-radius: 4px; color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 8px; text-transform: uppercase; }
.guidance-row .stale, .knowledge-row span[data-status="disabled"] { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.citation-row p, .guidance-row p, .authority-record p, .knowledge-row p { margin: var(--co-space-2) 0; color: var(--co-text-secondary); font-size: 10px; line-height: 1.5; overflow-wrap: anywhere; }
.citation-row dl { display: grid; gap: 4px; margin: 0; }
.guidance-row code, .authority-record > code, .knowledge-row code, .runbook-row code, .runbook-row small { display: block; max-width: 100%; overflow: hidden; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }
.authority-levels { display: grid; gap: 4px; margin: 0 0 var(--co-space-3); padding: 0; list-style: none; }
.authority-levels li { display: grid; grid-template-columns: 28px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); min-height: 38px; }
.authority-levels li > span { display: grid; width: 28px; height: 28px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-subtle); }
.authority-levels li > div { display: grid; min-width: 0; }
.authority-levels strong { color: var(--co-text-primary); font-size: 10px; }
.authority-levels small, .authority-levels i { color: var(--co-text-secondary); font-size: 8px; font-style: normal; }
.authority-record > header span { margin-left: auto; }
.authority-record.high-impact { border-left: 2px solid var(--co-status-warning-border); padding-left: var(--co-space-3); }
.authority-record details { margin: var(--co-space-2) 0; }
.authority-record summary { color: var(--co-action-primary); cursor: pointer; font-size: 9px; }
.authority-record pre { max-height: 180px; margin: var(--co-space-2) 0 0; padding: var(--co-space-2); overflow: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-canvas); font-size: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.authority-record form { display: grid; gap: var(--co-space-2); margin-top: var(--co-space-2); }
.authority-record form label { color: var(--co-text-secondary); font-size: 9px; font-weight: 700; }
.authority-record textarea { width: 100%; resize: vertical; padding: var(--co-space-2); border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-canvas); font-size: 10px; }
.authority-record form button { color: var(--co-text-on-action); border-color: var(--co-action-primary); background: var(--co-action-primary); }
.knowledge-row button { margin-top: var(--co-space-2); }
.runbook-row > div { justify-content: space-between; }
.runbook-row small { margin-top: 4px; }
.is-compact .guidance-section, .is-compact .authority-section { display: none; }
@media (max-width: 767px) { .agent-inspector { border-left: 0; } }
</style>
