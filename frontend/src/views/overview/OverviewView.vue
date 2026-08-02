<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";

import { getAgentInvestigations, type AgentRun } from "../../api/agent";
import { listAlerts, type AlertView } from "../../api/alerts";
import { apiErrorDetails } from "../../api/client";
import { getDevOpsWorkspace, type DevOpsWorkspace } from "../../api/devops";
import type { KubernetesResource } from "../../api/infrastructure";
import { listIncidents } from "../../api/incidents";
import { getOverview, type OverviewSnapshot } from "../../api/platform";
import OperationsAtlas from "../../components/infrastructure/OperationsAtlas.vue";
import {
  activeIncidents,
  deliveryHasPassedVerification,
  latestAgentRun,
  latestVerificationStatusForDelivery,
  pendingAgentItems,
  recentDeliveries,
  recentlyResolvedIncidents,
  unlinkedFiringAlerts,
} from "../../components/overview/overviewModel";
import WorkspacePageFrame from "../../components/workspace/WorkspacePageFrame.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";
import { useLatestAsync } from "../../composables/useLatestAsync";
import { incidentStatusLabel, severityLabel } from "../../models/incidents";
import { verificationStateLabel } from "../../models/recovery";
import type { IncidentView } from "../../types/incidents";
import { openAgentPanel } from "../../utils/agentContext";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

interface OverviewSectionFailure {
  section: string;
  error: unknown;
}

interface CommandCenterSnapshot {
  overview: OverviewSnapshot | null;
  incidents: IncidentView[];
  alerts: AlertView[];
  investigations: AgentRun[];
  devops: DevOpsWorkspace | null;
  failures: OverviewSectionFailure[];
}

type HeroTone = "critical" | "warning" | "working" | "healthy" | "unknown";

const route = useRoute();
const router = useRouter();
const request = useLatestAsync<CommandCenterSnapshot>();
const previewWebGLFailure = ref("");
const utcFormatter = new Intl.DateTimeFormat("zh-CN", {
  dateStyle: "medium",
  timeStyle: "medium",
  timeZone: "UTC",
});

const source = computed(() => request.data.value);
const overview = computed(() => source.value?.overview ?? null);
const incidentSourceReady = computed(() => Boolean(source.value && !source.value.failures.some((item) => item.section === "Incident")));
const alertSourceReady = computed(() => Boolean(source.value && !source.value.failures.some((item) => item.section === "Alert")));
const agentSourceReady = computed(() => Boolean(source.value && !source.value.failures.some((item) => item.section === "Agent")));
const deliverySourceReady = computed(() => Boolean(source.value && !source.value.failures.some((item) => item.section === "Delivery/Verification")));
const atlasSourceReady = computed(() => Boolean(overview.value));
const sourceCoverage = computed(() => [
  incidentSourceReady.value,
  alertSourceReady.value,
  agentSourceReady.value,
  deliverySourceReady.value,
  atlasSourceReady.value,
].filter(Boolean).length);
const attentionKnown = computed(() => incidentSourceReady.value && alertSourceReady.value && atlasSourceReady.value);
const atlas = computed(() => overview.value?.atlas ?? null);
const incidents = computed(() => source.value?.incidents ?? []);
const allActive = computed(() => activeIncidents(incidents.value));
const active = computed(() => allActive.value.slice(0, 4));
const resolved = computed(() => recentlyResolvedIncidents(incidents.value).slice(0, 3));
const allAlerts = computed(() => unlinkedFiringAlerts(source.value?.alerts ?? []));
const alerts = computed(() => allAlerts.value.slice(0, allActive.value.length ? 3 : 6));
const latestRun = computed(() => latestAgentRun(source.value?.investigations ?? []));
const deliveries = computed(() => recentDeliveries(source.value?.devops?.deliveries ?? []));
const runningInvestigations = computed(() => (source.value?.investigations ?? [])
  .filter((item) => item.status === "pending" || item.status === "running").length);
const verifiedDeliveries = computed(() => deliveries.value
  .filter((item) => deliveryHasPassedVerification(item, incidents.value)).length);
const awaitingApproval = computed(() => allActive.value.filter((item) => item.status === "awaiting_approval").length);
const unhealthyNodes = computed(() => atlas.value?.nodes.filter((item) => item.health.state !== "healthy") ?? []);
const healthyNodes = computed(() => atlas.value?.nodes.filter((item) => item.health.state === "healthy").length ?? 0);
const providerReady = computed(() => atlas.value?.provider_state === "available" || atlas.value?.provider_state === "partial");
const previewAvailable = computed(() => providerReady.value && Boolean(atlas.value?.nodes.length) && !previewWebGLFailure.value);
const providerAvailability = computed(() => {
  const providers = overview.value?.bootstrap.provider_health ?? [];
  return {
    available: providers.filter((item) => item.state === "available").length,
    total: providers.filter((item) => item.state !== "not_configured").length,
  };
});
const topIncident = computed(() => allActive.value[0] ?? null);
const attentionCount = computed(() => allActive.value.length + allAlerts.value.length + unhealthyNodes.value.length);
const atlasHealthPercent = computed(() => {
  const total = atlas.value?.nodes.length ?? 0;
  return total ? Math.round((healthyNodes.value / total) * 100) : 0;
});
const heroVerdict = computed<{ title: string; description: string; tone: HeroTone }>(() => {
  if (!overview.value) {
    return {
      title: "运行事实尚未接通",
      description: "正在等待当前 Scope、Provider 与 Atlas 的可信投影，页面不会使用演示数据填补空白。",
      tone: "unknown",
    };
  }
  const critical = allActive.value.find((item) => item.severity === "critical");
  if (critical) {
    return {
      title: "现在需要介入",
      description: incidentTitle(critical) + " 正在影响 " + (critical.operational_context.service || critical.operational_context.resource.name || "当前运行范围") + "。",
      tone: "critical",
    };
  }
  if (allActive.value.length) {
    return {
      title: "处置正在推进",
      description: allActive.value.length + " 个 Incident 仍在调查、审批、交付或验证阶段。",
      tone: "working",
    };
  }
  if (allAlerts.value.length || unhealthyNodes.value.length) {
    return {
      title: "有信号等待判断",
      description: allAlerts.value.length + " 个未关联 Alert，" + unhealthyNodes.value.length + " 个资源异常或状态未知。",
      tone: "warning",
    };
  }
  if (providerAvailability.value.total > providerAvailability.value.available) {
    return {
      title: "运行上下文不完整",
      description: "部分 Provider 暂不可用，当前结论只覆盖已经返回的可信事实。",
      tone: "warning",
    };
  }
  return {
    title: "系统保持平稳",
    description: "当前没有活跃 Incident 或未关联 firing Alert，持续观察真实 Provider 与交付验证。",
    tone: "healthy",
  };
});
const workflowStages = computed(() => [
  { key: "signal", label: "Signal", value: incidentSourceReady.value && alertSourceReady.value ? allAlerts.value.length + allActive.value.length : "—", active: incidentSourceReady.value && alertSourceReady.value && attentionCount.value > 0 },
  { key: "investigate", label: "Investigate", value: agentSourceReady.value ? runningInvestigations.value : "—", active: agentSourceReady.value && Boolean(latestRun.value) },
  { key: "authorize", label: "Authorize", value: incidentSourceReady.value ? awaitingApproval.value : "—", active: incidentSourceReady.value && awaitingApproval.value > 0 },
  { key: "verify", label: "Verify", value: deliverySourceReady.value && incidentSourceReady.value ? verifiedDeliveries.value : "—", active: deliverySourceReady.value && deliveries.value.length > 0 },
]);
const partialFailureText = computed(() => {
  const failures = source.value?.failures ?? [];
  if (!failures.length) return "";
  const details = apiErrorDetails(failures[0].error, "只读来源暂不可用");
  const identity = [details.code, details.requestID ? "Request " + details.requestID : "", details.traceID ? "Trace " + details.traceID : ""]
    .filter(Boolean)
    .join(" · ");
  const next = details.nextSteps.length ? details.nextSteps.join("；") : "检查本地 API 与 Provider 状态后重试";
  return [failures.map((item) => item.section).join("、"), details.message, identity, next].filter(Boolean).join(" · ");
});

