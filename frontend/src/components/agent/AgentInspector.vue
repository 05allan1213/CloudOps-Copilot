<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, onBeforeUnmount, ref } from "vue";

import type { ActionCard, AgentEvidenceCitation, OperationPlan } from "../../api/agent";
import { COPY_FEEDBACK_DURATION_MS } from "../../composables/useCopyFeedback";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";
import CopyFeedbackButton from "../workspace/CopyFeedbackButton.vue";
import WorkspaceTechnicalDetails, { type TechnicalDetailField } from "../workspace/WorkspaceTechnicalDetails.vue";

type AuthoritySelection = { kind: "card"; value: ActionCard } | { kind: "plan"; value: OperationPlan };

const props = defineProps<{ compact?: boolean; collapsed?: boolean }>();
const emit = defineEmits<{ "toggle-collapse": [] }>();
const store = useAgentWorkspaceStore();
const authoritySelection = ref<AuthoritySelection | null>(null);
const authorityReason = ref("");
const inspectorTab = ref<"context" | "evidence" | "authority">("context");
const evidenceScroll = ref<HTMLDivElement | null>(null);
const copiedEvidenceID = ref("");
let copyStatusTimer: ReturnType<typeof setTimeout> | undefined;

const run = computed(() => store.selectedRun);
const evidence = computed(() => run.value?.evidence_citations ?? []);
const guidance = computed(() => run.value?.guidance_citations ?? []);
const cards = computed(() => run.value?.action_cards ?? []);
const plans = computed(() => run.value?.operation_plans ?? []);
const inspectorTabs = computed(() => [
  { label: "上下文", value: "context", icon: "i-lucide-archive" },
  { label: `Evidence ${evidence.value.length}`, value: "evidence", icon: "i-lucide-database" },
  { label: `权限 ${cards.value.length + plans.value.length}`, value: "authority", icon: "i-lucide-shield-check" },
]);
const instanceID = computed(() => props.compact ? "global-agent-inspector" : "agent-inspector");
const contextResource = computed(() => store.activeSnapshot?.resource_refs.map((item) => `${item.kind}/${item.name}`).join(", ") || "未提供资源");
const contextScope = computed(() => store.activeSnapshot
  ? `${store.activeSnapshot.scope.cluster_id} / ${store.activeSnapshot.scope.environment}`
  : store.consultation ? `${store.consultation.scope.cluster_id} / ${store.consultation.scope.environment}` : "未提供");
const contextTime = computed(() => store.activeSnapshot
  ? `${formatCompactUTC(store.activeSnapshot.time_range.from)} — ${formatCompactUTC(store.activeSnapshot.time_range.to)} UTC`
  : "未提供");
const contextBoundary = computed(() => store.activeSnapshot
  ? `${store.activeSnapshot.evidence_refs.length} Evidence · ${store.activeSnapshot.query_execution_refs.length} Query`
  : `${evidence.value.length} Evidence`);
const contextStatus = computed(() => store.contextMismatch ? "页面上下文已变化，旧 Snapshot 保持不可变" : "当前不可变 Snapshot");
const contextTechnicalFields = computed<TechnicalDetailField[]>(() => {
  const snapshot = store.activeSnapshot;
  if (snapshot) {
    return [
      { label: "Snapshot ID", value: snapshot.id, code: true, copyValue: snapshot.id },
      { label: "Configuration Revision", value: snapshot.configuration_revision_id, code: true, copyValue: snapshot.configuration_revision_id },
      { label: "Incident ID", value: run.value?.incident_id || "无关联事件", code: Boolean(run.value?.incident_id), copyValue: run.value?.incident_id || undefined },
      { label: "Alert ID", value: run.value?.alert_id || "无关联事件", code: Boolean(run.value?.alert_id), copyValue: run.value?.alert_id || undefined },
      { label: "From UTC", value: formatUTC(snapshot.time_range.from), code: true, copyValue: snapshot.time_range.from },
      { label: "To UTC", value: formatUTC(snapshot.time_range.to), code: true, copyValue: snapshot.time_range.to },
      { label: "Content hash", value: snapshot.content_hash, code: true, copyValue: snapshot.content_hash },
    ];
  }
  if (!run.value) return [];
  return [
    { label: "Snapshot ID", value: run.value.context_snapshot_id, code: true, copyValue: run.value.context_snapshot_id },
    { label: "Configuration Revision", value: run.value.configuration_revision_id, code: true, copyValue: run.value.configuration_revision_id },
    { label: "Incident ID", value: run.value.incident_id || "无关联事件", code: Boolean(run.value.incident_id), copyValue: run.value.incident_id || undefined },
    { label: "Alert ID", value: run.value.alert_id || "无关联事件", code: Boolean(run.value.alert_id), copyValue: run.value.alert_id || undefined },
  ];
});
const evidenceVirtualized = computed(() => evidence.value.length > 40);
const evidenceVirtualizer = useVirtualizer<HTMLDivElement, HTMLElement>(computed(() => ({
  count: evidenceVirtualized.value ? evidence.value.length : 0,
  getScrollElement: () => evidenceScroll.value,
  estimateSize: () => 164,
  overscan: 8,
  getItemKey: (index: number) => evidence.value[index]?.id ?? index,
})));
const evidenceRows = computed(() => evidenceVirtualized.value
  ? evidenceVirtualizer.value.getVirtualItems().map((item) => ({ item: evidence.value[item.index], index: item.index, start: item.start }))
  : evidence.value.map((item, index) => ({ item, index, start: 0 })));
