<script setup lang="ts">
import { useVirtualizer } from "@tanstack/vue-virtual";
import type { ComponentPublicInstance } from "vue";
import { computed, ref } from "vue";

import type { ActionCard, AgentEvidenceCitation, OperationPlan } from "../../api/agent";
import { useAgentWorkspaceStore } from "../../stores/agentWorkspace";

type AuthoritySelection = { kind: "card"; value: ActionCard } | { kind: "plan"; value: OperationPlan };

const props = defineProps<{ compact?: boolean; collapsed?: boolean }>();
const emit = defineEmits<{ "toggle-collapse": [] }>();
const store = useAgentWorkspaceStore();
const authoritySelection = ref<AuthoritySelection | null>(null);
const authorityReason = ref("");
const evidenceScroll = ref<HTMLDivElement | null>(null);
const copiedEvidenceID = ref("");

const run = computed(() => store.selectedRun);
const evidence = computed(() => run.value?.evidence_citations ?? []);
const guidance = computed(() => run.value?.guidance_citations ?? []);
const cards = computed(() => run.value?.action_cards ?? []);
const plans = computed(() => run.value?.operation_plans ?? []);
const instanceID = computed(() => props.compact ? "global-agent-inspector" : "agent-inspector");
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

async function copyEvidence(item: AgentEvidenceCitation) {
  const value = prettyJSON(item);
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
  copiedEvidenceID.value = item.id;
  window.setTimeout(() => { if (copiedEvidenceID.value === item.id) copiedEvidenceID.value = ""; }, 1200);
}
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

    <section class="inspector-section context-section">
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
      <UAlert
        v-if="store.consultation && store.contextMismatch"
        class="context-drift"
        color="warning"
        variant="soft"
        icon="i-lucide-triangle-alert"
        title="页面上下文已变化"
        description="当前页面上下文与会话 Snapshot 不同；旧 Snapshot 保持不可变。"
      />
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
        v-if="store.activeSnapshot"
        class="inspector-facts"
      >
        <div>
          <dt>Snapshot</dt><dd translate="no">
            {{ store.activeSnapshot.id }}
          </dd>
        </div>
        <div>
          <dt>Configuration</dt><dd translate="no">
            {{ store.activeSnapshot.configuration_revision_id }}
          </dd>
        </div>
        <div><dt>Scope</dt><dd>{{ store.activeSnapshot.scope.cluster_id }} / {{ store.activeSnapshot.scope.environment }}</dd></div>
        <div><dt>Namespace</dt><dd>{{ store.activeSnapshot.scope.namespaces.join(", ") }}</dd></div>
        <div><dt>Resource</dt><dd>{{ store.activeSnapshot.resource_refs.map((item) => `${item.kind}/${item.name}`).join(", ") || "未提供" }}</dd></div>
        <div>
          <dt>Incident</dt><dd translate="no">
            {{ run?.incident_id || "无关联事件" }}
          </dd>
        </div>
        <div>
          <dt>Alert</dt><dd translate="no">
            {{ run?.alert_id || "无关联事件" }}
          </dd>
        </div>
        <div><dt>From UTC</dt><dd>{{ formatUTC(store.activeSnapshot.time_range.from) }}</dd></div>
        <div><dt>To UTC</dt><dd>{{ formatUTC(store.activeSnapshot.time_range.to) }}</dd></div>
        <div><dt>Evidence</dt><dd>{{ store.activeSnapshot.evidence_refs.length }} retained / {{ store.activeSnapshot.query_execution_refs.length }} query</dd></div>
        <div>
          <dt>Content hash</dt><dd translate="no">
            {{ store.activeSnapshot.content_hash }}
          </dd>
        </div>
      </dl>
      <dl
        v-else-if="run"
        class="inspector-facts"
      >
        <div>
          <dt>Snapshot</dt><dd translate="no">
            {{ run.context_snapshot_id }}
          </dd>
        </div>
        <div>
          <dt>Configuration</dt><dd translate="no">
            {{ run.configuration_revision_id }}
          </dd>
        </div>
        <div>
          <dt>Incident</dt><dd translate="no">
            {{ run.incident_id || "无关联事件" }}
          </dd>
        </div>
        <div>
          <dt>Alert</dt><dd translate="no">
            {{ run.alert_id || "无关联事件" }}
          </dd>
        </div>
        <div><dt>来源</dt><dd>{{ store.selection === "investigation" ? "自动/上下文 Investigation" : "Consultation" }}</dd></div>
      </dl>
      <div
        v-else
        class="inspector-empty"
      >
        未选择 Agent 记录。
      </div>
    </section>

    <section class="inspector-section evidence-section">
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
              <UTooltip text="复制完整 Evidence">
                <UButton
                  color="neutral"
                  variant="ghost"
                  :icon="copiedEvidenceID === row.item.id ? 'i-lucide-copy-check' : 'i-lucide-copy'"
                  square
                  size="xs"
                  :aria-label="`复制 Evidence ${row.item.evidence_id} 完整内容`"
                  @click="copyEvidence(row.item)"
                />
              </UTooltip>
            </header>
            <p>{{ row.item.summary }}</p>
            <dl>
              <div>
                <dt>Evidence</dt><dd translate="no">
                  {{ row.item.evidence_id }}
                </dd>
              </div>
              <div><dt>Observed UTC</dt><dd>{{ formatUTC(row.item.observed_at || row.item.collected_at) }}</dd></div>
              <div v-if="row.item.query_execution_id">
                <dt>Query</dt><dd translate="no">
                  {{ row.item.query_execution_id }}
                </dd>
              </div>
              <div>
                <dt>Configuration</dt><dd translate="no">
                  {{ row.item.configuration_revision_id }}
                </dd>
              </div>
              <div>
                <dt>Content hash</dt><dd translate="no">
                  {{ row.item.content_hash }}
                </dd>
              </div>
            </dl>
          </article>
        </div>
      </div>
    </section>

    <section class="inspector-section guidance-section">
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

    <section class="inspector-section authority-section">
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

    <section class="inspector-section knowledge-section">
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

    <section class="inspector-section runbook-section">
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
.agent-inspector { position: relative; min-width: 0; min-height: 0; border-left: 1px solid var(--co-border-default); background: var(--co-bg-surface); overflow-y: auto; overscroll-behavior: contain; }
.inspector-toolbar { position: sticky; z-index: 2; top: 0; display: flex; min-height: 60px; align-items: center; justify-content: space-between; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); border-bottom: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.inspector-toolbar > div { display: grid; gap: 1px; }
.inspector-toolbar strong { font-size: 13px; }
.section-kicker { color: var(--co-text-muted); font-size: 9px; font-weight: 700; text-transform: uppercase; }
.inspector-section { padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.inspector-section > header, .inspector-section > header > div, .citation-row > header, .guidance-row > header, .knowledge-row > header, .runbook-row > header, .authority-record > header { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.inspector-section > header { min-height: 26px; justify-content: space-between; margin-bottom: var(--co-space-2); }
.inspector-section > header > div :deep(svg), .authority-record > header :deep(svg) { width: 15px; height: 15px; color: var(--co-action-primary); }
.inspector-section h2 { margin: 0; font-size: 11px; letter-spacing: 0; }
.context-drift { margin-bottom: var(--co-space-2); }
.attach-context { margin-bottom: var(--co-space-3); }

.inspector-facts, .citation-row dl, .authority-facts { display: grid; gap: var(--co-space-1); margin: 0; }
.inspector-facts div, .citation-row dl div, .authority-facts div { display: grid; min-width: 0; grid-template-columns: 88px minmax(0, 1fr); gap: var(--co-space-2); }
.inspector-facts dt, .citation-row dt, .authority-facts dt { color: var(--co-text-muted); font-size: 9px; }
.inspector-facts dd, .citation-row dd, .authority-facts dd { min-width: 0; margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; overflow-wrap: anywhere; }
.inspector-empty { padding: var(--co-space-4) 0; color: var(--co-text-muted); font-size: 10px; text-align: center; }

.evidence-viewport { min-width: 0; }
.evidence-viewport.is-virtualized { height: min(42vh, 360px); overflow-y: auto; overscroll-behavior: contain; }
.evidence-list { position: relative; min-width: 0; }
.citation-row { min-width: 0; padding: var(--co-space-3) 0; border-top: 1px solid var(--co-border-default); }
.evidence-viewport.is-virtualized .citation-row { position: absolute; top: 0; left: 0; width: 100%; }
.citation-row > header { justify-content: flex-start; }
.citation-row > header strong { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.citation-row > header > :last-child { margin-left: auto; }
.citation-row p, .guidance-row p, .authority-record > p, .knowledge-row p { margin: var(--co-space-2) 0; color: var(--co-text-secondary); font-size: 10px; line-height: 1.5; overflow-wrap: anywhere; }

.guidance-row, .authority-record, .knowledge-row, .runbook-row { min-width: 0; padding: var(--co-space-3) 0; border-top: 1px solid var(--co-border-default); }
.guidance-row > header, .knowledge-row > header, .runbook-row > header { justify-content: space-between; }
.guidance-row strong, .knowledge-row strong, .runbook-row strong, .authority-record strong { min-width: 0; overflow: hidden; color: var(--co-text-primary); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.guidance-row code, .guidance-row time, .knowledge-row code, .knowledge-row time, .runbook-row code, .runbook-row small, .runbook-row time { display: block; max-width: 100%; margin-top: 3px; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; text-overflow: ellipsis; white-space: nowrap; }

.authority-levels { display: grid; gap: var(--co-space-1); margin: 0 0 var(--co-space-3); padding: 0; list-style: none; }
.authority-levels li { display: grid; grid-template-columns: 28px minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); min-height: 38px; }
.authority-levels li > span { display: grid; width: 28px; height: 28px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-subtle); }
.authority-levels li > span :deep(svg) { width: 14px; height: 14px; }
.authority-levels li > div { display: grid; min-width: 0; }
.authority-levels strong { color: var(--co-text-primary); font-size: 10px; }
.authority-levels small { color: var(--co-text-muted); font-size: 8px; }
.authority-record > header { justify-content: flex-start; }
.authority-record > header > :last-child { margin-left: auto; }
.authority-record.high-impact { padding-left: var(--co-space-3); border-left: 3px solid var(--co-status-warning-border); }
.authority-record details { margin: var(--co-space-2) 0; }
.authority-record summary { color: var(--co-action-primary); cursor: pointer; font-size: 9px; }
.authority-record pre, .authority-modal-facts pre { max-height: 180px; margin: var(--co-space-2) 0 0; padding: var(--co-space-2); overflow: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-code-text); background: var(--co-code-bg); font-size: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
.authority-record > :deep(a), .authority-record > :deep(button) { margin-top: var(--co-space-2); }
.knowledge-row :deep(button) { margin-top: var(--co-space-2); }

.agent-inspector-rail { display: grid; min-width: 0; min-height: 0; grid-template-rows: 34px 1fr; justify-items: center; padding-top: 3px; border-left: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.agent-inspector-rail > span { align-self: start; margin-top: var(--co-space-2); color: var(--co-text-muted); font-size: 9px; font-weight: 700; text-transform: uppercase; writing-mode: vertical-rl; }
.is-compact .guidance-section, .is-compact .authority-section, .is-compact .knowledge-section, .is-compact .runbook-section { display: none; }
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

@media (max-width: 767px) { .agent-inspector { border-left: 0; } }
</style>
