<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { Bot, CheckCircle2, Clock3, FileSearch, Radio, ShieldCheck } from "lucide-vue-next";
import { useRoute, useRouter, type RouteLocationRaw } from "vue-router";

import RealtimeTrustStatus from "../../components/workspace/RealtimeTrustStatus.vue";
import type { RealtimeTrustState } from "../../components/workspace/workspacePresentation";
import CommandFeedback from "../../components/incidents/CommandFeedback.vue";
import DeliveryRail from "../../components/incidents/DeliveryRail.vue";
import IncidentCommandConfirmation from "../../components/incidents/IncidentCommandConfirmation.vue";
import IncidentHeader from "../../components/incidents/IncidentHeader.vue";
import IncidentSectionShell from "../../components/incidents/IncidentSectionShell.vue";
import RemediationWorkbench from "../../components/incidents/RemediationWorkbench.vue";
import ResolutionReport from "../../components/incidents/ResolutionReport.vue";
import ResultBadge from "../../components/incidents/ResultBadge.vue";
import SeverityBadge from "../../components/incidents/SeverityBadge.vue";
import StateBlock from "../../components/incidents/StateBlock.vue";
import VerificationMatrix from "../../components/incidents/VerificationMatrix.vue";
import ZoneNav, { type IncidentZone } from "../../components/incidents/ZoneNav.vue";
import { useIncidentDetail } from "../../composables/incidents/useIncidentDetail";
import { useIncidentRealtime } from "../../composables/incidents/useIncidentRealtime";
import { canExposeResolutionReport } from "../../models/recovery";
import type { IncidentContextLinkView, IncidentRealtimeEvent, RemediationPlanView } from "../../types/incidents";
import { contextLocation } from "../../utils/contextLink";
import { formatIncidentTime } from "../../utils/incidentTime";

type Projection = IncidentRealtimeEvent["resource"] | "alerts";
type IncidentCommandKind = "investigate" | "recovery" | "close";

const route = useRoute();
const router = useRouter();
const viewRoot = ref<HTMLElement | null>(null);
const incidentID = String(route.params.incidentId ?? "");
const detail = useIncidentDetail(incidentID);
const realtime = useIncidentRealtime(incidentID, detail.refreshResource);
const commandKind = ref<IncidentCommandKind | null>(null);

const resolutionEligible = computed(() => canExposeResolutionReport(detail.verifications.data));
const incidentClosed = computed(() => detail.incident.value?.status === "closed");
const currentDecision = computed(() => detail.decision.data ?? detail.incident.value?.decision ?? null);
const latestInvestigation = computed(() => [...detail.investigations.data].sort((left, right) => {
  const time = Date.parse(right.updated_at || right.created_at) - Date.parse(left.updated_at || left.created_at);
  return time || right.id.localeCompare(left.id);
})[0] ?? null);
const latestInvestigationTerminal = computed(() => {
  const status = latestInvestigation.value?.status;
  return status === "completed" || status === "failed" || status === "cancelled";
});
const canStartInvestigation = computed(() => {
  const status = detail.incident.value?.status;
  return (status === "detected" || status === "investigating")
    && !detail.investigations.data.some((item) => item.status === "pending" || item.status === "running");
});
const canDecideRecovery = computed(() => detail.incident.value?.status === "investigating" && latestInvestigationTerminal.value);
const incidentCommandFeedback = computed(() => detail.commandFeedback.value?.resourceID === incidentID
  ? detail.commandFeedback.value
  : null);