const evidenceTotalSize = computed(() => evidenceVirtualized.value ? evidenceVirtualizer.value.getTotalSize() : 0);
const authoritySubject = computed(() => authoritySelection.value?.value ?? null);
const authorityKindLabel = computed(() => authoritySelection.value?.kind === "plan" ? "Operation Plan" : "Action Card");

function formatUTC(value?: string): string {
  if (!value) return "未提供";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
}

function formatCompactUTC(value?: string): string {
  if (!value) return "未提供";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString().slice(5, 16).replace("T", " ");
}

function evidenceTechnicalFields(item: AgentEvidenceCitation): TechnicalDetailField[] {
  return [
    { label: "Evidence ID", value: item.evidence_id, code: true, copyValue: item.evidence_id },
    { label: "Query Execution", value: item.query_execution_id || "未提供", code: Boolean(item.query_execution_id), copyValue: item.query_execution_id || undefined },
    { label: "Configuration Revision", value: item.configuration_revision_id, code: true, copyValue: item.configuration_revision_id },
    { label: "Observed UTC", value: formatUTC(item.observed_at || item.collected_at), code: true, copyValue: item.observed_at || item.collected_at },
    { label: "Content hash", value: item.content_hash, code: true, copyValue: item.content_hash },
  ];
}

function selectInspectorTab(value: string | number) {
  if (value === "context" || value === "evidence" || value === "authority") inspectorTab.value = value;
}

function compactJSON(value: unknown): string {
  try {
    return JSON.stringify(value);
  } catch {
    return "{}";
  }
}

function prettyJSON(value: unknown): string {
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return "{}";
  }
}

function statusColor(value: string): "success" | "info" | "warning" | "error" | "neutral" {
  if (value === "authorized" || value === "active" || value === "completed") return "success";
  if (value === "proposed" || value === "pending" || value === "running") return "info";
  if (value === "expired" || value === "disabled") return "warning";
  if (value === "failed" || value === "cancelled" || value === "rejected") return "error";
  return "neutral";
}

function openAuthority(kind: "card" | "plan", subject: ActionCard | OperationPlan) {
  authoritySelection.value = kind === "card"
    ? { kind, value: subject as ActionCard }
    : { kind, value: subject as OperationPlan };
  authorityReason.value = "已核对 exact target、parameters、preconditions、expiry 与 content hash";
}

async function confirmAuthority() {
  const selection = authoritySelection.value;
  if (!selection || authorityReason.value.trim().length < 2) return;
  if (selection.kind === "card") await store.authorizeCard(selection.value, authorityReason.value.trim());
  else await store.authorizePlan(selection.value, authorityReason.value.trim());
  if (!store.error) authoritySelection.value = null;
}

function updateAuthorityOpen(value: boolean) {
  if (!value && !store.mutating) authoritySelection.value = null;
}

function measureEvidence(element: Element | ComponentPublicInstance | null) {
  const resolved = element instanceof Element ? element : element?.$el;
  if (resolved instanceof HTMLElement && evidenceVirtualized.value) evidenceVirtualizer.value.measureElement(resolved);
}

function evidenceStyle(start: number) {
  return evidenceVirtualized.value ? { transform: `translateY(${start}px)` } : undefined;
}