const severityColors = {
  critical: "error",
  warning: "warning",
  info: "info",
  unknown: "neutral",
} as const;
const alertSeverityLabels: Record<AlertView["severity"], string> = {
  critical: "严重",
  warning: "警告",
  info: "信息",
  unknown: "未知",
};

function formatUTC(value?: string): string {
  if (!value) return "未报告";
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : utcFormatter.format(parsed) + " UTC";
}

function incidentTitle(incident: IncidentView): string {
  return incident.summary || (incident.operational_context.service || "未关联服务") + " Incident";
}

function agentSummary(run: AgentRun | null): string {
  if (!run) return "当前没有可显示的 Investigation。";
  return run.answer || run.failure_summary || run.objective || "Agent 尚未形成结论。";
}

function deliveryStatus(item: DevOpsWorkspace["deliveries"][number]): string {
  const verificationStatus = latestVerificationStatusForDelivery(item, incidents.value);
  if (verificationStatus) return "Verification " + verificationStateLabel(verificationStatus);
  if (item.status === "delivered") return "Delivery 已完成 · Verification NOT RUN";
  if (item.argo_operation_phase) return item.argo_sync_status + " · " + item.argo_operation_phase;
  return item.status || "等待 Provider 投影";
}

function deliveryStatusColor(item: DevOpsWorkspace["deliveries"][number]) {
  const verificationStatus = latestVerificationStatusForDelivery(item, incidents.value);
  if (verificationStatus === "passed") return "success";
  if (item.status === "failed" || verificationStatus === "failed" || verificationStatus === "timed_out") return "error";
  return "warning";
}

function deliveryTargetHash(item: DevOpsWorkspace["deliveries"][number]): string {
  return latestVerificationStatusForDelivery(item, incidents.value) ? "#verifications" : "#delivery";
}

async function loadCommandCenter(background = false) {
  previewWebGLFailure.value = "";
  await request.run(async ({ signal }) => {
    const [overviewResult, incidentResult, alertResult, investigationResult, devopsResult] = await Promise.allSettled([
      getOverview(signal),
      listIncidents({ limit: 100 }, signal),
      listAlerts({ limit: 100, status: "firing" }, signal),
      getAgentInvestigations(signal),
      getDevOpsWorkspace(signal),
    ]);
    const failures: OverviewSectionFailure[] = [];
    if (overviewResult.status === "rejected") failures.push({ section: "运行范围与 Atlas", error: overviewResult.reason });
    if (incidentResult.status === "rejected") failures.push({ section: "Incident", error: incidentResult.reason });
    if (alertResult.status === "rejected") failures.push({ section: "Alert", error: alertResult.reason });
    if (investigationResult.status === "rejected") failures.push({ section: "Agent", error: investigationResult.reason });
    if (devopsResult.status === "rejected") failures.push({ section: "Delivery/Verification", error: devopsResult.reason });
    return {
      overview: overviewResult.status === "fulfilled" ? overviewResult.value : null,
      incidents: incidentResult.status === "fulfilled" ? incidentResult.value.items : [],
      alerts: alertResult.status === "fulfilled" ? alertResult.value.items : [],
      investigations: investigationResult.status === "fulfilled" ? investigationResult.value : [],
      devops: devopsResult.status === "fulfilled" ? devopsResult.value : null,
      failures,
    };
  }, { background });
}

function openAtlas(resource?: KubernetesResource) {
  const query: Record<string, string> = {};
  if (resource) query.resource = resource.id;
  void router.push({ name: "atlas", query });
}

function beginScopeInvestigation() {
  const current = overview.value;
  if (!current) return;
  const collectedAt = new Date(current.atlas.collected_at);
  const to = Number.isNaN(collectedAt.getTime()) ? new Date() : collectedAt;
  const from = new Date(to.getTime() - 60 * 60 * 1000);
  const contextResources = (unhealthyNodes.value.length ? unhealthyNodes.value : current.atlas.nodes.slice(0, 6))
    .slice(0, 12)
    .map((resource) => ({
      id: resource.id,
      kind: resource.kind,
      namespace: resource.namespace || "",
      name: resource.name,
    }));
  openAgentPanel({
    context: {
      route: route.fullPath,
      input: {
        title: current.bootstrap.active_scope.cluster_id + " 只读态势调查",
        cluster_id: current.bootstrap.active_scope.cluster_id,
        environment: current.bootstrap.active_scope.environment,
        namespaces: current.bootstrap.active_scope.namespaces,
        resource_refs: contextResources,
        filters: { source: "overview", mode: "read-only", provider_state: current.atlas.provider_state },
        from: from.toISOString(),
        to: to.toISOString(),
        query_definition_refs: [],
        query_execution_refs: [],
        evidence_refs: [],
      },
    },
  });
}

function handleOperationalScopeChanged() {
  void loadCommandCenter(true);
}

onMounted(() => {
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  void loadCommandCenter();
});
onBeforeUnmount(() => window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged));
</script>

