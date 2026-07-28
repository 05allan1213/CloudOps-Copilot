<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from "vue";
import { ArrowLeft, Bot, CheckCircle2, Clock3, FileSearch, GitPullRequest, Link2, Search, ShieldCheck } from "lucide-vue-next";
import { ElMessage, ElMessageBox } from "element-plus";
import { useRoute, useRouter, type RouteLocationRaw } from "vue-router";

import CommandFeedback from "../../components/incidents/CommandFeedback.vue";
import IncidentHeader from "../../components/incidents/IncidentHeader.vue";
import IncidentSectionShell from "../../components/incidents/IncidentSectionShell.vue";
import ResolutionReport from "../../components/incidents/ResolutionReport.vue";
import ResultBadge from "../../components/incidents/ResultBadge.vue";
import SeverityBadge from "../../components/incidents/SeverityBadge.vue";
import StateBlock from "../../components/incidents/StateBlock.vue";
import VerificationMatrix from "../../components/incidents/VerificationMatrix.vue";
import ZoneNav, { type IncidentZone } from "../../components/incidents/ZoneNav.vue";
import { useIncidentDetail } from "../../composables/incidents/useIncidentDetail";
import { useIncidentRealtime } from "../../composables/incidents/useIncidentRealtime";
import { canExposeResolutionReport } from "../../models/recovery";
import type { IncidentContextLinkView, IncidentRealtimeEvent } from "../../types/incidents";
import { contextLocation } from "../../utils/contextLink";
import { formatIncidentTime } from "../../utils/incidentTime";

type Projection = "alerts" | "timeline" | "evidence" | "investigations" | "decision" | "verifications" | "resolution_report";