function reportEvidenceCopied(item: AgentEvidenceCitation) {
  copiedEvidenceID.value = item.id;
  if (copyStatusTimer !== undefined) clearTimeout(copyStatusTimer);
  copyStatusTimer = setTimeout(() => {
    if (copiedEvidenceID.value === item.id) copiedEvidenceID.value = "";
  }, COPY_FEEDBACK_DURATION_MS);
}

onBeforeUnmount(() => {
  if (copyStatusTimer !== undefined) clearTimeout(copyStatusTimer);
});
</script>

<template>
  <aside
    v-if="collapsed && !compact"
    class="agent-inspector-rail"
    aria-label="已折叠 Agent Inspector"
  >
    <UTooltip
      text="展开 Agent Inspector"
      :content="{ side: 'left' }"
    >
      <UButton
        color="neutral"
        variant="ghost"
        icon="i-lucide-panel-right-open"
        square
        size="xs"
        aria-label="展开 Agent Inspector"
        data-testid="agent-inspector-expand"
        @click="emit('toggle-collapse')"
      />
    </UTooltip>
    <span aria-hidden="true">Inspector</span>
  </aside>

  <aside
    v-else
    class="agent-inspector"
    :class="{ 'is-compact': compact }"
    aria-label="Agent Context、Evidence 与 Authority"
  >
    <header
      v-if="!compact"
      class="inspector-toolbar"
    >
      <div><span class="section-kicker">Inspector</span><strong>上下文与权限</strong></div>
      <UTooltip text="折叠 Agent Inspector">
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-panel-right-close"
          square
          aria-label="折叠 Agent Inspector"
          data-testid="agent-inspector-collapse"
          @click="emit('toggle-collapse')"
        />
      </UTooltip>
    </header>

    <UTabs
      v-if="!compact"
      class="inspector-tabs"
      :model-value="inspectorTab"
      :items="inspectorTabs"
      :content="false"
      color="primary"
      variant="pill"
      size="xs"
      aria-label="Agent Inspector 视图"
      @update:model-value="selectInspectorTab"
    />

    <section
      v-if="compact || inspectorTab === 'context'"
      class="inspector-section context-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-archive"
            aria-hidden="true"
          /><h2>Context Snapshot</h2>
        </div>
        <UBadge
          color="neutral"
          variant="subtle"
          size="sm"
          :label="String(store.consultation?.snapshots.length || (run ? 1 : 0))"
        />
      </header>
      <UButton
        v-if="store.consultation && store.contextMismatch"
        class="attach-context"
        color="primary"
        variant="outline"
        icon="i-lucide-refresh-cw"
        label="附加当前上下文"
        block
        :loading="store.mutating"
        :disabled="!store.currentContext || store.currentContext.route !== store.route"
        @click="store.attachCurrentContext"
      />
      <dl
        v-if="store.activeSnapshot || run"
        class="context-summary-facts"
      >
        <div class="context-primary-fact">
          <dt>当前对象</dt><dd>{{ contextResource }}</dd>
        </div>
        <div><dt>范围</dt><dd>{{ contextScope }}</dd></div>
        <div><dt>时间</dt><dd>{{ contextTime }}</dd></div>
        <div>
          <dt>状态</dt><dd :class="{ 'is-warning': store.contextMismatch }">
            {{ contextStatus }}
          </dd>
        </div>
        <div><dt>Evidence 边界</dt><dd>{{ contextBoundary }}</dd></div>
      </dl>
      <div
        v-else
        class="inspector-empty"
      >
        未选择 Agent 记录。
      </div>
      <WorkspaceTechnicalDetails
        v-if="contextTechnicalFields.length"
        class="context-technical-details"
        title="技术详情"
        description="Snapshot ID、配置版本、完整 UTC 与 Content hash"
        :fields="contextTechnicalFields"
      />
    </section>

    <section
      v-if="compact || inspectorTab === 'evidence'"
      class="inspector-section evidence-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-database"
            aria-hidden="true"
          /><h2>Current Evidence</h2>
        </div>
        <UBadge
          color="info"
          variant="subtle"
          size="sm"
          :label="String(evidence.length)"
        />
      </header>
      <div
        v-if="!evidence.length"
        class="inspector-empty"
      >
        当前运行没有可引用的 Evidence。
      </div>
      <div
        v-else
        ref="evidenceScroll"
        class="evidence-viewport"
        :class="{ 'is-virtualized': evidenceVirtualized }"
        role="feed"
        aria-label="Agent Evidence citations"
      >
        <div
          class="evidence-list"
          :style="evidenceVirtualized ? { height: `${evidenceTotalSize}px` } : undefined"
        >
          <article
            v-for="row in evidenceRows"
            :key="row.item.id"
            :ref="measureEvidence"
            class="citation-row"
            :data-index="row.index"
            :style="evidenceStyle(row.start)"
            :aria-posinset="row.index + 1"
            :aria-setsize="evidence.length"
          >
            <header>
              <strong>{{ row.item.source }}</strong>
              <UBadge
                color="neutral"
                variant="subtle"
                size="sm"
                :label="row.item.use"
              />
              <CopyFeedbackButton
                :value="prettyJSON(row.item)"
                :label="`复制 Evidence ${row.item.evidence_id} 完整内容`"
                success-label="完整 Evidence 已复制"
                @copied="reportEvidenceCopied(row.item)"
              />
            </header>
            <p>{{ row.item.summary }}</p>
            <WorkspaceTechnicalDetails
              title="Evidence 技术详情"
              description="完整身份、采集时间与内容哈希"
              :fields="evidenceTechnicalFields(row.item)"
            />
          </article>
        </div>
      </div>
    </section>

    <section
      v-if="!compact && inspectorTab === 'evidence'"
      class="inspector-section guidance-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-lightbulb"
            aria-hidden="true"
          /><h2>Guidance Citations</h2>
        </div>
        <UBadge
          color="neutral"
          variant="subtle"
          size="sm"
          :label="String(guidance.length)"
        />
      </header>
      <div
        v-if="!guidance.length"
        class="inspector-empty"
      >
        本次运行未检索 Knowledge 或 Runbook。
      </div>
      <article
        v-for="item in guidance"
        :key="item.id"
        class="guidance-row"
      >
        <header>
          <strong>{{ item.type }}</strong><UBadge
            :color="item.stale ? 'warning' : 'success'"
            variant="subtle"
            size="sm"
            :label="item.stale ? 'stale' : `${item.age_seconds.toLocaleString('zh-CN')} s`"
          />
        </header>
        <p>{{ item.title }}</p>
        <code translate="no">revision {{ item.revision }} · {{ item.revision_id }}</code>
        <time :datetime="item.created_at">{{ formatUTC(item.created_at) }}</time>
      </article>
    </section>

    <section
      v-if="!compact && inspectorTab === 'authority'"
      class="inspector-section authority-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-shield-check"
            aria-hidden="true"
          /><h2>Authority</h2>
        </div>
        <UBadge
          color="neutral"
          variant="subtle"
          size="sm"
          label="3 levels"
        />
      </header>
      <ol class="authority-levels">
        <li>
          <span><UIcon
            name="i-lucide-link-2"
            aria-hidden="true"
          /></span><div><strong>Read</strong><small>bounded Provider tools</small></div><UBadge
            color="success"
            variant="subtle"
            size="sm"
            label="active"
          />
        </li>
        <li>
          <span><UIcon
            name="i-lucide-key-round"
            aria-hidden="true"
          /></span><div><strong>Reversible</strong><small>exact Action Card</small></div><UBadge
            color="neutral"
            variant="subtle"
            size="sm"
            :label="String(cards.length)"
          />
        </li>
        <li>
          <span><UIcon
            name="i-lucide-lock-keyhole"
            aria-hidden="true"
          /></span><div><strong>High impact</strong><small>immutable Operation Plan</small></div><UBadge
            color="neutral"
            variant="subtle"
            size="sm"
            :label="String(plans.length)"
          />
        </li>
      </ol>

      <article
        v-for="card in cards"
        :key="card.id"
        class="authority-record"
      >
        <header>
          <UIcon
            name="i-lucide-file-key-2"
            aria-hidden="true"
          /><strong>{{ card.action_type }}</strong><UBadge
            :color="statusColor(card.status)"
            variant="subtle"
            size="sm"
            :label="card.status"
          />
        </header>
        <p>{{ card.risk }}</p>
        <dl class="authority-facts">
          <div>
            <dt>Exact hash</dt><dd translate="no">
              {{ card.content_hash }}
            </dd>
          </div>
          <div>
            <dt>Target</dt><dd translate="no">
              {{ compactJSON(card.target) }}
            </dd>
          </div>
          <div>
            <dt>Preconditions</dt><dd translate="no">
              {{ compactJSON(card.preconditions) }}
            </dd>
          </div>
          <div><dt>Expires UTC</dt><dd>{{ formatUTC(card.expires_at) }}</dd></div>
          <template v-if="card.authorization">
            <div>
              <dt>Authorized by</dt><dd translate="no">
                {{ card.authorization.authorized_by }}
              </dd>
            </div>
            <div>
              <dt>Authorized hash</dt><dd translate="no">
                {{ card.authorization.authorized_content_hash }}
              </dd>
            </div>
            <div><dt>Authorized UTC</dt><dd>{{ formatUTC(card.authorization.created_at) }}</dd></div>
            <div><dt>Authorization expiry</dt><dd>{{ formatUTC(card.authorization.expires_at) }}</dd></div>
          </template>
        </dl>
        <details><summary>Exact payload</summary><pre>{{ prettyJSON({ target: card.target, parameters: card.parameters, preconditions: card.preconditions }) }}</pre></details>
        <UButton
          v-if="card.status === 'proposed'"
          color="primary"
          variant="outline"
          icon="i-lucide-shield-check"
          label="审查 exact Action Card"
          block
          @click="openAuthority('card', card)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-external-link"
          trailing
          label="在 DevOps 审查 / 执行"
          block
          :to="{ name: 'devops', query: { subject: card.id } }"
        />
      </article>

      <article
        v-for="plan in plans"
        :key="plan.id"
        class="authority-record high-impact"
      >
        <header>
          <UIcon
            name="i-lucide-lock-keyhole"
            aria-hidden="true"
          /><strong>{{ plan.operation_type }}</strong><UBadge
            :color="statusColor(plan.status)"
            variant="subtle"
            size="sm"
            :label="plan.status"
          />
        </header>
        <p>{{ plan.risk }}</p>
        <dl class="authority-facts">
          <div>
            <dt>Exact hash</dt><dd translate="no">
              {{ plan.content_hash }}
            </dd>
          </div>
          <div>
            <dt>Target</dt><dd translate="no">
              {{ compactJSON(plan.target) }}
            </dd>
          </div>
          <div>
            <dt>Preconditions</dt><dd translate="no">
              {{ compactJSON(plan.preconditions) }}
            </dd>
          </div>
          <div><dt>Expires UTC</dt><dd>{{ formatUTC(plan.expires_at) }}</dd></div>
          <template v-if="plan.authorization">
            <div>
              <dt>Authorized by</dt><dd translate="no">
                {{ plan.authorization.authorized_by }}
              </dd>
            </div>
            <div>
              <dt>Authorized hash</dt><dd translate="no">
                {{ plan.authorization.authorized_content_hash }}
              </dd>
            </div>
            <div><dt>Authorized UTC</dt><dd>{{ formatUTC(plan.authorization.created_at) }}</dd></div>
            <div><dt>Authorization expiry</dt><dd>{{ formatUTC(plan.authorization.expires_at) }}</dd></div>
          </template>
        </dl>
        <details><summary>Exact immutable plan</summary><pre>{{ prettyJSON({ target: plan.target, parameters: plan.parameters, intended_state: plan.intended_state, preconditions: plan.preconditions, verification_intent: plan.verification_intent }) }}</pre></details>
        <UButton
          v-if="plan.status === 'proposed'"
          color="warning"
          variant="outline"
          icon="i-lucide-lock-keyhole"
          label="审查 Operation Plan"
          block
          @click="openAuthority('plan', plan)"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-external-link"
          trailing
          label="在 DevOps 审查 / 执行"
          block
          :to="{ name: 'devops', query: { subject: plan.id } }"
        />
      </article>
    </section>

    <section
      v-if="!compact && inspectorTab === 'evidence'"
      class="inspector-section knowledge-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-circle-check"
            aria-hidden="true"
          /><h2>Owner Knowledge</h2>
        </div>
        <UBadge
          color="neutral"
          variant="subtle"
          size="sm"
          :label="String(store.knowledge.length)"
        />
      </header>
      <div
        v-if="!store.knowledge.length"
        class="inspector-empty"
      >
        尚无 Owner-confirmed Knowledge。
      </div>
      <article
        v-for="item in store.knowledge"
        :key="item.id"
        class="knowledge-row"
      >
        <header>
          <strong>{{ item.title }}</strong><UBadge
            :color="statusColor(item.status)"
            variant="subtle"
            size="sm"
            :label="item.status"
          />
        </header>
        <p>{{ item.current_revision.content }}</p>
        <code translate="no">revision {{ item.current_revision.revision }} · {{ item.current_revision.id }}</code>
        <time :datetime="item.current_revision.created_at">{{ formatUTC(item.current_revision.created_at) }}</time>
        <UButton
          color="neutral"
          variant="outline"
          :icon="item.status === 'active' ? 'i-lucide-ban' : 'i-lucide-circle-check'"
          :label="item.status === 'active' ? '禁用检索' : '启用新 revision'"
          size="xs"
          :loading="store.mutating"
          @click="store.setKnowledgeStatus(item, item.status === 'active' ? 'disabled' : 'active')"
        />
      </article>
    </section>

    <section
      v-if="!compact && inspectorTab === 'evidence'"
      class="inspector-section runbook-section"
    >
      <header>
        <div>
          <UIcon
            name="i-lucide-book-open-text"
            aria-hidden="true"
          /><h2>Runbook Guidance</h2>
        </div>
        <UBadge
          color="neutral"
          variant="subtle"
          size="sm"
          :label="String(store.runbooks.length)"
        />
      </header>
      <div
        v-if="!store.runbooks.length"
        class="inspector-empty"
      >
        当前 Git source 没有可用 Runbook。
      </div>
      <article
        v-for="item in store.runbooks"
        :key="item.id"
        class="runbook-row"
      >
        <header>
          <strong>{{ item.title }}</strong><UIcon
            name="i-lucide-external-link"
            aria-hidden="true"
          />
        </header>
        <code translate="no">{{ item.path }}</code>
        <small translate="no">{{ item.revision }}</small>
        <time :datetime="item.modified_at">{{ formatUTC(item.modified_at) }}</time>
      </article>
    </section>

    <UModal
      :open="Boolean(authoritySelection)"
      :title="`授权 exact ${authorityKindLabel}`"
      description="本步骤只创建精确授权记录，不执行操作，也不证明 Delivery 或 Verification。"
      :close="false"
      :dismissible="!store.mutating"
      :ui="{ content: 'agent-authority-modal', body: 'agent-authority-modal-body' }"
      @update:open="updateAuthorityOpen"
    >
      <template #body>
        <div
          v-if="authoritySubject"
          class="authority-modal-content"
        >
          <UAlert
            color="warning"
            variant="soft"
            icon="i-lucide-shield-alert"
            title="授权不等于执行"
            description="请核对 exact hash、target、preconditions 与 expiry；执行仍属于独立受控阶段。"
          />
          <dl class="authority-modal-facts">
            <div>
              <dt>Subject</dt><dd translate="no">
                {{ authoritySubject.id }}
              </dd>
            </div>
            <div>
              <dt>Exact hash</dt><dd translate="no">
                {{ authoritySubject.content_hash }}
              </dd>
            </div>
            <div><dt>Target</dt><dd><pre>{{ prettyJSON(authoritySubject.target) }}</pre></dd></div>
            <div><dt>Preconditions</dt><dd><pre>{{ prettyJSON(authoritySubject.preconditions) }}</pre></dd></div>
            <div><dt>Expires UTC</dt><dd>{{ formatUTC(authoritySubject.expires_at) }}</dd></div>
          </dl>
          <UFormField
            label="授权理由"
            :name="`${instanceID}-authority-reason`"
            required
          >
            <UTextarea
              :id="`${instanceID}-authority-reason`"
              v-model="authorityReason"
              name="authorization_reason"
              autocomplete="off"
              :maxlength="1024"
              :rows="4"
              autofocus
            />
          </UFormField>
        </div>
      </template>
      <template #footer>
        <div class="modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-x"
            label="取消"
            :disabled="store.mutating"
            @click="authoritySelection = null"
          />
          <UButton
            color="warning"
            icon="i-lucide-shield-check"
            label="确认 exact hash"
            :loading="store.mutating"
            :disabled="authorityReason.trim().length < 2"
            @click="confirmAuthority"
          />
        </div>
      </template>
    </UModal>

    <p
      class="copy-status"
      aria-live="polite"
    >
      {{ copiedEvidenceID ? "完整 Evidence 已复制" : "" }}
    </p>
  </aside>