<template>
  <WorkspacePageFrame
    as="article"
    width="content"
    class="overview-view"
    :data-tone="heroVerdict.tone"
    data-testid="overview-command-center"
  >
    <div class="overview-ambient" aria-hidden="true">
      <span class="ambient-wash" />
      <span class="ambient-grid" />
    </div>

    <header class="overview-intro">
      <div>
        <span class="overview-eyebrow"><i aria-hidden="true" /> Operations Agent Command Center</span>
        <p>
          {{ overview ? overview.bootstrap.active_scope.cluster_id + " · " + overview.bootstrap.active_scope.environment + " · " + (overview.bootstrap.active_scope.namespaces.join(", ") || "无 Namespace") : "等待当前 Operational Scope" }}
        </p>
      </div>
      <div class="intro-actions">
        <span v-if="overview" class="freshness-state">
          <i aria-hidden="true" />
          {{ overview.atlas.freshness.state === "fresh" ? "Live projection" : "Stale projection" }}
        </span>
        <UTooltip text="刷新全部只读态势来源">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新运维态势"
            :loading="request.refreshing.value"
            :disabled="request.loading.value"
            @click="loadCommandCenter(true)"
          />
        </UTooltip>
      </div>
    </header>

    <WorkspaceState
      v-if="request.loading.value && !source"
      class="overview-loading"
      kind="loading"
      title="正在汇总当前运维事实"
      description="并行读取 Scope、Incident、Alert、Agent 与 Delivery/Verification 投影。"
    />

    <template v-else-if="source">
      <section
        v-if="source.failures.length"
        class="source-status"
        :class="{ 'is-blocked': !overview }"
        role="status"
        aria-live="polite"
      >
        <span class="source-status-icon" aria-hidden="true">
          <UIcon :name="overview ? 'i-lucide-split' : 'i-lucide-cloud-off'" />
        </span>
        <div>
          <strong>{{ overview ? source.failures.length + " 个只读来源处于 partial" : "实时运行投影暂不可用" }}</strong>
          <p>{{ partialFailureText }}</p>
        </div>
        <UTooltip text="重试全部只读来源">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-refresh-cw"
            square
            aria-label="重试全部只读来源"
            :loading="request.refreshing.value"
            :disabled="request.loading.value"
            @click="loadCommandCenter(true)"
          />
        </UTooltip>
      </section>

      <section class="overview-hero">
        <div class="hero-copy">
          <h1 tabindex="-1">
            {{ heroVerdict.title }}<span class="hero-period">。</span>
          </h1>
          <p>{{ heroVerdict.description }}</p>
          <div class="hero-actions">
            <UButton
              class="hero-primary-action"
              color="neutral"
              variant="solid"
              icon="i-lucide-scan-search"
              label="让 Agent 调查"
              :disabled="!overview"
              data-testid="overview-readonly-investigation"
              @click="beginScopeInvestigation"
            />
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-activity"
              trailing-icon="i-lucide-arrow-right"
              label="查看 Incident"
              to="/incidents"
            />
          </div>
        </div>

        <div
          class="hero-instrument"
          :style="{ '--source-coverage': `${sourceCoverage * 72}deg` }"
        >
          <div class="instrument-head">
            <span>Current attention</span>
            <span
              class="instrument-live"
              :class="{ 'is-partial': !attentionKnown }"
            ><i aria-hidden="true" /> {{ attentionKnown ? "live" : "partial" }}</span>
          </div>
          <div class="instrument-core">
            <div class="attention-dial" aria-hidden="true">
              <div>
                <strong>{{ attentionKnown ? attentionCount : "—" }}</strong>
                <small>signals</small>
              </div>
            </div>
            <div class="coverage-copy">
              <span>Source coverage</span>
              <strong>{{ sourceCoverage }}<small>/5</small></strong>
              <p>{{ attentionKnown ? "Incident · Alert · resource projection" : "Waiting for complete read-only projection" }}</p>
            </div>
          </div>
          <div class="attention-ratio" aria-hidden="true">
            <span v-if="attentionKnown && allActive.length" class="ratio-critical" :style="{ flexGrow: allActive.length }" />
            <span v-if="attentionKnown && allAlerts.length" class="ratio-warning" :style="{ flexGrow: allAlerts.length }" />
            <span v-if="attentionKnown && unhealthyNodes.length" class="ratio-resource" :style="{ flexGrow: unhealthyNodes.length }" />
            <span v-if="!attentionKnown || !attentionCount" class="ratio-unknown" />
          </div>
          <dl>
            <div><dt>Incident</dt><dd>{{ incidentSourceReady ? allActive.length : "—" }}</dd></div>
            <div><dt>Alert</dt><dd>{{ alertSourceReady ? allAlerts.length : "—" }}</dd></div>
            <div><dt>Resources</dt><dd>{{ atlasSourceReady ? unhealthyNodes.length : "—" }}</dd></div>
          </dl>
        </div>

        <div
          class="hero-signal"
          :class="{ 'is-partial': !attentionKnown }"
          aria-label="运维闭环当前阶段"
        >
          <div class="signal-line" aria-hidden="true" />
          <div
            v-for="stage in workflowStages"
            :key="stage.key"
            class="signal-stage"
            :class="{ active: stage.active }"
          >
            <i aria-hidden="true" />
            <span>{{ stage.label }}</span>
            <strong>{{ stage.value }}</strong>
          </div>
        </div>
      </section>

      <section class="overview-stats" aria-label="当前运维状态摘要">
        <article :class="{ alerting: allActive.some((item) => item.severity === 'critical'), 'is-unknown': !incidentSourceReady }">
          <UIcon name="i-lucide-siren" aria-hidden="true" />
          <span>Active incidents</span>
          <strong>{{ incidentSourceReady ? allActive.length : "—" }}</strong>
          <small>{{ incidentSourceReady ? (awaitingApproval ? awaitingApproval + " waiting for approval" : "No pending approval") : "Source unavailable" }}</small>
        </article>
        <article :class="{ warning: allAlerts.length > 0, 'is-unknown': !alertSourceReady }">
          <UIcon name="i-lucide-bell-ring" aria-hidden="true" />
          <span>Unlinked alerts</span>
          <strong>{{ alertSourceReady ? allAlerts.length : "—" }}</strong>
          <small>{{ alertSourceReady ? "Firing without Incident" : "Source unavailable" }}</small>
        </article>
        <article :class="{ working: runningInvestigations > 0, 'is-unknown': !agentSourceReady }">
          <UIcon name="i-lucide-bot" aria-hidden="true" />
          <span>Agent runs</span>
          <strong>{{ agentSourceReady ? runningInvestigations : "—" }}</strong>
          <small>{{ agentSourceReady ? (latestRun ? latestRun.status : "No current run") : "Source unavailable" }}</small>
        </article>
        <article :class="{ healthy: verifiedDeliveries > 0, 'is-unknown': !deliverySourceReady || !incidentSourceReady }">
          <UIcon name="i-lucide-badge-check" aria-hidden="true" />
          <span>Verified</span>
          <strong>{{ deliverySourceReady && incidentSourceReady ? verifiedDeliveries : "—" }}</strong>
          <small>{{ deliverySourceReady && incidentSourceReady ? "Recent recovery proof" : "Source unavailable" }}</small>
        </article>
        <article :class="{ warning: providerAvailability.available < providerAvailability.total, 'is-unknown': !atlasSourceReady }">
          <UIcon name="i-lucide-plug-zap" aria-hidden="true" />
          <span>Providers</span>
          <strong>{{ atlasSourceReady ? providerAvailability.available + "/" + providerAvailability.total : "—" }}</strong>
          <small>{{ atlasSourceReady ? "Available integrations" : "Source unavailable" }}</small>
        </article>
      </section>

      <section class="narrative-section">
        <header class="section-heading">
          <div>
            <span class="section-eyebrow">Now / Response</span>
            <h2>当前最值得关注的处置</h2>
            <p>Incident 事实与 Agent 调查在同一条决策链中呈现。</p>
          </div>
          <UButton color="neutral" variant="ghost" label="全部事件" trailing-icon="i-lucide-arrow-right" to="/incidents" />
        </header>

        <div class="response-band">
          <section class="incident-focus" aria-labelledby="overview-incidents-title">
            <div class="focus-label">
              <span>发生了什么</span>
              <UBadge
                v-if="topIncident"
                :color="severityColors[topIncident.severity]"
                variant="subtle"
                :label="severityLabel(topIncident.severity)"
              />
            </div>
            <template v-if="topIncident">
              <h3 id="overview-incidents-title">{{ incidentTitle(topIncident) }}</h3>
              <p>{{ topIncident.operational_context.service || topIncident.operational_context.resource.name || "未关联资源" }}</p>
              <div class="incident-state">
                <span>{{ incidentStatusLabel(topIncident.status) }}</span>
                <time :datetime="topIncident.updated_at || topIncident.last_seen_at">{{ formatUTC(topIncident.updated_at || topIncident.last_seen_at) }}</time>
              </div>
              <UButton
                class="focus-action"
                color="neutral"
                variant="outline"
                label="进入处置工作台"
                trailing-icon="i-lucide-arrow-right"
                :to="{ name: 'incidents', query: { selected: topIncident.id } }"
              />
            </template>
            <div v-else class="quiet-state">
              <span><UIcon name="i-lucide-circle-check" aria-hidden="true" /></span>
              <div>
                <h3 id="overview-incidents-title">当前没有活跃 Incident</h3>
                <p>{{ resolved.length ? "近期已解决记录仍保留在审计链中。" : "继续观察 Alert、Provider 与资源状态。" }}</p>
              </div>
            </div>

            <div v-if="active.length > 1" class="incident-queue">
              <UButton
                v-for="incident in active.slice(1)"
                :key="incident.id"
                color="neutral"
                variant="ghost"
                class="queue-item"
                :to="{ name: 'incidents', query: { selected: incident.id } }"
              >
                <span><strong>{{ incidentTitle(incident) }}</strong><small>{{ incidentStatusLabel(incident.status) }}</small></span>
                <UIcon name="i-lucide-arrow-up-right" aria-hidden="true" />
              </UButton>
            </div>

            <div v-if="alerts.length" class="alert-queue">
              <span>Unlinked signal</span>
              <UButton
                v-for="alert in alerts.slice(0, 2)"
                :key="alert.id"
                color="neutral"
                variant="ghost"
                class="queue-item"
                :to="{ name: 'alert-detail', params: { alertId: alert.id } }"
              >
                <span><strong>{{ alert.summary }}</strong><small>{{ alert.service_name || alert.target_name || alert.namespace }}</small></span>
                <UBadge :color="severityColors[alert.severity]" variant="outline" :label="alertSeverityLabels[alert.severity]" />
              </UButton>
            </div>
          </section>

          <section class="agent-focus" aria-labelledby="overview-agent-title">
            <div class="focus-label">
              <span>Agent 调查到了什么</span>
              <span v-if="latestRun" class="agent-run-state"><i aria-hidden="true" /> {{ latestRun.status }}</span>
            </div>
            <template v-if="latestRun">
              <h3 id="overview-agent-title">最新调查结论</h3>
              <p class="agent-conclusion">{{ agentSummary(latestRun) }}</p>
              <dl class="agent-boundary">
                <div><dt>Evidence</dt><dd>{{ latestRun.evidence_count }}</dd></div>
                <div><dt>Confidence boundary</dt><dd>{{ latestRun.uncertainty || "未报告" }}</dd></div>
                <div><dt>Pending</dt><dd>{{ pendingAgentItems(latestRun) }}</dd></div>
              </dl>
              <ol v-if="latestRun.evidence_citations.length" class="evidence-list">
                <li v-for="citation in latestRun.evidence_citations.slice(0, 2)" :key="citation.id">
                  <i aria-hidden="true" />
                  <span>{{ citation.summary }}<small>{{ citation.source }}</small></span>
                </li>
              </ol>
              <div class="agent-focus-actions">
                <UButton
                  color="neutral"
                  variant="solid"
                  label="继续调查"
                  icon="i-lucide-sparkles"
                  :to="{ name: 'agent', query: { investigation: latestRun.id } }"
                />
                <UButton
                  color="neutral"
                  variant="ghost"
                  label="在 Dock 中打开"
                  icon="i-lucide-panel-right-open"
                  @click="openAgentPanel()"
                />
              </div>
            </template>
            <div v-else class="quiet-state agent-empty">
              <span><UIcon name="i-lucide-sparkles" aria-hidden="true" /></span>
              <div>
                <h3 id="overview-agent-title">尚无 Agent Investigation</h3>
                <p>从当前 Scope 发起只读调查，不会执行审批或变更。</p>
              </div>
              <UButton color="neutral" variant="solid" label="开始调查" :disabled="!overview" @click="beginScopeInvestigation" />
            </div>
          </section>
        </div>
      </section>

      <section class="atlas-section" aria-labelledby="overview-atlas-title">
        <header class="section-heading">
          <div>
            <span class="section-eyebrow">Topology / Context</span>
            <h2 id="overview-atlas-title">Operations Atlas</h2>
            <p>来自当前 Kubernetes Provider 的真实节点、关系与健康状态。</p>
          </div>
          <div class="atlas-summary">
            <span><strong>{{ atlas?.nodes.length || 0 }}</strong> nodes</span>
            <span><strong>{{ atlasHealthPercent }}%</strong> healthy</span>
            <UButton color="neutral" variant="outline" label="进入 Atlas" trailing-icon="i-lucide-maximize-2" to="/atlas" />
          </div>
        </header>
        <div class="atlas-stage">
          <OperationsAtlas
            v-if="atlas && previewAvailable"
            :snapshot="atlas"
            @select="openAtlas"
            @unavailable="previewWebGLFailure = $event"
          />
          <WorkspaceState
            v-else-if="atlas && !atlas.nodes.length"
            kind="empty"
            title="当前 Scope 没有拓扑节点"
            description="没有为首页填充演示数据。"
          />
          <WorkspaceState
            v-else-if="atlas && !providerReady"
            kind="error"
            title="Kubernetes Provider 不可用"
            :description="atlas.provider_detail"
          />
          <WorkspaceState
            v-else-if="previewWebGLFailure"
            kind="partial"
            title="3D 预览不可用"
            description="可进入完整 Atlas 使用结构化等价视图。"
          />
          <WorkspaceState v-else kind="loading" title="正在读取 Atlas 预览" />
          <div v-if="atlas" class="atlas-caption">
            <span><i aria-hidden="true" /> {{ atlas.freshness.state }}</span>
            <span>{{ unhealthyNodes.length }} abnormal / unknown</span>
            <small>{{ atlas.source.identity }}</small>
          </div>
        </div>
      </section>

      <section class="delivery-section">
        <header class="section-heading">
          <div>
            <span class="section-eyebrow">Recovery / Proof</span>
            <h2>交付是否真正恢复了系统</h2>
            <p>Delivery 只有经过 Verification 才能成为恢复结论。</p>
          </div>
          <UButton color="neutral" variant="ghost" label="查看 DevOps" trailing-icon="i-lucide-arrow-right" to="/devops" />
        </header>

        <div v-if="deliveries.length" class="delivery-stream">
          <UButton
            v-for="(delivery, index) in deliveries"
            :key="delivery.id"
            color="neutral"
            variant="ghost"
            class="delivery-entry"
            :to="{ name: 'incident-detail', params: { incidentId: delivery.incident_id }, hash: deliveryTargetHash(delivery) }"
          >
            <span class="delivery-index">{{ String(index + 1).padStart(2, "0") }}</span>
            <span class="delivery-copy">
              <strong>{{ delivery.repository || delivery.argo_application || delivery.id }}</strong>
              <small>{{ delivery.commit_sha || delivery.target_revision || "无 revision" }}</small>
            </span>
            <UBadge :color="deliveryStatusColor(delivery)" variant="subtle" :label="deliveryStatus(delivery)" />
            <time :datetime="delivery.last_observed_at">{{ formatUTC(delivery.last_observed_at) }}</time>
            <UIcon name="i-lucide-arrow-up-right" aria-hidden="true" />
          </UButton>
        </div>
        <WorkspaceState
          v-else
          class="delivery-empty"
          kind="empty"
          title="近期没有 Delivery 投影"
          description="不会把 dispatched 或 accepted 推断为恢复成功。"
        />
      </section>

      <nav class="workspace-links" aria-label="常用工作区">
        <RouterLink to="/infrastructure"><UIcon name="i-lucide-boxes" /><span><strong>资源</strong><small>查看当前基础设施</small></span><UIcon name="i-lucide-arrow-up-right" /></RouterLink>
        <RouterLink to="/monitoring"><UIcon name="i-lucide-chart-no-axes-combined" /><span><strong>可观测性</strong><small>指标、日志与链路</small></span><UIcon name="i-lucide-arrow-up-right" /></RouterLink>
        <RouterLink to="/agent"><UIcon name="i-lucide-sparkles" /><span><strong>Agent</strong><small>进入完整调查台</small></span><UIcon name="i-lucide-arrow-up-right" /></RouterLink>
        <RouterLink to="/settings"><UIcon name="i-lucide-sliders-horizontal" /><span><strong>控制</strong><small>Provider 与配置</small></span><UIcon name="i-lucide-arrow-up-right" /></RouterLink>
      </nav>
    </template>
  </WorkspacePageFrame>