const route = useRoute();
const router = useRouter();
const viewRoot = ref<HTMLElement | null>(null);
const incidentID = String(route.params.incidentId ?? "");
const detail = useIncidentDetail(incidentID);
const realtime = useIncidentRealtime(incidentID, detail.refreshResource);
const resolutionEligible = computed(() => canExposeResolutionReport(detail.verifications.data));
const incidentClosed = computed(() => detail.incident.value?.status === "closed");
const currentDecision = computed(() => detail.decision.data ?? detail.incident.value?.decision ?? null);
const latestInvestigation = computed(() => [...detail.investigations.data].sort((left, right) => {
  const time = Date.parse(right.created_at) - Date.parse(left.created_at);
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
const zones: IncidentZone[] = [
  { id: "what-happened", label: "发生了什么", index: "01" },
  { id: "investigation-zone", label: "调查", index: "02" },
  { id: "decision-zone", label: "决策", index: "03" },
  { id: "recovery-zone", label: "恢复", index: "04" },
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
  const resources: Record<Projection, IncidentRealtimeEvent["resource"]> = {
    alerts: "incident",
    timeline: "timeline",
    evidence: "evidence",
    investigations: "investigations",
    decision: "remediation_plans",
    verifications: "verifications",
    resolution_report: "resolution_report",
  };
  void detail.refreshResource(resources[projection]).catch(() => {
    // The section keeps the last durable projection and exposes its request identity.
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
  const labels: Record<string, string> = {
    owner_created: "Owner 创建",
    owner_attached: "Owner 关联",
    escalation_policy: "升级策略",
    legacy_automatic_ingress: "旧自动接入",
  };
  return labels[value] ?? value;
}

function compactID(value?: string): string {
  if (!value) return "未生成";
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

async function promptReason(title: string, required: boolean, maxLength = 2048): Promise<string | null> {
  try {
    const result = await ElMessageBox.prompt("该原因将与命令一起持久化。", title, {
      confirmButtonText: "提交命令",
      cancelButtonText: "取消",
      inputType: "textarea",
      inputPlaceholder: required ? "例如：已核对最终恢复证明…" : "可选：本次调查目标…",
      inputValidator: (value) => {
        const text = value.trim();
        if (required && !text) return "提交前请填写原因。";
        if (text.length > maxLength) return `原因不能超过 ${maxLength} 个字符。`;
        return true;
      },
    });
    return result.value.trim();
  } catch {
    return null;
  }
}

async function runInvestigation() {
  const reason = await promptReason("发起有界调查", false, 1024);
  if (reason === null) return;
  await runIncidentCommand(() => detail.investigate(reason));
}

async function runClose() {
  if (!detail.incident.value?.recovery.can_close) return;
  const reason = await promptReason("关闭 Incident", true);
  if (reason === null) return;
  await runIncidentCommand(() => detail.close(reason));
}

async function runRecoveryDecision() {
  if (!canDecideRecovery.value) return;
  const reason = await promptReason("进入恢复验证", true, 1024);
  if (reason === null) return;
  await runIncidentCommand(() => detail.decideRecovery(reason));
}

async function runIncidentCommand(request: () => Promise<unknown>) {
  try {
    await request();
  } catch (cause) {
    if (!incidentCommandFeedback.value) ElMessage.error(cause instanceof Error ? cause.message : "命令提交失败");
  }
}

async function retryCommand() {
  try {
    await detail.retryLastCommand();
  } catch {
    // CommandFeedback preserves the retry result and exact request identity.
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
  <section ref="viewRoot" class="incident-detail-view">
    <RouterLink :to="{ name: 'incidents' }" class="back-link">
      <ArrowLeft :size="17" aria-hidden="true" />
      返回 Incident 列表
    </RouterLink>

    <template v-if="detail.pageState.value !== 'ready' || !detail.incident.value">
      <h1 class="visually-hidden">Incident 详情</h1>
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

      <div v-if="detail.pageError.value" class="page-refresh-warning" role="status" aria-live="polite">
        <strong>当前投影仍可查看。</strong>
        <span>{{ detail.pageError.value.message }}</span>
        <code v-if="detail.pageError.value.requestID" translate="no">Request {{ detail.pageError.value.requestID }}</code>
      </div>

      <CommandFeedback
        :feedback="incidentCommandFeedback"
        :pending="detail.commandPending.value"
        @retry="retryCommand"
        @refresh="refreshIncidentAfterConflict"
      />

      <ZoneNav :zones="zones" />

      <div class="zone-stack">
        <section id="what-happened" class="incident-zone" aria-labelledby="what-happened-title">
          <header class="zone-heading">
            <span aria-hidden="true">01</span>
            <div><h2 id="what-happened-title">发生了什么</h2><p>当前 Cycle 的运行范围、关联 Alert 与按服务端顺序记录的事实。</p></div>
          </header>

          <dl class="scope-grid">
            <div><dt>集群 / 环境</dt><dd>{{ detail.incident.value.operational_context.cluster }} / {{ detail.incident.value.operational_context.environment }}</dd></div>
            <div><dt>Namespace / 服务</dt><dd>{{ detail.incident.value.operational_context.namespace }} / {{ detail.incident.value.operational_context.service }}</dd></div>
            <div><dt>资源</dt><dd>{{ detail.incident.value.operational_context.resource.kind }} / {{ detail.incident.value.operational_context.resource.name }}</dd></div>
            <div><dt>观察窗口</dt><dd>{{ formatIncidentTime(detail.incident.value.operational_context.time_range.from) }} → {{ formatIncidentTime(detail.incident.value.operational_context.time_range.to) }}</dd></div>
          </dl>

          <nav v-if="contextEntries.length" class="context-links" aria-label="同一调查的 Context Links">
            <RouterLink v-for="entry in contextEntries" :key="entry.link.workspace" :to="entry.target">
              <Link2 :size="16" aria-hidden="true" />
              {{ workspaceLabel(entry.link.workspace) }}
            </RouterLink>
          </nav>

          <IncidentSectionShell
            id="related-alerts"
            title="关联 Alert"
            :state="detail.alerts.state"
            :error="detail.alerts.error"
            :refreshing="detail.alerts.refreshing"
            :loading-more="detail.alerts.loadingMore"
            empty-text="当前 Cycle 没有关联 Alert。"
            retry-label="重试"
            retryable
            @retry="retryProjection('alerts')"
          >
            <template #heading><span class="section-count">{{ detail.alerts.data.length }} / {{ detail.incident.value.related_alert_count }}</span></template>
            <ol class="record-list alert-relations">
              <li v-for="item in detail.alerts.data" :key="item.id">
                <div class="record-status"><SeverityBadge :severity="item.severity" /><ResultBadge :result="item.status" /></div>
                <div class="record-main"><RouterLink :to="targetFor(item.context_link)">{{ item.summary }}</RouterLink><span>{{ item.namespace }}/{{ item.target_name }} · {{ item.service }}</span></div>
                <dl><div><dt>关联来源</dt><dd>{{ relationProvenance(item.provenance) }}</dd></div><div><dt>最近出现</dt><dd>{{ formatIncidentTime(item.last_seen_at) }}</dd></div><div><dt>Cycle</dt><dd>{{ item.cycle }}</dd></div></dl>
              </li>
            </ol>
            <button v-if="detail.alerts.nextCursor" type="button" class="load-more" :disabled="detail.alerts.loadingMore" @click="detail.moreAlerts">
              {{ detail.alerts.loadingMore ? "正在加载…" : "加载更多 Alert" }}
            </button>
          </IncidentSectionShell>

          <IncidentSectionShell
            id="timeline"
            title="时间线"
            :state="detail.timeline.state"
            :error="detail.timeline.error"
            :refreshing="detail.timeline.refreshing"
            :loading-more="detail.timeline.loadingMore"
            empty-text="当前 Cycle 没有 Timeline 事件。"
            retry-label="重试"
            retryable
            @retry="retryProjection('timeline')"
          >
            <ol class="timeline-list">
              <li v-for="item in detail.timeline.data" :key="item.id">
                <span class="timeline-marker"><Clock3 :size="15" aria-hidden="true" /></span>
                <div><header><strong>{{ item.summary || item.type }}</strong><time :datetime="item.occurred_at">{{ formatIncidentTime(item.occurred_at) }}</time></header><p>{{ item.type }} · {{ item.actor_type }} / {{ item.actor_id || "system" }}</p><span v-if="item.source_status || item.target_status">{{ item.source_status || "—" }} → {{ item.target_status || "—" }}<template v-if="item.reason_code"> · {{ item.reason_code }}</template></span></div>
              </li>
            </ol>
            <button v-if="detail.timeline.nextCursor" type="button" class="load-more" :disabled="detail.timeline.loadingMore" @click="detail.moreTimeline">
              {{ detail.timeline.loadingMore ? "正在加载…" : "加载更多 Timeline" }}
            </button>
          </IncidentSectionShell>
        </section>

        <section id="investigation-zone" class="incident-zone" aria-labelledby="investigation-zone-title">
          <header class="zone-heading">
            <span aria-hidden="true">02</span>
            <div><h2 id="investigation-zone-title">调查</h2><p>有界 Agent Investigation 与可归因 Evidence。</p></div>
          </header>

          <div class="zone-action-row">
            <div><Bot :size="18" aria-hidden="true" /><span>{{ detail.investigations.data.length }} 次当前 Cycle 调查</span></div>
            <button type="button" class="primary-action" :disabled="detail.commandPending.value || !canStartInvestigation" @click="runInvestigation">
              <Search :size="16" aria-hidden="true" />发起调查
            </button>
          </div>

          <IncidentSectionShell
            id="investigations"
            title="Investigation 记录"
            :state="detail.investigations.state"
            :error="detail.investigations.error"
            :refreshing="detail.investigations.refreshing"
            :loading-more="detail.investigations.loadingMore"
            empty-text="当前 Cycle 尚未发起 Investigation。"
            retry-label="重试"
            retryable
            @retry="retryProjection('investigations')"
          >
            <ol class="record-list investigation-list">
              <li v-for="item in detail.investigations.data" :key="item.id">
                <div class="record-icon"><Bot :size="18" aria-hidden="true" /></div>
                <div class="record-main"><RouterLink :to="targetFor(item.context_link)">{{ item.objective || "Incident 调查" }}</RouterLink><span>{{ item.outcome || item.failure_summary || "等待持久化结果" }}</span></div>
                <ResultBadge :result="item.status" />
                <dl><div><dt>步骤</dt><dd>{{ item.used_steps }} / {{ item.max_steps }}</dd></div><div><dt>模型</dt><dd>{{ item.actual_model || item.model_provider || "NOT RUN" }}</dd></div><div><dt>Prompt</dt><dd>{{ item.prompt_version }}</dd></div><div><dt>Cycle / 版本</dt><dd>{{ item.cycle }} / {{ item.version }}</dd></div></dl>
              </li>
            </ol>
            <button v-if="detail.investigations.nextCursor" type="button" class="load-more" :disabled="detail.investigations.loadingMore" @click="detail.moreInvestigations">
              {{ detail.investigations.loadingMore ? "正在加载…" : "加载更多 Investigation" }}
            </button>
          </IncidentSectionShell>

          <IncidentSectionShell
            id="evidence"
            title="Evidence 证据"
            :state="detail.evidence.state"
            :error="detail.evidence.error"
            :refreshing="detail.evidence.refreshing"
            :loading-more="detail.evidence.loadingMore"
            empty-text="当前 Cycle 没有可归因 Evidence。"
            retry-label="重试"
            retryable
            @retry="retryProjection('evidence')"
          >
            <ol class="record-list evidence-list">
              <li v-for="item in detail.evidence.data" :key="item.id">
                <div class="record-icon"><FileSearch :size="18" aria-hidden="true" /></div>
                <div class="record-main"><RouterLink :to="targetFor(item.context_link)">{{ item.summary || item.type }}</RouterLink><span>{{ item.source }}<template v-if="item.tool_name"> · {{ item.tool_name }}</template> · {{ item.resource_ref || "未绑定资源" }}</span></div>
                <ResultBadge :result="item.valid ? 'valid' : 'invalid'" />
                <dl><div><dt>采集时间</dt><dd>{{ formatIncidentTime(item.collected_at) }}</dd></div><div><dt>Hash</dt><dd><code translate="no">{{ compactID(item.content_hash) }}</code></dd></div><div><dt>Producer</dt><dd>{{ item.producer_type || "未投影" }}<template v-if="item.producer_version"> / {{ item.producer_version }}</template></dd></div><div><dt>Provenance</dt><dd>{{ item.migrated_legacy_context ? "迁移的 legacy context" : "原生" }}<template v-if="item.truncated"> · 已截断</template></dd></div></dl>
              </li>
            </ol>
            <button v-if="detail.evidence.nextCursor" type="button" class="load-more" :disabled="detail.evidence.loadingMore" @click="detail.moreEvidence">
              {{ detail.evidence.loadingMore ? "正在加载…" : "加载更多 Evidence" }}
            </button>
          </IncidentSectionShell>
        </section>

        <section id="decision-zone" class="incident-zone" aria-labelledby="decision-zone-title">
          <header class="zone-heading">
            <span aria-hidden="true">03</span>
            <div><h2 id="decision-zone-title">决策</h2><p>当前 Cycle 的 no-change 或 action 决策投影。</p></div>
          </header>

          <div class="zone-action-row decision-action-row">
            <div>
              <ShieldCheck :size="18" aria-hidden="true" />
              <span>{{ latestInvestigationTerminal ? "当前 Investigation 已形成终态记录" : "等待当前 Cycle 的 Investigation 终态" }}</span>
            </div>
            <button
              type="button"
              class="primary-action"
              :disabled="detail.commandPending.value || !canDecideRecovery"
              :title="canDecideRecovery ? '进入恢复验证' : '需要 Investigate 状态与终态 Investigation'"
              @click="runRecoveryDecision"
            >
              <ShieldCheck :size="16" aria-hidden="true" />进入恢复验证
            </button>
          </div>

          <IncidentSectionShell
            id="decision"
            title="决策记录"
            :state="detail.decision.state"
            :error="detail.decision.error"
            :refreshing="detail.decision.refreshing"
            empty-text="当前 Cycle 尚未形成决策。"
            retry-label="重试"
            retryable
            @retry="retryProjection('decision')"
          >
            <article v-if="currentDecision" class="decision-record">
              <header><div><span>{{ currentDecision.kind === 'no_change' ? '无变更' : currentDecision.kind === 'action' ? 'Action' : currentDecision.kind === 'recovery' ? '恢复验证' : '等待决策' }}</span><h3>{{ currentDecision.summary }}</h3></div><ResultBadge :result="currentDecision.status" /></header>
              <p v-if="currentDecision.reason">{{ currentDecision.reason }}</p>
              <dl>
                <div><dt>Cycle</dt><dd>{{ currentDecision.cycle }}</dd></div>
                <div><dt>执行者</dt><dd>{{ currentDecision.actor || "system" }}</dd></div>
                <div><dt>Investigation</dt><dd><code translate="no">{{ compactID(currentDecision.investigation_id) }}</code></dd></div>
                <div><dt>Verification</dt><dd><code translate="no">{{ compactID(currentDecision.verification_id) }}</code></dd></div>
                <div v-if="currentDecision.remediation_plan_id"><dt>Plan</dt><dd><code translate="no">{{ compactID(currentDecision.remediation_plan_id) }}</code></dd></div>
                <div v-if="currentDecision.delivery_id"><dt>Delivery</dt><dd><code translate="no">{{ compactID(currentDecision.delivery_id) }}</code></dd></div>
              </dl>
              <RouterLink v-if="currentDecision.context_link" class="decision-link" :to="targetFor(currentDecision.context_link)">
                <GitPullRequest :size="16" aria-hidden="true" />在 {{ workspaceLabel(currentDecision.context_link.workspace) }} 继续
              </RouterLink>
            </article>
          </IncidentSectionShell>
        </section>

        <section id="recovery-zone" class="incident-zone" aria-labelledby="recovery-zone-title">
          <header class="zone-heading">
            <span aria-hidden="true">04</span>
            <div><h2 id="recovery-zone-title">恢复</h2><p>Recovery Verification、共同稳定窗口、ResolutionReport 与最终关闭。</p></div>
          </header>

          <dl class="recovery-strip">
            <div><dt>恢复状态</dt><dd><ShieldCheck :size="17" aria-hidden="true" />{{ detail.incident.value.recovery.state }}</dd></div>
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
              <CheckCircle2 :size="19" aria-hidden="true" />
              <span v-if="incidentClosed">Incident 已关闭，最终恢复证明与关闭历史保持可审计。</span>
              <span v-else>{{ detail.incident.value.recovery.can_close ? "恢复证明完整，可以关闭。" : "等待通过的 Verification、共同窗口与 ResolutionReport。" }}</span>
            </div>
            <button
              type="button"
              class="close-action"
              :disabled="incidentClosed || !detail.incident.value.recovery.can_close || detail.commandPending.value"
              :title="incidentClosed ? 'Incident 已关闭' : detail.incident.value.recovery.can_close ? '关闭 Incident' : '恢复证明尚不完整'"
              @click="runClose"
            >
              <CheckCircle2 :size="17" aria-hidden="true" />{{ incidentClosed ? "Incident 已关闭" : "关闭 Incident" }}
            </button>
          </div>
        </section>
      </div>
    </template>
  </section>
</template>

<style scoped>
.incident-detail-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-4); }
.back-link { display: inline-flex; width: fit-content; min-height: 44px; align-items: center; gap: var(--co-space-2); color: var(--co-action-primary); font-size: 13px; font-weight: 700; }
.back-link:hover { color: var(--co-action-hover); text-decoration: underline; text-underline-offset: 3px; }
.back-link:focus-visible, button:focus-visible, .context-links a:focus-visible, .decision-link:focus-visible { outline: 2px solid var(--co-action-primary); outline-offset: 2px; }
.page-refresh-warning { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); padding: var(--co-space-3) var(--co-space-4); border-left: 3px solid var(--co-status-warning-fg); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); font-size: 12px; }
.page-refresh-warning code { overflow-wrap: anywhere; }
.zone-stack { display: grid; min-width: 0; gap: var(--co-space-10); }
.incident-zone { display: grid; min-width: 0; gap: 0; scroll-margin-top: 64px; }
.zone-heading { display: grid; grid-template-columns: 38px minmax(0, 1fr); min-width: 0; gap: var(--co-space-3); padding: var(--co-space-6) 0 var(--co-space-3); }
.zone-heading > span { display: grid; width: 34px; height: 34px; place-items: center; border: 1px solid var(--co-border-strong); border-radius: 50%; color: var(--co-text-muted); background: var(--co-bg-surface); font-family: var(--co-font-mono); font-size: 11px; font-weight: 700; }
.zone-heading h2, .zone-heading p { margin: 0; }
.zone-heading h2 { color: var(--co-text-primary); font-size: 21px; text-wrap: balance; }
.zone-heading p { max-width: 82ch; margin-top: 3px; color: var(--co-text-secondary); font-size: 13px; text-wrap: pretty; }
.scope-grid, .recovery-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); min-width: 0; margin: var(--co-space-3) 0 var(--co-space-4); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.scope-grid div, .recovery-strip div { min-width: 0; padding: var(--co-space-3) var(--co-space-4); border-right: 1px solid var(--co-border-default); }
.scope-grid div:last-child, .recovery-strip div:last-child { border-right: 0; }
.scope-grid dt, .recovery-strip dt, .record-list dt, .decision-record dt { color: var(--co-text-muted); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.scope-grid dd, .recovery-strip dd, .record-list dd, .decision-record dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-primary); font-size: 12px; overflow-wrap: anywhere; }
.recovery-strip dd { display: flex; align-items: center; gap: 6px; font-variant-numeric: tabular-nums; }
.context-links { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); padding-bottom: var(--co-space-3); }
.context-links a, .decision-link { display: inline-flex; min-height: 40px; align-items: center; gap: 7px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-surface); font-size: 12px; font-weight: 700; }
.context-links a:hover, .decision-link:hover { border-color: var(--co-border-strong); color: var(--co-action-primary); background: var(--co-bg-hover); }
.section-count { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 12px; font-variant-numeric: tabular-nums; }
.record-list, .timeline-list { display: grid; min-width: 0; margin: 0; padding: 0; list-style: none; }
.record-list > li { display: grid; grid-template-columns: auto minmax(220px, 1.25fr) auto minmax(340px, 1fr); min-width: 0; align-items: center; gap: var(--co-space-4); padding: var(--co-space-4) 0; border-bottom: 1px solid var(--co-border-default); content-visibility: auto; contain-intrinsic-size: auto 96px; }
.record-status { display: grid; gap: 5px; justify-items: start; }
.record-icon { display: grid; width: 36px; height: 36px; place-items: center; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-secondary); background: var(--co-bg-subtle); }
.record-main { display: grid; min-width: 0; gap: 4px; }
.record-main a { color: var(--co-text-primary); font-size: 13px; font-weight: 750; overflow-wrap: anywhere; }
.record-main a:hover { color: var(--co-action-primary); text-decoration: underline; text-underline-offset: 3px; }
.record-main span { color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.record-list dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); min-width: 0; gap: var(--co-space-2) var(--co-space-4); margin: 0; }
.alert-relations > li { grid-template-columns: auto minmax(240px, 1fr) minmax(360px, 1fr); }
.alert-relations dl { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.timeline-list > li { position: relative; display: grid; grid-template-columns: 32px minmax(0, 1fr); min-width: 0; gap: var(--co-space-3); padding-bottom: var(--co-space-4); content-visibility: auto; contain-intrinsic-size: auto 84px; }
.timeline-list > li:not(:last-child)::before { position: absolute; top: 31px; bottom: 0; left: 15px; width: 1px; background: var(--co-border-default); content: ""; }
.timeline-marker { z-index: 1; display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 50%; color: var(--co-text-secondary); background: var(--co-bg-canvas); }
.timeline-list > li > div { min-width: 0; padding-bottom: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.timeline-list header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); }
.timeline-list strong { color: var(--co-text-primary); font-size: 13px; overflow-wrap: anywhere; }
.timeline-list time { flex: 0 0 auto; color: var(--co-text-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
.timeline-list p, .timeline-list div > span { margin: 4px 0 0; color: var(--co-text-muted); font-size: 11px; overflow-wrap: anywhere; }
.load-more, .primary-action, .close-action { display: inline-flex; width: fit-content; min-height: 42px; align-items: center; justify-content: center; gap: 7px; padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); cursor: pointer; font-weight: 750; }
.load-more { justify-self: center; color: var(--co-text-primary); background: var(--co-bg-surface); }
.primary-action { border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.close-action { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.load-more:hover, .primary-action:hover, .close-action:hover { border-color: var(--co-border-strong); filter: brightness(.98); }
button:disabled { cursor: not-allowed; opacity: .55; }
.zone-action-row, .close-row { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding: var(--co-space-3) 0; border-block: 1px solid var(--co-border-default); }
.zone-action-row > div, .close-row > div { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); color: var(--co-text-secondary); font-size: 12px; }
.decision-record { display: grid; min-width: 0; gap: var(--co-space-4); padding-left: var(--co-space-4); border-left: 3px solid var(--co-action-primary); }
.decision-record header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); }
.decision-record header > div { min-width: 0; }
.decision-record header span { color: var(--co-text-muted); font-size: 11px; text-transform: uppercase; }
.decision-record h3, .decision-record p { margin: 0; }
.decision-record h3 { margin-top: 3px; color: var(--co-text-primary); font-size: 16px; overflow-wrap: anywhere; }
.decision-record p { color: var(--co-text-secondary); font-size: 13px; overflow-wrap: anywhere; }
.decision-record dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); min-width: 0; gap: var(--co-space-3) var(--co-space-5); margin: 0; }
.decision-link { width: fit-content; }
.close-row { margin-top: var(--co-space-5); border-color: var(--co-status-success-border); }