</template>

<style scoped>
.agent-inspector {
  position: relative;
  min-width: 0;
  min-height: 0;
  border-left: 1px solid var(--co-border-default);
  background: linear-gradient(180deg, color-mix(in srgb, var(--co-bg-surface) 72%, var(--co-bg-floating)), var(--co-bg-surface));
  overflow-y: auto;
  overscroll-behavior: contain;
}
.inspector-toolbar {
  position: sticky;
  z-index: 2;
  top: 0;
  display: flex;
  min-height: 62px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-2);
  padding: 11px 12px 10px 15px;
  border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  background: color-mix(in srgb, var(--co-bg-floating) 84%, transparent);
  backdrop-filter: blur(var(--co-glass-blur));
}
.inspector-toolbar > div { display: grid; gap: 2px; }
.inspector-toolbar strong { font-size: 14px; font-weight: 720; }
.inspector-toolbar :deep(button) { width: 30px; min-width: 30px; height: 30px; border-radius: 8px; }
.section-kicker { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; }
.inspector-tabs { position: sticky; z-index: 2; top: 62px; margin: 8px 10px 0; padding: 3px; border: 1px solid var(--co-border-default); border-radius: 9px; background: color-mix(in srgb, var(--co-bg-floating) 88%, transparent); backdrop-filter: blur(var(--co-glass-blur)); }
.inspector-section { position: relative; padding: 14px 15px; border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 68%, transparent); }
.authority-section { background: color-mix(in srgb, var(--co-status-warning-bg) 12%, transparent); }
.inspector-section > header,
.inspector-section > header > div,
.citation-row > header,
.guidance-row > header,
.knowledge-row > header,
.runbook-row > header,
.authority-record > header { display: flex; min-width: 0; align-items: center; gap: 7px; }
.inspector-section > header { min-height: 28px; justify-content: space-between; margin-bottom: 10px; }
.inspector-section > header > div :deep(svg), .authority-record > header :deep(svg) { width: 14px; height: 14px; color: var(--co-viz-live); }
.inspector-section h2 { margin: 0; color: var(--co-text-primary); font-size: 11px; font-weight: 760; letter-spacing: 0; text-transform: uppercase; }
.attach-context { margin-bottom: var(--co-space-3); }