</template>

<style scoped>
.overview-view {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 0;
}
.overview-ambient {
  position: absolute;
  inset: 0 -34px auto;
  height: 590px;
  z-index: -1;
  overflow: hidden;
  pointer-events: none;
}
.ambient-wash {
  position: absolute;
  top: 0;
  left: 50%;
  width: min(980px, 96%);
  height: 500px;
  transform: translateX(-50%);
  background: radial-gradient(ellipse 75% 70% at 50% 12%, color-mix(in srgb, var(--co-viz-live) 11%, transparent), transparent 72%);
  mask-image: linear-gradient(to bottom, transparent, #000 22%, #000 62%, transparent);
}
.overview-view[data-tone="critical"] .ambient-wash {
  background: radial-gradient(ellipse 75% 70% at 50% 12%, color-mix(in srgb, var(--co-viz-failure) 11%, transparent), transparent 72%);
}
.overview-view[data-tone="warning"] .ambient-wash,
.overview-view[data-tone="working"] .ambient-wash {
  background: radial-gradient(ellipse 75% 70% at 50% 12%, color-mix(in srgb, var(--co-viz-amber) 10%, transparent), transparent 72%);
}
.ambient-grid {
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(to right, var(--co-border-default) 1px, transparent 1px),
    linear-gradient(to bottom, var(--co-border-default) 1px, transparent 1px);
  background-size: 72px 72px;
  opacity: .24;
  mask-image: radial-gradient(ellipse 70% 58% at 50% 0%, #000 0%, transparent 78%);
}

.overview-intro {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.overview-intro > div:first-child { display: grid; min-width: 0; gap: 4px; }
.overview-eyebrow,
.section-eyebrow {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .1em;
  text-transform: uppercase;
}
.overview-eyebrow i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--co-viz-live);
  box-shadow: 0 0 0 4px var(--co-viz-live-soft);
}
.overview-intro p { margin: 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 11px; }
.intro-actions { display: flex; flex: 0 0 auto; align-items: center; gap: 8px; }
.freshness-state {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
}
.freshness-state i { width: 5px; height: 5px; border-radius: 50%; background: var(--co-viz-live); }
.overview-loading { min-height: 420px; margin-top: 28px; }
.source-status {
  display: grid;
  min-width: 0;
  min-height: 56px;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding: 9px 10px 9px 12px;
  border: 1px solid var(--co-status-warning-border);
  border-radius: 12px;
  background: color-mix(in srgb, var(--co-status-warning-bg) 38%, var(--co-bg-floating));
  box-shadow: var(--co-shadow-subtle);
}
.source-status.is-blocked { border-color: color-mix(in srgb, var(--co-status-critical-border) 72%, var(--co-border-default)); background: color-mix(in srgb, var(--co-status-critical-bg) 28%, var(--co-bg-floating)); }
.source-status-icon { display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 9px; color: var(--co-status-warning-fg); background: var(--co-bg-floating); }
.source-status.is-blocked .source-status-icon { color: var(--co-status-critical-fg); }
.source-status-icon :deep(svg) { width: 15px; height: 15px; }
.source-status > div { display: grid; min-width: 0; gap: 2px; }
.source-status strong { color: var(--co-text-primary); font-size: 12px; }
.source-status p { margin: 0; overflow: hidden; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.source-status :deep(button) { width: 32px; min-width: 32px; height: 32px; border-radius: 8px; }

.overview-hero {
  position: relative;
  display: grid;
  min-height: 388px;
  grid-template-columns: minmax(0, 1.12fr) minmax(300px, .88fr);
  align-items: center;
  gap: 48px;
  margin-top: 14px;
  padding: 18px 0 92px;
}
.hero-copy { position: relative; z-index: 1; display: flex; min-width: 0; align-items: flex-start; flex-direction: column; gap: 18px; }
.hero-copy h1 {
  max-width: 760px;
  margin: 0;
  color: var(--co-text-primary);
  font-size: 68px;
  font-weight: 750;
  letter-spacing: 0;
  line-height: 1.05;
}
.hero-copy h1:focus { outline: none; }
.hero-period { color: var(--co-viz-live); }
[data-tone="critical"] .hero-period { color: var(--co-viz-failure); }
[data-tone="warning"] .hero-period,
[data-tone="working"] .hero-period { color: var(--co-viz-amber); }
.hero-copy > p { max-width: 620px; margin: 0; color: var(--co-text-secondary); font-size: 15px; line-height: 1.75; }
.hero-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }
.hero-primary-action {
  min-height: 44px;
  padding-inline: 18px;
  border-radius: 999px;
  color: var(--co-bg-canvas);
  background: var(--co-ink-action);
  box-shadow: 0 14px 30px color-mix(in srgb, var(--co-ink-action) 20%, transparent);
}
.hero-primary-action:hover { transform: translateY(-1px); }

.hero-instrument {
  position: relative;
  z-index: 1;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 14px;
  padding: 22px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  border-radius: 20px;
  background: linear-gradient(145deg, color-mix(in srgb, var(--co-bg-floating) 88%, transparent), color-mix(in srgb, var(--co-bg-surface) 66%, transparent));
  box-shadow: var(--co-shadow-panel);
  backdrop-filter: blur(14px);
}
.instrument-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.instrument-head > span:first-child {
  color: var(--co-text-muted);
  font-family: var(--co-font-mono);
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .12em;
  text-transform: uppercase;
}
.instrument-live { display: inline-flex; align-items: center; gap: 6px; color: var(--co-status-success-fg); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.instrument-live i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-viz-live); box-shadow: 0 0 0 4px var(--co-viz-live-soft); }
.instrument-live.is-partial { color: var(--co-status-warning-fg); }
.instrument-live.is-partial i { background: var(--co-viz-amber); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-viz-amber) 13%, transparent); }
.instrument-core { display: grid; min-width: 0; grid-template-columns: 132px minmax(0, 1fr); align-items: center; gap: 20px; }
.attention-dial {
  position: relative;
  display: grid;
  width: 132px;
  height: 132px;
  place-items: center;
  border-radius: 50%;
  background: conic-gradient(from -90deg, var(--co-viz-live) 0 var(--source-coverage), var(--co-border-default) var(--source-coverage) 360deg);
  box-shadow: 0 16px 34px rgb(52 46 39 / 10%);
}
.attention-dial::before {
  position: absolute;
  inset: 7px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  border-radius: inherit;
  background: color-mix(in srgb, var(--co-bg-floating) 94%, transparent);
  box-shadow: inset 0 0 28px color-mix(in srgb, var(--co-bg-surface) 65%, transparent);
  content: "";
}
.attention-dial > div { position: relative; z-index: 1; display: grid; justify-items: center; gap: 2px; }
.attention-dial strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 42px; font-weight: 620; line-height: 1; font-variant-numeric: tabular-nums; }
.attention-dial small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 700; text-transform: uppercase; }
.coverage-copy { display: grid; min-width: 0; align-content: center; gap: 3px; }
.coverage-copy > span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; }
.coverage-copy > strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 34px; font-weight: 650; line-height: 1.15; font-variant-numeric: tabular-nums; }
.coverage-copy > strong small { margin-left: 3px; color: var(--co-text-muted); font-size: 12px; font-weight: 650; }
.coverage-copy p { margin: 5px 0 0; color: var(--co-text-secondary); font-family: var(--co-font-mono); font-size: 9px; line-height: 1.5; }
.attention-ratio { display: flex; height: 5px; gap: 2px; }
.attention-ratio span { min-width: 4px; border-radius: 3px; }
.ratio-critical { background: var(--co-viz-failure); }
.ratio-warning { background: var(--co-viz-amber); }
.ratio-resource { background: var(--co-viz-live); }
.ratio-unknown { flex: 1 1 auto; background: var(--co-border-strong); }
.hero-instrument dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; padding-top: 12px; border-top: 1px solid var(--co-border-default); }
.hero-instrument dl div { display: grid; gap: 2px; padding-inline: 12px; border-right: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent); }
.hero-instrument dl div:first-child { padding-left: 0; }
.hero-instrument dl div:last-child { padding-right: 0; border-right: 0; }
.hero-instrument dt { color: var(--co-text-muted); font-size: 9px; }
.hero-instrument dd { margin: 0; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 13px; font-weight: 750; }