const contextEntries = computed(() => (detail.incident.value?.context_links ?? []).flatMap((link) => {
  const target = contextLocation(link);
  return target ? [{ link, target }] : [];
}));
const trustState = computed<RealtimeTrustState>(() => {
  if (realtime.resyncState.value === "failed") return "resync-failed";
  if (realtime.resyncState.value === "resyncing") return "resyncing";
  if (realtime.state.value === "connected") return "live";
  if (realtime.state.value === "connecting") return "connecting";
  if (realtime.state.value === "reconnecting") return "reconnecting";
  return "disconnected";
});
const commandFacts = computed(() => {
  const incident = detail.incident.value;
  const kind = commandKind.value;
  const target = incident ? `${incident.operational_context.cluster}/${incident.operational_context.namespace}/${incident.operational_context.resource.kind}/${incident.operational_context.resource.name}` : incidentID;
  if (kind === "investigate") return {
    title: "发起有界 Agent 调查",
    description: "创建当前 Cycle 的持久化 Investigation 命令。",
    effect: "Agent 只能在后端允许的有界 read-only 工具与预算内调查；此确认不会授权变更。",
    target,
    authority: "authenticated Incident investigation command",
    recovery: "可停止后续调查，但已持久化的运行与 Evidence 不会被前端删除。",
    confirmLabel: "发起调查",
    severity: "primary" as const,
    reasonRequired: false,
  };
  if (kind === "recovery") return {
    title: "进入恢复验证",
    description: "提交当前 Incident 精确版本的 no-change Recovery Decision。",
    effect: "该命令不会直接解决 Incident；它只能启动确定性 Verification。",
    target,
    authority: "authenticated Incident recovery decision",
    recovery: "验证失败、超时或无结论会回到 Investigate，历史尝试保留。",
    confirmLabel: "进入 Verification",
    severity: "warning" as const,
    reasonRequired: true,
  };
  return {
    title: "关闭 Incident",
    description: "仅在完整 Recovery Verification 与 ResolutionReport 已持久化时提交关闭命令。",
    effect: "关闭会终结当前 Incident Cycle 的活动协调状态，但不会删除审计历史。",
    target,
    authority: "authenticated Incident close command",
    recovery: "前端不承诺重新打开；关闭前必须核对最终恢复证明。",
    confirmLabel: "关闭 Incident",
    severity: "error" as const,
    reasonRequired: true,
  };
});
const zones: IncidentZone[] = [
  { id: "agent-investigation", label: "Agent 调查", index: "01", aliases: ["what-happened", "investigation-zone", "investigations"] },
  { id: "evidence", label: "Evidence", index: "02" },
  { id: "approval", label: "Approval", index: "03", aliases: ["decision-zone", "decision", "remediation-plans"] },
  { id: "delivery", label: "Delivery", index: "04" },
  { id: "verification", label: "Verification", index: "05", aliases: ["verifications"] },
  { id: "timeline", label: "Timeline", index: "06" },
  { id: "resolution", label: "Resolution", index: "07", aliases: ["recovery-zone", "resolution-report"] },
];
const workspaceLabels: Record<IncidentContextLinkView["workspace"], string> = {
  monitoring: "监控",
  logs: "日志",
  traces: "链路",
  agent: "Agent",
  alerts: "Alert",
  devops: "DevOps",
};

onMounted(loadDetail);

async function loadDetail() {
  await detail.load();
  if (detail.pageState.value !== "ready") return;
  if (realtime.state.value === "disconnected") realtime.start();
  await nextTick();
  const heading = viewRoot.value?.querySelector<HTMLElement>("h1");
  if (heading) {
    heading.tabIndex = -1;
    heading.focus({ preventScroll: true });
  }
}

function retryProjection(projection: Projection) {
  const resource = projection === "alerts" ? "incident" : projection;
  void detail.refreshResource(resource).catch(() => {
    // The section retains the last durable projection and exact request identity.
  });
}

function returnToIncidents() {
  void router.push({ name: "incidents" });
}

function targetFor(link: IncidentContextLinkView): RouteLocationRaw {
  return contextLocation(link) ?? { name: "incidents" };
}

function workspaceLabel(workspace: IncidentContextLinkView["workspace"]): string {
  return workspaceLabels[workspace];
}

function relationProvenance(value: string): string {
  return ({
    owner_created: "Owner 创建",
    owner_attached: "Owner 关联",
    escalation_policy: "升级策略",
    legacy_automatic_ingress: "旧自动接入",
  } as Record<string, string>)[value] ?? value;
}