.inspector-facts, .citation-row dl, .authority-facts { display: grid; gap: 5px; margin: 0; }
.inspector-facts { padding: 9px 0; }
.inspector-facts div, .citation-row dl div, .authority-facts div { display: grid; min-width: 0; grid-template-columns: 88px minmax(0, 1fr); gap: var(--co-space-2); }
.inspector-facts dt, .citation-row dt, .authority-facts dt { color: var(--co-text-muted); font-size: 8px; }
.inspector-facts dd, .citation-row dd, .authority-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 8px; line-height: 1.45; overflow-wrap: anywhere; }
.inspector-empty { display: flex; min-height: 58px; align-items: center; gap: 9px; padding: 13px 8px; color: var(--co-text-muted); font-size: 9px; line-height: 1.5; text-align: left; }
.inspector-empty::before { width: 5px; height: 5px; flex: 0 0 5px; border-radius: 50%; background: var(--co-border-strong); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-border-default) 44%, transparent); content: ""; }
.context-summary-facts { display: grid; gap: 0; margin: 0 0 12px; }
.context-summary-facts > div { display: grid; min-width: 0; grid-template-columns: 82px minmax(0, 1fr); gap: 10px; padding: 8px 0; border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 62%, transparent); }
.context-summary-facts dt { color: var(--co-text-muted); font-size: 9px; }
.context-summary-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-size: 10px; line-height: 1.45; overflow-wrap: anywhere; }
.context-summary-facts .context-primary-fact { grid-template-columns: 1fr; gap: 3px; padding-top: 2px; }
.context-primary-fact dd { color: var(--co-text-primary); font-size: 12px; font-weight: 700; }
.context-summary-facts dd.is-warning { color: var(--co-status-warning-fg); }
.context-technical-details { margin-top: 4px; }
.context-technical-details :deep(.workspace-technical-details-trigger) { min-height: 42px; }
.context-technical-details :deep(.workspace-technical-details-trigger strong) { font-size: 10px; }
.context-technical-details :deep(.workspace-technical-details-trigger small) { font-size: 8px; }
.context-technical-details :deep(.workspace-technical-details-content) { padding-inline: 9px; }
.context-technical-details :deep(.workspace-technical-details-content dl > div) { grid-template-columns: 92px minmax(0, 1fr) auto; gap: 6px; }
.context-technical-details :deep(dt), .context-technical-details :deep(dd) { font-size: 8px; }