.hero-signal {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  display: grid;
  height: 86px;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  align-items: center;
}
.signal-line { position: absolute; right: 2%; left: 2%; top: 27px; height: 1px; background: linear-gradient(90deg, transparent, var(--co-border-strong) 8%, var(--co-border-strong) 92%, transparent); }
.signal-line::after {
  content: "";
  position: absolute;
  top: -1px;
  left: 0;
  width: 18%;
  height: 3px;
  border-radius: 999px;
  background: var(--co-viz-live);
  box-shadow: 0 0 16px color-mix(in srgb, var(--co-viz-live) 55%, transparent);
  animation: signal-travel 5s cubic-bezier(.23, 1, .32, 1) infinite;
}
.hero-signal.is-partial .signal-line::after {
  width: 12%;
  background: color-mix(in srgb, var(--co-viz-live) 44%, var(--co-border-strong));
  box-shadow: 0 0 14px color-mix(in srgb, var(--co-viz-live) 20%, transparent);
  animation: signal-probe 4.6s ease-in-out infinite;
  opacity: .72;
}
.signal-stage { position: relative; z-index: 1; display: grid; justify-items: center; gap: 4px; color: var(--co-text-muted); }
.signal-stage i { width: 11px; height: 11px; border: 3px solid var(--co-bg-canvas); border-radius: 50%; background: var(--co-border-strong); box-shadow: 0 0 0 1px var(--co-border-default); }
.signal-stage.active i { background: var(--co-viz-live); box-shadow: 0 0 0 1px color-mix(in srgb, var(--co-viz-live) 48%, transparent), 0 0 14px var(--co-viz-live-soft); }
.signal-stage span { font-family: var(--co-font-mono); font-size: 9px; font-weight: 700; text-transform: uppercase; }
.signal-stage strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 11px; }