@media (max-width: 1050px) {
  .scope-grid, .recovery-strip { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .scope-grid div:nth-child(2), .recovery-strip div:nth-child(2) { border-right: 0; }
  .scope-grid div:nth-child(n + 3), .recovery-strip div:nth-child(n + 3) { border-top: 1px solid var(--co-border-default); }
  .record-list > li { grid-template-columns: auto minmax(0, 1fr) auto; }
  .record-list dl { grid-column: 2 / -1; }
  .alert-relations > li { grid-template-columns: auto minmax(0, 1fr); }
  .alert-relations dl { grid-column: 2; }
}

@media (max-width: 767px) {
  .zone-stack { gap: var(--co-space-8); }
  .zone-heading { padding-top: var(--co-space-5); }
  .scope-grid, .recovery-strip { grid-template-columns: minmax(0, 1fr); }
  .scope-grid div, .recovery-strip div, .scope-grid div:nth-child(2), .recovery-strip div:nth-child(2) { border-right: 0; }
  .scope-grid div + div, .recovery-strip div + div { border-top: 1px solid var(--co-border-default); }
  .context-links { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .context-links a { min-width: 0; justify-content: center; }
  .record-list > li, .alert-relations > li { grid-template-columns: auto minmax(0, 1fr); align-items: flex-start; gap: var(--co-space-3); }
  .record-list > li > :deep(.result-badge) { justify-self: start; }
  .record-list dl, .alert-relations dl { grid-column: 1 / -1; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .timeline-list header { display: grid; gap: 3px; }
  .zone-action-row, .close-row { align-items: stretch; flex-direction: column; }
  .zone-action-row .primary-action, .close-row .close-action { width: 100%; }
  .decision-record dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 420px) {
  .context-links { grid-template-columns: minmax(0, 1fr); }
  .record-list dl, .alert-relations dl, .decision-record dl { grid-template-columns: minmax(0, 1fr); }
}
</style>