.evidence-viewport { min-width: 0; }
.evidence-viewport.is-virtualized { height: min(42vh, 360px); overflow-y: auto; overscroll-behavior: contain; }
.evidence-list { position: relative; display: grid; min-width: 0; gap: 0; }
.citation-row { min-width: 0; padding: 10px 0 12px; border-bottom: 1px solid var(--co-border-default); }
.evidence-viewport.is-virtualized .citation-row { position: absolute; top: 0; left: 0; width: 100%; }
.citation-row > header { justify-content: flex-start; }
.citation-row > header strong { min-width: 0; overflow: hidden; color: var(--co-text-primary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.citation-row > header > :last-child { margin-left: auto; }
.citation-row p, .guidance-row p, .authority-record > p, .knowledge-row p { margin: 8px 0; color: var(--co-text-secondary); font-size: 10px; line-height: 1.55; overflow-wrap: anywhere; }

.guidance-row, .authority-record, .knowledge-row, .runbook-row { min-width: 0; margin-top: 0; padding: 10px 0 12px; border-bottom: 1px solid var(--co-border-default); }
.guidance-row > header, .knowledge-row > header, .runbook-row > header { justify-content: space-between; }
.guidance-row strong, .knowledge-row strong, .runbook-row strong, .authority-record strong { min-width: 0; overflow: hidden; color: var(--co-text-primary); font-size: 10px; font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.guidance-row code, .guidance-row time, .knowledge-row code, .knowledge-row time, .runbook-row code, .runbook-row small, .runbook-row time { display: block; max-width: 100%; margin-top: 3px; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }

.authority-levels { display: grid; gap: 5px; margin: 0 0 12px; padding: 0; list-style: none; }
.authority-levels li { display: grid; min-height: 42px; grid-template-columns: 30px minmax(0, 1fr) auto; align-items: center; gap: 9px; padding: 5px 0; border-bottom: 1px solid color-mix(in srgb, var(--co-border-default) 62%, transparent); }
.authority-levels li > span { display: grid; width: 30px; height: 30px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 8px; color: var(--co-text-secondary); background: var(--co-bg-floating); }
.authority-levels li:first-child > span { color: var(--co-status-success-fg); background: var(--co-viz-live-soft); }
.authority-levels li:last-child > span { color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.authority-levels li > span :deep(svg) { width: 13px; height: 13px; }
.authority-levels li > div { display: grid; min-width: 0; }
.authority-levels strong { color: var(--co-text-primary); font-size: 9px; }
.authority-levels small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 7px; }
.authority-record > header { justify-content: flex-start; }
.authority-record > header > :last-child { margin-left: auto; }
.authority-record.high-impact { padding-left: 9px; border-left: 2px solid var(--co-viz-amber); }
.authority-record details { margin: var(--co-space-2) 0; }
.authority-record summary { color: var(--co-action-primary); cursor: pointer; font-family: var(--co-font-mono); font-size: 8px; }
.authority-record pre, .authority-modal-facts pre { max-height: 180px; margin: var(--co-space-2) 0 0; padding: var(--co-space-2); overflow: auto; border: 1px solid var(--co-border-default); border-radius: 8px; color: var(--co-code-text); background: var(--co-code-bg); font-size: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.authority-record > :deep(a), .authority-record > :deep(button), .knowledge-row :deep(button) { margin-top: var(--co-space-2); }

.agent-inspector-rail { display: grid; min-width: 0; min-height: 0; grid-template-rows: 42px 1fr; justify-items: center; padding-top: 5px; border-left: 1px solid var(--co-border-default); background: color-mix(in srgb, var(--co-bg-surface) 82%, var(--co-bg-floating)); }
.agent-inspector-rail > span { align-self: start; margin-top: var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; letter-spacing: .1em; text-transform: uppercase; writing-mode: vertical-rl; }
.is-compact .inspector-section { padding: 12px; }
.is-compact .context-summary-facts { margin-bottom: 8px; }
.is-compact .context-technical-details { display: none; }
.copy-status { position: sticky; bottom: var(--co-space-2); margin: 0 var(--co-space-2); pointer-events: none; color: var(--co-status-success-fg); font-size: 9px; text-align: right; }

.authority-modal-content { display: grid; min-width: 0; gap: var(--co-space-4); }
.authority-modal-facts { display: grid; gap: var(--co-space-1); margin: 0; }
.authority-modal-facts > div { display: grid; min-width: 0; grid-template-columns: 116px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-1) 0; border-bottom: 1px solid var(--co-border-default); }
.authority-modal-facts dt { color: var(--co-text-muted); font-size: 10px; }
.authority-modal-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; overflow-wrap: anywhere; }
.authority-modal-facts pre { margin: 0; }
.modal-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
:global(.agent-authority-modal) { width: min(720px, calc(100vw - 32px)); }
:global(.agent-authority-modal-body) { min-height: 0; overflow-y: auto; }
</style>