.overview-stats {
  counter-reset: overview-stat;
  display: grid;
  grid-template-columns: 1.18fr repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin-top: 28px;
  overflow: visible;
  border: 0;
  background: transparent;
  box-shadow: none;
}
.overview-stats article {
  position: relative;
  counter-increment: overview-stat;
  display: grid;
  min-width: 0;
  min-height: 116px;
  grid-template-columns: 28px minmax(0, 1fr);
  grid-template-rows: auto auto 1fr;
  column-gap: 9px;
  padding: 18px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 86%, transparent);
  border-radius: 13px;
  background: color-mix(in srgb, var(--co-bg-surface) 70%, transparent);
  box-shadow: var(--co-shadow-row);
}
.overview-stats article::before { position: absolute; top: 0; right: 18px; left: 18px; height: 2px; border-radius: 0 0 999px 999px; background: var(--co-border-strong); content: ""; }
.overview-stats article::after { position: absolute; top: 9px; right: 11px; color: color-mix(in srgb, var(--co-text-muted) 42%, transparent); font-family: var(--co-font-mono); font-size: 7px; font-weight: 800; content: "0" counter(overview-stat); }
.overview-stats article.alerting::before { background: var(--co-viz-failure); }
.overview-stats article.warning::before { background: var(--co-viz-amber); }
.overview-stats article.working::before,
.overview-stats article.healthy::before { background: var(--co-viz-live); }
.overview-stats article.is-unknown::before { background: var(--co-border-strong); }
.overview-stats article > :deep(svg) { grid-row: 1 / 3; align-self: center; width: 18px; height: 18px; color: var(--co-text-muted); }
.overview-stats article.alerting > :deep(svg) { color: var(--co-status-critical-fg); }
.overview-stats article.warning > :deep(svg) { color: var(--co-status-warning-fg); }
.overview-stats article.working > :deep(svg),
.overview-stats article.healthy > :deep(svg) { color: var(--co-status-success-fg); }
.overview-stats span { align-self: end; color: var(--co-text-secondary); font-size: 10px; font-weight: 700; }
.overview-stats strong { color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 27px; font-weight: 650; line-height: 1.05; font-variant-numeric: tabular-nums; }
.overview-stats small { grid-column: 1 / -1; align-self: end; overflow: hidden; color: var(--co-text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }

.narrative-section,
.atlas-section,
.delivery-section { display: flex; min-width: 0; flex-direction: column; gap: 24px; margin-top: var(--co-section-separation); }
.section-heading { display: flex; min-width: 0; align-items: flex-end; justify-content: space-between; gap: 24px; }
.section-heading > div:first-child { display: grid; min-width: 0; gap: 6px; }
.section-eyebrow::before { content: "▍"; color: var(--co-viz-live); }
.section-heading h2 { margin: 0; color: var(--co-text-primary); font-size: 28px; font-weight: 680; letter-spacing: 0; line-height: 1.2; }
.section-heading p { max-width: 680px; margin: 0; color: var(--co-text-secondary); font-size: 13px; line-height: 1.6; }

.response-band {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, .88fr) minmax(0, 1.12fr);
  gap: 34px;
  background: transparent;
}
.response-band > section {
  position: relative;
  min-width: 0;
  overflow: hidden;
  min-height: 188px;
  padding: 24px 22px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 86%, transparent);
  border-radius: 16px;
  background: color-mix(in srgb, var(--co-bg-surface) 44%, transparent);
  box-shadow: var(--co-shadow-section);
}
.incident-focus { box-shadow: inset 2px 0 0 color-mix(in srgb, var(--co-viz-live) 58%, transparent), var(--co-shadow-section); }
.agent-focus { background: linear-gradient(90deg, color-mix(in srgb, var(--co-viz-live) 3%, transparent), transparent 72%), color-mix(in srgb, var(--co-bg-surface) 44%, transparent); box-shadow: inset 1px 0 0 color-mix(in srgb, var(--co-viz-live) 34%, var(--co-border-default)), var(--co-shadow-section); }
.focus-label { display: flex; min-height: 24px; align-items: center; justify-content: space-between; gap: 12px; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; letter-spacing: .09em; text-transform: uppercase; }
.response-band h3 { margin: 16px 0 8px; color: var(--co-text-primary); font-size: 20px; font-weight: 680; line-height: 1.35; }
.incident-focus > p { margin: 0; color: var(--co-text-secondary); font-size: 12px; }
.incident-state { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 10px; margin-top: 22px; padding-block: 12px; border-block: 1px solid var(--co-border-default); color: var(--co-text-secondary); font-size: 10px; }
.incident-state span { font-weight: 750; }
.incident-state time { color: var(--co-text-muted); font-family: var(--co-font-mono); }
.focus-action { margin-top: 18px; }
.agent-run-state { display: inline-flex; align-items: center; gap: 6px; color: var(--co-status-success-fg); }
.agent-run-state i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-viz-live); }
.agent-conclusion { display: -webkit-box; margin: 14px 0 18px; overflow: hidden; color: var(--co-text-primary); font-size: 14px; line-height: 1.75; -webkit-box-orient: vertical; -webkit-line-clamp: 4; }
.agent-boundary { display: grid; grid-template-columns: .7fr 1.6fr .7fr; margin: 0; padding-block: 12px; border-block: 1px solid var(--co-border-default); }
.agent-boundary div { display: grid; min-width: 0; gap: 3px; padding-right: 12px; }
.agent-boundary dt { color: var(--co-text-muted); font-size: 9px; }
.agent-boundary dd { margin: 0; overflow: hidden; color: var(--co-text-primary); font-family: var(--co-font-mono); font-size: 11px; font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.evidence-list { display: grid; gap: 8px; margin: 16px 0 0; padding: 0; list-style: none; }
.evidence-list li { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: start; gap: 9px; color: var(--co-text-secondary); font-size: 11px; }
.evidence-list li > i { width: 6px; height: 6px; margin-top: 5px; border-radius: 50%; background: var(--co-viz-live); }
.evidence-list li span { display: grid; min-width: 0; }
.evidence-list small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; }
.agent-focus-actions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 18px; }
.agent-focus-actions :deep(button:first-child),
.agent-focus-actions :deep(a:first-child) { color: var(--co-bg-canvas); background: var(--co-ink-action); }
.quiet-state { display: grid; min-height: 140px; grid-template-columns: auto minmax(0, 1fr); align-content: center; align-items: center; gap: 14px; }
.quiet-state > span { display: grid; width: 44px; height: 44px; place-items: center; border: 1px solid var(--co-status-success-border); border-radius: 14px; color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.quiet-state h3 { margin: 0 0 4px; }
.quiet-state p { margin: 0; color: var(--co-text-secondary); font-size: 11px; }
.agent-empty { grid-template-columns: auto minmax(0, 1fr) auto; }
.incident-queue,
.alert-queue { display: grid; gap: 4px; margin-top: 18px; padding-top: 14px; border-top: 1px solid var(--co-border-default); }
.alert-queue > span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 8px; font-weight: 800; text-transform: uppercase; }
.queue-item { display: grid; width: 100%; min-width: 0; grid-template-columns: minmax(0, 1fr) auto; justify-content: stretch; text-align: left; }
.queue-item > span { display: grid; min-width: 0; }
.queue-item strong,
.queue-item small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.queue-item strong { font-size: 11px; }
.queue-item small { color: var(--co-text-muted); font-size: 9px; }