function compactID(value?: string): string {
  if (!value) return "未生成";
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function openCommand(kind: IncidentCommandKind) {
  commandKind.value = kind;
}

async function confirmCommand(reason: string) {
  const kind = commandKind.value;
  if (!kind) return;
  try {
    if (kind === "investigate") await detail.investigate(reason);
    else if (kind === "recovery") await detail.decideRecovery(reason);
    else await detail.close(reason);
    commandKind.value = null;
  } catch {
    // CommandFeedback exposes the exact transport and idempotency result.
  }
}

async function decidePlan(plan: RemediationPlanView, decision: "approved" | "rejected", reason: string) {
  try {
    await detail.decideRemediation(plan, decision, reason);
  } catch {
    // ApprovalPanel keeps the command feedback adjacent to the exact Plan.
  }
}

async function retryCommand() {
  try {
    await detail.retryLastCommand();
  } catch {
    // CommandFeedback preserves retry identity and outcome.
  }
}

async function refreshIncidentAfterConflict() {
  try {
    await detail.load({ preserve: true });
    detail.clearCommandFeedback();
  } catch {
    // The visible durable projection remains available.
  }
}
</script>

<template>
  <section
    ref="viewRoot"
    class="incident-detail-view"
  >
    <UButton
      color="neutral"
      variant="ghost"
      icon="i-lucide-arrow-left"
      label="返回 Incident 列表"
      :to="{ name: 'incidents' }"
      class="back-link"
    />

    <template v-if="detail.pageState.value !== 'ready' || !detail.incident.value">
      <h1 class="visually-hidden">
        Incident 详情
      </h1>
      <StateBlock
        :state="detail.pageState.value === 'ready' ? 'error' : detail.pageState.value"
        :heading-level="2"
        :busy="detail.refreshing.value || detail.pageState.value === 'loading'"
        :title="detail.pageState.value === 'loading' ? '正在读取 Incident…' : undefined"
        :message="detail.pageError.value?.message"
        :request-i-d="detail.pageError.value?.requestID"
        :trace-i-d="detail.pageError.value?.traceID"
        :primary-action-label="['error', 'unavailable'].includes(detail.pageState.value) ? '重试' : undefined"
        secondary-action-label="返回列表"
        @primary-action="loadDetail"
        @secondary-action="returnToIncidents"
      />
    </template>

    <template v-else>
      <IncidentHeader
        :incident="detail.incident.value"
        :realtime-state="realtime.state.value"
        :realtime-notice="realtime.notice.value"
        :refreshing="detail.refreshing.value"
        :last-updated-at="detail.lastUpdatedAt.value"
      />
      <RealtimeTrustStatus
        :state="trustState"
        :cursor="realtime.lastCursor.value"
        :last-continuous-at="realtime.lastEventAt.value"
        :detail="realtime.notice.value"
      />

      <UAlert
        v-if="detail.pageError.value"
        color="warning"
        variant="soft"
        icon="i-lucide-triangle-alert"
        title="当前投影仍可查看"
        :description="detail.pageError.value.message"
      />
      <CommandFeedback
        :feedback="incidentCommandFeedback"
        :pending="detail.commandPending.value"
        @retry="retryCommand"
        @refresh="refreshIncidentAfterConflict"
      />

      <ZoneNav :zones="zones" />

      <div class="zone-stack">
        <section
          id="agent-investigation"
          class="incident-zone"
          aria-labelledby="agent-investigation-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">01</span>
            <div>
              <h2 id="agent-investigation-title">
                Agent 调查
              </h2><p>运行范围、持久化 Signal、关联 Alert、Investigation 与 Recovery Decision。</p>
            </div>
          </header>

          <dl class="scope-grid">
            <div><dt>集群 / 环境</dt><dd>{{ detail.incident.value.operational_context.cluster }} / {{ detail.incident.value.operational_context.environment }}</dd></div>
            <div><dt>Namespace / 服务</dt><dd>{{ detail.incident.value.operational_context.namespace }} / {{ detail.incident.value.operational_context.service }}</dd></div>
            <div><dt>资源</dt><dd>{{ detail.incident.value.operational_context.resource.kind }} / {{ detail.incident.value.operational_context.resource.name }}</dd></div>
            <div><dt>观察窗口</dt><dd>{{ formatIncidentTime(detail.incident.value.operational_context.time_range.from) }} → {{ formatIncidentTime(detail.incident.value.operational_context.time_range.to) }}</dd></div>
          </dl>

          <nav
            v-if="contextEntries.length"
            class="context-links"
            aria-label="同一调查的相关资源"
          >
            <UButton
              v-for="entry in contextEntries"
              :key="entry.link.workspace"
              color="neutral"
              variant="outline"
              icon="i-lucide-link-2"
              :label="workspaceLabel(entry.link.workspace)"
              :to="entry.target"
            />
          </nav>

          <IncidentSectionShell
            id="signals"
            title="Persisted Signals"
            :state="detail.signals.state"
            :error="detail.signals.error"
            :refreshing="detail.signals.refreshing"
            :loading-more="detail.signals.loadingMore"
            empty-text="当前 Cycle 没有持久化 Signal。"
            projection-note="仅展示 API 返回的持久化 Signal；浏览器不追加 live cluster read。"
            retryable
            @retry="retryProjection('signals')"
          >
            <ol class="record-list compact-records">
              <li
                v-for="item in detail.signals.data"
                :key="item.id"
              >
                <div class="record-icon">
                  <Radio
                    :size="17"
                    aria-hidden="true"
                  />
                </div>
                <div class="record-main">
                  <strong>{{ item.summary || item.kind }}</strong><span>{{ item.kind }} · {{ compactID(item.id) }}</span>
                </div>
                <ResultBadge :result="item.status || 'unknown'" />
              </li>
            </ol>
            <UButton
              v-if="detail.signals.nextCursor"
              color="neutral"
              variant="outline"
              icon="i-lucide-chevrons-down"
              :loading="detail.signals.loadingMore"
              label="加载更多 Signal"
              @click="detail.moreSignals"
            />
          </IncidentSectionShell>

          <IncidentSectionShell
            id="related-alerts"
            title="关联 Alert"
            :state="detail.alerts.state"
            :error="detail.alerts.error"
            :refreshing="detail.alerts.refreshing"
            :loading-more="detail.alerts.loadingMore"
            empty-text="当前 Cycle 没有关联 Alert。"
            retryable
            @retry="retryProjection('alerts')"
          >
            <template #heading>
              <span class="section-count">{{ detail.alerts.data.length }} / {{ detail.incident.value.related_alert_count }}</span>
            </template>
            <ol class="record-list alert-relations">
              <li
                v-for="item in detail.alerts.data"
                :key="item.id"
              >
                <div class="record-status">
                  <SeverityBadge :severity="item.severity" /><ResultBadge :result="item.status" />
                </div>
                <div class="record-main">
                  <RouterLink :to="targetFor(item.context_link)">
                    {{ item.summary }}
                  </RouterLink><span>{{ item.namespace }}/{{ item.target_name }} · {{ item.service }}</span>
                </div>
                <dl><div><dt>关联来源</dt><dd>{{ relationProvenance(item.provenance) }}</dd></div><div><dt>最近出现</dt><dd>{{ formatIncidentTime(item.last_seen_at) }}</dd></div><div><dt>Cycle</dt><dd>{{ item.cycle }}</dd></div></dl>
              </li>
            </ol>
            <UButton
              v-if="detail.alerts.nextCursor"
              color="neutral"
              variant="outline"
              icon="i-lucide-chevrons-down"
              :loading="detail.alerts.loadingMore"
              label="加载更多 Alert"
              @click="detail.moreAlerts"
            />
          </IncidentSectionShell>

          <div class="zone-action-row">
            <div>
              <Bot
                :size="18"
                aria-hidden="true"
              /><span>{{ detail.investigations.data.length }} 次当前 Cycle 调查</span>
            </div>
            <UButton
              color="primary"
              icon="i-lucide-search"
              label="发起调查"
              :disabled="detail.commandPending.value || !canStartInvestigation"
              @click="openCommand('investigate')"
            />
          </div>
          <IncidentSectionShell
            id="investigations"
            title="Investigation 记录"
            :state="detail.investigations.state"
            :error="detail.investigations.error"
            :refreshing="detail.investigations.refreshing"
            :loading-more="detail.investigations.loadingMore"
            empty-text="当前 Cycle 尚未发起 Investigation。"
            retryable
            @retry="retryProjection('investigations')"
          >
            <ol class="record-list investigation-list">
              <li
                v-for="item in detail.investigations.data"
                :key="item.id"
              >
                <div class="record-icon">
                  <Bot
                    :size="18"
                    aria-hidden="true"
                  />
                </div>
                <div class="record-main">
                  <RouterLink :to="targetFor(item.context_link)">
                    {{ item.objective || "Incident 调查" }}
                  </RouterLink><span>{{ item.outcome || item.failure_summary || "等待持久化结果" }}</span>
                </div>
                <ResultBadge :result="item.status" />
                <dl><div><dt>步骤</dt><dd>{{ item.used_steps }} / {{ item.max_steps }}</dd></div><div><dt>模型</dt><dd>{{ item.actual_model || item.model_provider || "NOT RUN" }}</dd></div><div><dt>Prompt</dt><dd>{{ item.prompt_version }}</dd></div><div><dt>Cycle / 版本</dt><dd>{{ item.cycle }} / {{ item.version }}</dd></div></dl>
              </li>
            </ol>
            <UButton
              v-if="detail.investigations.nextCursor"
              color="neutral"
              variant="outline"
              icon="i-lucide-chevrons-down"
              :loading="detail.investigations.loadingMore"
              label="加载更多 Investigation"
              @click="detail.moreInvestigations"
            />
          </IncidentSectionShell>

          <IncidentSectionShell
            id="decision"
            title="Recovery Decision"
            :state="detail.decision.state"
            :error="detail.decision.error"
            :refreshing="detail.decision.refreshing"
            empty-text="当前 Cycle 尚未形成决策。"
            retryable
            @retry="retryProjection('incident')"
          >
            <article
              v-if="currentDecision"
              class="decision-record"
            >
              <header><div><span>{{ currentDecision.kind }}</span><h3>{{ currentDecision.summary }}</h3></div><ResultBadge :result="currentDecision.status" /></header>
              <p v-if="currentDecision.reason">
                {{ currentDecision.reason }}
              </p>
              <dl>
                <div><dt>Cycle</dt><dd>{{ currentDecision.cycle }}</dd></div><div><dt>执行者</dt><dd>{{ currentDecision.actor || "system" }}</dd></div><div><dt>Investigation</dt><dd><code translate="no">{{ compactID(currentDecision.investigation_id) }}</code></dd></div><div v-if="currentDecision.remediation_plan_id">
                  <dt>Plan</dt><dd><code translate="no">{{ compactID(currentDecision.remediation_plan_id) }}</code></dd>
                </div><div v-if="currentDecision.delivery_id">
                  <dt>Delivery</dt><dd><code translate="no">{{ compactID(currentDecision.delivery_id) }}</code></dd>
                </div>
              </dl>
            </article>
            <div class="zone-action-row">
              <div>
                <ShieldCheck
                  :size="18"
                  aria-hidden="true"
                /><span>{{ latestInvestigationTerminal ? "Investigation 已形成终态" : "等待 Investigation 终态" }}</span>
              </div>
              <UButton
                color="warning"
                icon="i-lucide-shield-check"
                label="进入恢复验证"
                :disabled="detail.commandPending.value || !canDecideRecovery"
                @click="openCommand('recovery')"
              />
            </div>
          </IncidentSectionShell>
        </section>

        <section
          id="evidence"
          class="incident-zone"
          aria-labelledby="evidence-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">02</span><div>
              <h2 id="evidence-zone-title">
                Evidence
              </h2><p>可归因、可复制、带内容 hash 的当前 Cycle 证据。</p>
            </div>
          </header>
          <IncidentSectionShell
            id="evidence-records"
            title="Evidence 证据"
            :state="detail.evidence.state"
            :error="detail.evidence.error"
            :refreshing="detail.evidence.refreshing"
            :loading-more="detail.evidence.loadingMore"
            empty-text="当前 Cycle 没有可归因 Evidence。"
            retryable
            @retry="retryProjection('evidence')"
          >
            <ol class="record-list evidence-list">
              <li
                v-for="item in detail.evidence.data"
                :key="item.id"
              >
                <div class="record-icon">
                  <FileSearch
                    :size="18"
                    aria-hidden="true"
                  />
                </div>
                <div class="record-main">
                  <RouterLink :to="targetFor(item.context_link)">
                    {{ item.summary || item.type }}
                  </RouterLink><span>{{ item.source }}<template v-if="item.tool_name"> · {{ item.tool_name }}</template> · {{ item.resource_ref || "未绑定资源" }}</span>
                </div>
                <ResultBadge :result="item.valid ? 'valid' : 'invalid'" />
                <dl>
                  <div><dt>采集时间</dt><dd>{{ formatIncidentTime(item.collected_at) }}</dd></div><div><dt>Content hash</dt><dd><code translate="no">{{ item.content_hash }}</code></dd></div><div>
                    <dt>Producer</dt><dd>
                      {{ item.producer_type || "未投影" }}<template v-if="item.producer_version">
                        / {{ item.producer_version }}
                      </template>
                    </dd>
                  </div><div>
                    <dt>Provenance</dt><dd>
                      {{ item.migrated_legacy_context ? "迁移的 legacy context" : "原生" }}<template v-if="item.truncated">
                        · 已截断
                      </template>
                    </dd>
                  </div>
                </dl>
              </li>
            </ol>
            <UButton
              v-if="detail.evidence.nextCursor"
              color="neutral"
              variant="outline"
              icon="i-lucide-chevrons-down"
              :loading="detail.evidence.loadingMore"
              label="加载更多 Evidence"
              @click="detail.moreEvidence"
            />
          </IncidentSectionShell>
        </section>

        <section
          id="approval"
          class="incident-zone"
          aria-labelledby="approval-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">03</span><div>
              <h2 id="approval-zone-title">
                Approval
              </h2><p>只对服务端生成的精确 Plan、版本、hash、Evidence 绑定与 rollback 事实作出 Owner Decision。</p>
            </div>
          </header>
          <RemediationWorkbench
            :state="detail.remediationPlans.state"
            :error="detail.remediationPlans.error"
            :plans="detail.remediationPlans.data"
            :next-cursor="detail.remediationPlans.nextCursor"
            :refreshing="detail.remediationPlans.refreshing"
            :loading-more="detail.remediationPlans.loadingMore"
            :incident-version="detail.incident.value.version"
            :incident-status="detail.incident.value.status"
            :command-pending="detail.commandPending.value"
            :command-feedback="detail.commandFeedback.value"
            @load-more="detail.moreRemediationPlans"
            @retry-resource="retryProjection('remediation_plans')"
            @decide="decidePlan"
            @retry-command="retryCommand"
            @refresh-conflict="refreshIncidentAfterConflict"
          />
        </section>

        <section
          id="delivery"
          class="incident-zone"
          aria-labelledby="delivery-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">04</span><div>
              <h2 id="delivery-zone-title">
                Delivery
              </h2><p>Git、CI、Human Merge、Argo 与 rollout 的持久化观测；健康状态不替代恢复证明。</p>
            </div>
          </header>
          <DeliveryRail
            section-i-d="delivery-projection"
            :state="detail.delivery.state"
            :error="detail.delivery.error"
            :delivery="detail.delivery.data"
            :refreshing="detail.delivery.refreshing"
            @retry="retryProjection('delivery')"
          />
        </section>

        <section
          id="verification"
          class="incident-zone"
          aria-labelledby="verification-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">05</span><div>
              <h2 id="verification-zone-title">
                Verification
              </h2><p>必要检查、样本、阈值与共同稳定窗口决定恢复是否得到证明。</p>
            </div>
          </header>
          <dl class="recovery-strip">
            <div>
              <dt>恢复状态</dt><dd>
                <ShieldCheck
                  :size="17"
                  aria-hidden="true"
                />{{ detail.incident.value.recovery.state }}
              </dd>
            </div>
            <div><dt>Verification</dt><dd>{{ detail.incident.value.recovery.verification_attempts }} 次 / {{ detail.incident.value.recovery.failed_verification_count }} 次失败</dd></div>
            <div><dt>共同窗口</dt><dd>{{ detail.incident.value.recovery.common_window_completed_at ? formatIncidentTime(detail.incident.value.recovery.common_window_completed_at) : "未完成" }}</dd></div>
            <div><dt>ResolutionReport</dt><dd><code translate="no">{{ compactID(detail.incident.value.recovery.resolution_report_id) }}</code></dd></div>
          </dl>
          <VerificationMatrix
            :state="detail.verifications.state"
            :error="detail.verifications.error"
            :runs="detail.verifications.data"
            :next-cursor="detail.verifications.nextCursor"
            :refreshing="detail.verifications.refreshing"
            :loading-more="detail.verifications.loadingMore"
            @load-more="detail.moreVerifications"
            @retry="retryProjection('verifications')"
          />
        </section>

        <section
          id="timeline"
          class="incident-zone"
          aria-labelledby="timeline-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">06</span><div>
              <h2 id="timeline-zone-title">
                Timeline
              </h2><p>服务端顺序、精确 UTC 与渐进分页保持 5k 事件场景可读。</p>
            </div>
          </header>
          <IncidentSectionShell
            id="timeline-events"
            title="活动时间线"
            :state="detail.timeline.state"
            :error="detail.timeline.error"
            :refreshing="detail.timeline.refreshing"
            :loading-more="detail.timeline.loadingMore"
            empty-text="当前 Cycle 没有 Timeline 事件。"
            projection-note="每页渐进追加且使用 content-visibility；浏览器不重排服务端顺序。"
            retryable
            @retry="retryProjection('timeline')"
          >
            <ol class="timeline-list">
              <li
                v-for="item in detail.timeline.data"
                :key="item.id"
              >
                <span class="timeline-marker"><Clock3
                  :size="15"
                  aria-hidden="true"
                /></span>
                <div><header><strong>{{ item.summary || item.type }}</strong><time :datetime="item.occurred_at">{{ formatIncidentTime(item.occurred_at) }}</time></header><p>{{ item.type }} · {{ item.actor_type }} / {{ item.actor_id || "system" }}</p><span v-if="item.source_status || item.target_status">{{ item.source_status || "—" }} → {{ item.target_status || "—" }}<template v-if="item.reason_code"> · {{ item.reason_code }}</template></span></div>
              </li>
            </ol>
            <UButton
              v-if="detail.timeline.nextCursor"
              color="neutral"
              variant="outline"
              icon="i-lucide-chevrons-down"
              :loading="detail.timeline.loadingMore"
              label="加载更多 Timeline"
              @click="detail.moreTimeline"
            />
          </IncidentSectionShell>
        </section>

        <section
          id="resolution"
          class="incident-zone"
          aria-labelledby="resolution-zone-title"
        >
          <header class="zone-heading">
            <span aria-hidden="true">07</span><div>
              <h2 id="resolution-zone-title">
                Resolution
              </h2><p>ResolutionReport 与最终关闭保持独立；Delivery 完成不能直接解决 Incident。</p>
            </div>
          </header>
          <ResolutionReport
            :state="detail.resolutionReport.state"
            :error="detail.resolutionReport.error"
            :report="detail.resolutionReport.data"
            :eligible="resolutionEligible"
            :refreshing="detail.resolutionReport.refreshing"
            @retry="retryProjection('resolution_report')"
          />
          <div class="close-row">
            <div>
              <CheckCircle2
                :size="19"
                aria-hidden="true"
              /><span v-if="incidentClosed">Incident 已关闭，最终恢复证明与关闭历史保持可审计。</span><span v-else>{{ detail.incident.value.recovery.can_close ? "恢复证明完整，可以关闭。" : "等待通过的 Verification、共同窗口与 ResolutionReport。" }}</span>
            </div>
            <UButton
              color="error"
              variant="soft"
              icon="i-lucide-circle-check-big"
              :label="incidentClosed ? 'Incident 已关闭' : '关闭 Incident'"
              :disabled="incidentClosed || !detail.incident.value.recovery.can_close || detail.commandPending.value"
              @click="openCommand('close')"
            />
          </div>
        </section>
      </div>

      <IncidentCommandConfirmation
        :open="Boolean(commandKind)"
        :title="commandFacts.title"
        :description="commandFacts.description"
        :target="commandFacts.target"
        :effect="commandFacts.effect"
        :authority="commandFacts.authority"
        :version="`Incident v${detail.incident.value.version} · Cycle ${detail.incident.value.cycle}`"
        :recovery="commandFacts.recovery"
        :confirm-label="commandFacts.confirmLabel"
        :severity="commandFacts.severity"
        :reason-required="commandFacts.reasonRequired"
        :pending="detail.commandPending.value"
        @update:open="(open) => { if (!open) commandKind = null; }"
        @confirm="confirmCommand"
      />
    </template>
  </section>
</template>

<style scoped>
.incident-detail-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-4); }
.back-link { width: fit-content; }
.zone-stack { display: grid; min-width: 0; gap: var(--co-space-8); }
.incident-zone { display: grid; min-width: 0; scroll-margin-top: 72px; }
.zone-heading { display: grid; grid-template-columns: 34px minmax(0, 1fr); min-width: 0; gap: var(--co-space-3); padding: var(--co-space-6) 0 var(--co-space-3); }
.zone-heading > span { display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: var(--co-radius-pill); color: var(--co-text-muted); background: var(--co-bg-surface); font-family: var(--co-font-mono); font-size: 11px; }
.zone-heading h2, .zone-heading p { margin: 0; }
.zone-heading h2 { color: var(--co-text-primary); font-size: 20px; }
.zone-heading p { margin-top: 3px; color: var(--co-text-secondary); font-size: 13px; }
.scope-grid, .recovery-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); min-width: 0; margin: var(--co-space-3) 0 var(--co-space-4); overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.scope-grid div, .recovery-strip div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); }
.scope-grid div:last-child, .recovery-strip div:last-child { border-right: 0; }
.scope-grid dt, .recovery-strip dt, .record-list dt, .decision-record dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scope-grid dd, .recovery-strip dd, .record-list dd, .decision-record dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.context-links { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); padding-bottom: var(--co-space-3); }
.section-count { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 12px; }
.record-list, .timeline-list { display: grid; min-width: 0; margin: 0; padding: 0; list-style: none; }
.record-list > li { display: grid; grid-template-columns: auto minmax(220px, 1.2fr) auto minmax(330px, 1fr); min-width: 0; align-items: center; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-bottom: 1px solid var(--co-border-default); content-visibility: auto; contain-intrinsic-size: auto 92px; }
.compact-records > li { grid-template-columns: auto minmax(0, 1fr) auto; }
.alert-relations > li { grid-template-columns: auto minmax(240px, 1fr) minmax(350px, 1fr); }
.record-status { display: grid; gap: 4px; justify-items: start; }
.record-icon { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-subtle); }
.record-main { display: grid; min-width: 0; gap: 3px; }
.record-main :is(a, strong) { color: var(--co-text-primary); font-size: 12px; font-weight: 750; overflow-wrap: anywhere; }
.record-main span { color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.record-list dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); min-width: 0; gap: var(--co-space-2) var(--co-space-4); margin: 0; }
.alert-relations dl { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.zone-action-row, .close-row { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-block: 1px solid var(--co-border-default); }
.zone-action-row > div, .close-row > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); color: var(--co-text-secondary); font-size: 12px; }
.decision-record { display: grid; min-width: 0; gap: var(--co-space-3); padding-left: var(--co-space-4); border-left: var(--co-severity-marker-width) solid var(--co-action-primary); }
.decision-record header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.decision-record header span { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
.decision-record h3, .decision-record p { margin: 0; }
.decision-record h3 { margin-top: 3px; font-size: 15px; }
.decision-record dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-3); margin: 0; }
.timeline-list > li { position: relative; display: grid; grid-template-columns: 32px minmax(0, 1fr); gap: var(--co-space-3); padding-bottom: var(--co-space-4); content-visibility: auto; contain-intrinsic-size: auto 82px; }
.timeline-list > li:not(:last-child)::before { position: absolute; top: 31px; bottom: 0; left: 15px; width: 1px; background: var(--co-border-default); content: ""; }
.timeline-marker { z-index: 1; display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-pill); background: var(--co-bg-canvas); }
.timeline-list > li > div { min-width: 0; padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.timeline-list header { display: flex; justify-content: space-between; gap: var(--co-space-3); }
.timeline-list strong { font-size: 12px; overflow-wrap: anywhere; }
.timeline-list time, .timeline-list p, .timeline-list div > span { color: var(--co-text-muted); font-size: 11px; }
.timeline-list p { margin: 4px 0 0; }
.close-row { margin-top: var(--co-space-4); border-color: var(--co-status-critical-border); }
@media (max-width: 1050px) {
  .scope-grid, .recovery-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .record-list > li { grid-template-columns: auto minmax(0, 1fr) auto; }
  .record-list dl { grid-column: 2 / -1; }
  .alert-relations > li { grid-template-columns: auto minmax(0, 1fr); }
  .alert-relations dl { grid-column: 2; }
}
@media (max-width: 767px) {
  .scope-grid, .recovery-strip { grid-template-columns: minmax(0, 1fr); }
  .scope-grid div, .recovery-strip div { border-right: 0; border-top: 1px solid var(--co-border-default); }
  .record-list > li, .alert-relations > li { grid-template-columns: auto minmax(0, 1fr); align-items: flex-start; }
  .record-list dl, .alert-relations dl { grid-column: 1 / -1; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .zone-action-row, .close-row { align-items: stretch; flex-direction: column; }
  .zone-action-row :deep(button), .close-row :deep(button) { width: 100%; justify-content: center; }
  .decision-record dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