.atlas-summary { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 16px; color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; text-transform: uppercase; }
.atlas-summary strong { color: var(--co-text-primary); font-size: 14px; }
.atlas-stage {
  position: relative;
  min-height: 410px;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 88%, transparent);
  border-radius: 16px;
  background-color: var(--co-bg-surface);
  background-image:
    linear-gradient(to right, color-mix(in srgb, var(--co-border-default) 52%, transparent) 1px, transparent 1px),
    linear-gradient(to bottom, color-mix(in srgb, var(--co-border-default) 52%, transparent) 1px, transparent 1px);
  background-size: 64px 64px;
  box-shadow: inset 0 1px 0 color-mix(in srgb, var(--co-bg-floating) 70%, transparent);
}
.atlas-stage > :deep(*) { min-height: 100%; }
.atlas-stage :deep(.workspace-state-loading) { position: absolute; inset: 0; min-height: 0; place-content: center; border: 0; background: transparent; }
.atlas-stage :deep(.workspace-state-heading) { width: fit-content; min-width: 260px; padding: 16px 18px; border: 1px solid var(--co-border-default); border-radius: 13px; background: color-mix(in srgb, var(--co-bg-floating) 88%, transparent); box-shadow: var(--co-shadow-panel); backdrop-filter: blur(12px); }
.atlas-stage :deep(.workspace-state-heading strong) { font-size: 15px; }
.atlas-stage :deep(.workspace-state-skeleton) { display: none; }
.atlas-caption {
  position: absolute;
  right: 18px;
  bottom: 18px;
  left: 18px;
  min-height: 0;
  display: grid;
  grid-template-columns: auto auto minmax(0, 1fr);
  align-items: center;
  gap: 14px;
  padding: 10px 12px;
  border: 1px solid color-mix(in srgb, var(--co-border-default) 72%, transparent);
  border-radius: 11px;
  color: var(--co-text-secondary);
  background: color-mix(in srgb, var(--co-bg-floating) 78%, transparent);
  box-shadow: var(--co-shadow-floating);
  backdrop-filter: blur(14px);
  font-family: var(--co-font-mono);
  font-size: 9px;
  pointer-events: none;
}
.atlas-caption span { display: inline-flex; align-items: center; gap: 6px; white-space: nowrap; }
.atlas-caption i { width: 6px; height: 6px; border-radius: 50%; background: var(--co-viz-live); }
.atlas-caption small { overflow: hidden; color: var(--co-text-muted); text-align: right; text-overflow: ellipsis; white-space: nowrap; }

.delivery-stream { display: grid; gap: 9px; }
.delivery-entry {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 68px;
  grid-template-columns: 28px minmax(0, 1fr) auto auto auto;
  align-items: center;
  gap: 14px;
  padding-inline: 14px;
  border: 1px solid var(--co-border-default);
  border-radius: 11px;
  background: color-mix(in srgb, var(--co-bg-surface) 68%, transparent);
  text-align: left;
}
.delivery-index { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; }
.delivery-copy { display: grid; min-width: 0; gap: 3px; }
.delivery-copy strong,
.delivery-copy small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.delivery-copy strong { color: var(--co-text-primary); font-size: 12px; }
.delivery-copy small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.delivery-entry time { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.delivery-empty { min-height: 112px; align-items: center; justify-content: flex-start; border: 1px solid color-mix(in srgb, var(--co-border-default) 86%, transparent); border-radius: 14px; background: color-mix(in srgb, var(--co-bg-surface) 62%, transparent); }

.workspace-links { display: grid; grid-template-columns: 1.08fr 1.18fr .9fr 1fr; gap: 24px; margin-top: var(--co-section-separation); padding-bottom: 16px; }
.workspace-links a {
  display: grid;
  min-width: 0;
  min-height: 74px;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 12px;
  padding: 14px 12px;
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-frame);
  background: color-mix(in srgb, var(--co-bg-surface) 54%, transparent);
  transition: transform 180ms var(--co-ease-out), border-color 180ms var(--co-ease-out), box-shadow 180ms var(--co-ease-out);
}
.workspace-links a:hover { border-color: var(--co-viz-live); box-shadow: none; transform: translateY(-2px); }
.workspace-links a > :deep(svg:first-child) { width: 19px; height: 19px; color: var(--co-text-secondary); }
.workspace-links a > :deep(svg:last-child) { width: 14px; height: 14px; color: var(--co-text-muted); }
.workspace-links span { display: grid; min-width: 0; gap: 3px; }
.workspace-links strong { color: var(--co-text-primary); font-size: 12px; }
.workspace-links small { overflow: hidden; color: var(--co-text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }

@keyframes signal-travel {
  0% { left: 0; opacity: 0; }
  12% { opacity: 1; }
  82% { opacity: 1; }
  100% { left: 82%; opacity: 0; }
}

@keyframes signal-probe {
  0% { left: 0; opacity: 0; }
  15% { opacity: .72; }
  50% { opacity: .48; }
  85% { opacity: .72; }
  100% { left: 88%; opacity: 0; }
}

@media (max-width: 1180px) {
  .overview-view { gap: 0; }
  .overview-hero { gap: 34px; }
  .hero-copy h1 { font-size: 48px; }
  .instrument-core { grid-template-columns: 110px minmax(0, 1fr); gap: 14px; }
  .attention-dial { width: 110px; height: 110px; }
  .attention-dial strong { font-size: 34px; }
  .overview-stats { grid-template-columns: repeat(5, minmax(122px, 1fr)); gap: 9px; }
  .overview-stats article { min-height: 112px; padding: 15px; }
  .response-band > section { padding: 24px; }
  .workspace-links { gap: 10px; }
}

@media (max-width: 1024px) {
  .overview-hero { grid-template-columns: minmax(0, 1fr) 300px; }
  .hero-copy h1 { font-size: 44px; }
  .overview-stats { overflow-x: auto; }
  .response-band { grid-template-columns: minmax(0, 1fr); }
  .workspace-links { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (prefers-reduced-motion: reduce) {
  .signal-line::after { animation: none; left: 41%; opacity: 1; }
  .hero-primary-action:hover,
  .workspace-links a:hover { transform: none; }
}
</style>
