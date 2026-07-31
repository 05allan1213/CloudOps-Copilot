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
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
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
const atlas = computed(() => overview.value?.atlas ?? null);
const incidents = computed(() => source.value?.incidents ?? []);
const active = computed(() => activeIncidents(incidents.value).slice(0, 4));
const resolved = computed(() => recentlyResolvedIncidents(incidents.value).slice(0, 3));
const alerts = computed(() => unlinkedFiringAlerts(source.value?.alerts ?? []).slice(0, active.value.length ? 3 : 6));
const latestRun = computed(() => latestAgentRun(source.value?.investigations ?? []));
const deliveries = computed(() => recentDeliveries(source.value?.devops?.deliveries ?? []));
const runningInvestigations = computed(() => (source.value?.investigations ?? [])
  .filter((item) => item.status === "pending" || item.status === "running").length);
const verifiedDeliveries = computed(() => deliveries.value
  .filter((item) => deliveryHasPassedVerification(item, incidents.value)).length);
const awaitingApproval = computed(() => active.value.filter((item) => item.status === "awaiting_approval").length);
const unhealthyNodes = computed(() => atlas.value?.nodes.filter((item) => item.health.state !== "healthy") ?? []);
const providerReady = computed(() => atlas.value?.provider_state === "available" || atlas.value?.provider_state === "partial");
const previewAvailable = computed(() => providerReady.value && Boolean(atlas.value?.nodes.length) && !previewWebGLFailure.value);
const providerAvailability = computed(() => {
  const providers = overview.value?.bootstrap.provider_health ?? [];
  return {
    available: providers.filter((item) => item.state === "available").length,
    total: providers.filter((item) => item.state !== "not_configured").length,
  };
});
const partialFailureText = computed(() => {
  const failures = source.value?.failures ?? [];
  if (!failures.length) return "";
  const details = apiErrorDetails(failures[0].error, "只读来源暂不可用");
  const identity = [details.code, details.requestID ? `Request ${details.requestID}` : "", details.traceID ? `Trace ${details.traceID}` : ""]
    .filter(Boolean)
    .join(" · ");
  return `${failures.map((item) => item.section).join("、")}。${identity}`;
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
  return Number.isNaN(parsed.getTime()) ? value : `${utcFormatter.format(parsed)} UTC`;
}

function incidentTitle(incident: IncidentView): string {
  return incident.summary || `${incident.operational_context.service || "未关联服务"} Incident`;
}

function agentSummary(run: AgentRun | null): string {
  if (!run) return "当前没有可显示的 Investigation。";
  return run.answer || run.failure_summary || run.objective || "Agent 尚未形成结论。";
}

function deliveryStatus(item: DevOpsWorkspace["deliveries"][number]): string {
  const verificationStatus = latestVerificationStatusForDelivery(item, incidents.value);
  if (verificationStatus) return `Verification ${verificationStateLabel(verificationStatus)}`;
  if (item.status === "delivered") return "Delivery 已完成 · Verification NOT RUN";
  if (item.argo_operation_phase) return `${item.argo_sync_status} · ${item.argo_operation_phase}`;
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
        title: `${current.bootstrap.active_scope.cluster_id} 只读态势调查`,
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
  <article
    class="overview-view"
    data-testid="overview-command-center"
  >
    <WorkspaceHeader
      title="运维态势"
      eyebrow="Operations Agent Command Center"
      :description="overview ? `${overview.bootstrap.active_scope.cluster_id} · ${overview.bootstrap.active_scope.environment} · ${overview.bootstrap.active_scope.namespaces.join(', ') || '无 Namespace'}` : '读取当前 Operational Scope 与运维事实。'"
    >
      <template #context>
        <UBadge
          v-if="overview"
          color="neutral"
          variant="outline"
          icon="i-lucide-network"
          :label="overview.bootstrap.active_scope.name"
        />
        <UBadge
          v-if="overview"
          :color="overview.atlas.freshness.state === 'fresh' ? 'success' : 'warning'"
          variant="subtle"
          :label="overview.atlas.freshness.state === 'fresh' ? '当前投影' : '投影已陈旧'"
        />
      </template>
      <template #actions>
        <UTooltip text="携带当前 Scope 打开只读调查上下文">
          <UButton
            color="primary"
            icon="i-lucide-search"
            label="只读调查"
            :disabled="!overview"
            data-testid="overview-readonly-investigation"
            @click="beginScopeInvestigation"
          />
        </UTooltip>
        <UTooltip text="刷新全部只读态势来源">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新运维态势"
            :loading="request.refreshing.value"
            :disabled="request.loading.value"
            @click="loadCommandCenter(true)"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

    <WorkspaceState
      v-if="request.loading.value && !source"
      kind="loading"
      title="正在汇总当前运维事实"
      description="并行读取 Scope、Incident、Alert、Agent 与 Delivery/Verification 投影。"
    />

    <template v-else-if="source">
      <UAlert
        v-if="source.failures.length"
        color="warning"
        variant="soft"
        icon="i-lucide-split"
        :title="`${source.failures.length} 个只读来源暂不可用`"
        :description="partialFailureText"
        role="status"
      />

      <ApiErrorNotice
        v-if="!overview && source.failures[0]"
        :error="source.failures[0].error"
        title="运行范围与 Atlas 读取失败"
        retryable
        @retry="loadCommandCenter()"
      />

      <section
        class="overview-status-strip"
        aria-label="当前运维状态摘要"
      >
        <div :class="{ 'is-critical': active.some((item) => item.severity === 'critical') }">
          <UIcon
            name="i-lucide-siren"
            aria-hidden="true"
          />
          <span>活跃 Incident</span><strong>{{ active.length }}</strong>
        </div>
        <div :class="{ 'is-warning': alerts.length > 0 }">
          <UIcon
            name="i-lucide-triangle-alert"
            aria-hidden="true"
          />
          <span>未关联 Alert</span><strong>{{ alerts.length }}</strong>
        </div>
        <div :class="{ 'is-info': runningInvestigations > 0 }">
          <UIcon
            name="i-lucide-bot"
            aria-hidden="true"
          />
          <span>Agent 调查中</span><strong>{{ runningInvestigations }}</strong>
        </div>
        <div :class="{ 'is-success': verifiedDeliveries > 0 }">
          <UIcon
            name="i-lucide-badge-check"
            aria-hidden="true"
          />
          <span>近期已验证</span><strong>{{ verifiedDeliveries }}</strong>
        </div>
        <div :class="{ 'is-warning': providerAvailability.available < providerAvailability.total }">
          <UIcon
            name="i-lucide-plug-zap"
            aria-hidden="true"
          />
          <span>Provider</span><strong>{{ providerAvailability.available }}/{{ providerAvailability.total }}</strong>
        </div>
      </section>

      <section class="overview-grid overview-grid-primary">
        <section
          class="overview-panel incident-panel"
          aria-labelledby="overview-incidents-title"
        >
          <header>
            <div>
              <span>发生了什么</span>
              <h2 id="overview-incidents-title">
                活跃 Incident 与 Alert
              </h2>
            </div>
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-arrow-right"
              trailing
              label="全部 Incident"
              to="/incidents"
            />
          </header>

          <div
            v-if="active.length"
            class="overview-entity-list"
          >
            <UButton
              v-for="incident in active"
              :key="incident.id"
              color="neutral"
              variant="ghost"
              block
              class="overview-entity-row incident-row"
              :class="`is-${incident.severity}`"
              :to="{ name: 'incidents', query: { selected: incident.id } }"
            >
              <span class="entity-copy">
                <strong>{{ incidentTitle(incident) }}</strong>
                <small>{{ incident.operational_context.service || incident.operational_context.resource.name || "未关联资源" }}</small>
              </span>
              <UBadge
                :color="severityColors[incident.severity]"
                variant="subtle"
                :label="severityLabel(incident.severity)"
              />
              <span class="entity-status">{{ incidentStatusLabel(incident.status) }}</span>
            </UButton>
          </div>
          <div
            v-else
            class="overview-healthy-state"
            role="status"
          >
            <UIcon
              name="i-lucide-circle-check"
              aria-hidden="true"
            />
            <div><strong>当前没有活跃 Incident</strong><span>继续显示近期已解决记录与真实 Alert。</span></div>
          </div>

          <div
            v-if="!active.length && resolved.length"
            class="recent-resolved"
          >
            <span>近期已解决</span>
            <UButton
              v-for="incident in resolved"
              :key="incident.id"
              color="neutral"
              variant="ghost"
              :label="incidentTitle(incident)"
              :to="{ name: 'incidents', query: { selected: incident.id } }"
            />
          </div>

          <div class="unlinked-alerts">
            <header><span>未关联 Alert</span><strong>{{ alerts.length }}</strong></header>
            <div
              v-if="alerts.length"
              class="alert-list"
            >
              <UButton
                v-for="alert in alerts"
                :key="alert.id"
                color="neutral"
                variant="ghost"
                block
                class="alert-row"
                :to="{ name: 'alert-detail', params: { alertId: alert.id } }"
              >
                <span><strong>{{ alert.summary }}</strong><small>{{ alert.service_name || alert.target_name || alert.namespace }}</small></span>
                <UBadge
                  :color="severityColors[alert.severity]"
                  variant="outline"
                  :label="alertSeverityLabels[alert.severity]"
                />
              </UButton>
            </div>
            <p v-else>
              当前没有未关联的 firing Alert。
            </p>
          </div>
        </section>

        <section
          class="overview-panel agent-panel"
          aria-labelledby="overview-agent-title"
        >
          <header>
            <div>
              <span>Agent 调查到了什么</span>
              <h2 id="overview-agent-title">
                最新调查摘要
              </h2>
            </div>
            <UButton
              v-if="latestRun"
              color="neutral"
              variant="ghost"
              icon="i-lucide-arrow-right"
              trailing
              label="进入 Agent"
              :to="{ name: 'agent', query: { investigation: latestRun.id } }"
            />
          </header>

          <template v-if="latestRun">
            <div class="agent-run-identity">
              <UBadge
                :color="latestRun.status === 'completed' ? 'success' : latestRun.status === 'failed' ? 'error' : 'info'"
                variant="subtle"
                :label="latestRun.status"
              />
              <span class="mono-text">{{ latestRun.id }}</span>
              <time :datetime="latestRun.updated_at">{{ formatUTC(latestRun.updated_at) }}</time>
            </div>
            <p class="agent-conclusion">
              {{ agentSummary(latestRun) }}
            </p>
            <dl class="agent-boundary">
              <div><dt>Evidence</dt><dd>{{ latestRun.evidence_count }}</dd></div>
              <div><dt>置信边界</dt><dd>{{ latestRun.uncertainty || "未报告" }}</dd></div>
              <div><dt>待处理项</dt><dd>{{ pendingAgentItems(latestRun) }}</dd></div>
            </dl>
            <section class="evidence-summary">
              <h3>关键 Evidence</h3>
              <ol v-if="latestRun.evidence_citations.length">
                <li
                  v-for="citation in latestRun.evidence_citations.slice(0, 3)"
                  :key="citation.id"
                >
                  <span>{{ citation.summary }}</span>
                  <small class="mono-text">{{ citation.source }} · {{ citation.content_hash }}</small>
                </li>
              </ol>
              <p v-else>
                当前 Investigation 尚未产生 Evidence citation。
              </p>
            </section>
            <UAlert
              v-if="pendingAgentItems(latestRun) || awaitingApproval"
              color="warning"
              variant="soft"
              icon="i-lucide-file-key-2"
              title="存在待处理的授权或审批事实"
              :description="`${pendingAgentItems(latestRun)} 个 Agent 建议项，${awaitingApproval} 个 Incident 等待审批。请进入对应 Incident 完成操作。`"
            />
          </template>
          <WorkspaceState
            v-else
            kind="empty"
            title="当前没有 Agent Investigation"
            description="可以从当前 Scope 发起只读调查；首页不会执行审批或变更。"
          >
            <template #actions>
              <UButton
                color="primary"
                variant="soft"
                icon="i-lucide-search"
                label="携带当前 Scope"
                :disabled="!overview"
                @click="beginScopeInvestigation"
              />
            </template>
          </WorkspaceState>
        </section>
      </section>

      <section class="overview-grid overview-grid-secondary">
        <section
          class="overview-panel atlas-preview-panel"
          aria-labelledby="overview-atlas-title"
        >
          <header>
            <div>
              <span>真实拓扑上下文</span>
              <h2 id="overview-atlas-title">
                Operations Atlas
              </h2>
            </div>
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-maximize-2"
              label="完整 Atlas"
              to="/atlas"
            />
          </header>
          <div class="atlas-preview">
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
            <WorkspaceState
              v-else
              kind="loading"
              title="正在读取 Atlas 预览"
            />
            <div
              v-if="atlas"
              class="atlas-preview-meta"
            >
              <span>{{ atlas.nodes.length }} nodes · {{ unhealthyNodes.length }} 异常/未知</span>
              <span class="mono-text">{{ atlas.source.identity }}</span>
            </div>
          </div>
        </section>

        <section
          class="overview-panel delivery-panel"
          aria-labelledby="overview-delivery-title"
        >
          <header>
            <div>
              <span>是否恢复并得到验证</span>
              <h2 id="overview-delivery-title">
                最近 Delivery / Verification
              </h2>
            </div>
            <UButton
              color="neutral"
              variant="ghost"
              icon="i-lucide-arrow-right"
              trailing
              label="查看 DevOps"
              to="/devops"
            />
          </header>
          <div
            v-if="deliveries.length"
            class="delivery-list"
          >
            <UButton
              v-for="delivery in deliveries"
              :key="delivery.id"
              color="neutral"
              variant="ghost"
              block
              class="delivery-row"
              :to="{ name: 'incident-detail', params: { incidentId: delivery.incident_id }, hash: deliveryTargetHash(delivery) }"
            >
              <span class="delivery-identity">
                <strong>{{ delivery.repository || delivery.argo_application || delivery.id }}</strong>
                <small class="mono-text">{{ delivery.commit_sha || delivery.target_revision || "无 revision" }}</small>
              </span>
              <UBadge
                :color="deliveryStatusColor(delivery)"
                variant="subtle"
                :label="deliveryStatus(delivery)"
              />
              <time :datetime="delivery.last_observed_at">{{ formatUTC(delivery.last_observed_at) }}</time>
            </UButton>
          </div>
          <WorkspaceState
            v-else
            kind="empty"
            title="近期没有 Delivery 投影"
            description="不会把 dispatched 或 accepted 推断为恢复成功。"
          />
        </section>
      </section>
    </template>
  </article>
</template>

<style scoped>
.overview-view {
  display: grid;
  width: min(100%, var(--co-content-max-width));
  min-width: 0;
  margin: 0 auto;
  gap: var(--co-space-4);
}

.overview-status-strip {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.overview-status-strip > div {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--co-space-2);
  min-height: 48px;
  padding: 0 var(--co-space-3);
  border-right: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  font-size: 11px;
}
.overview-status-strip > div:last-child { border-right: 0; }
.overview-status-strip svg { color: var(--co-status-neutral-fg); }
.overview-status-strip strong { color: var(--co-text-primary); font-size: 15px; font-variant-numeric: tabular-nums; }
.overview-status-strip .is-critical svg { color: var(--co-status-critical-fg); }
.overview-status-strip .is-warning svg { color: var(--co-status-warning-fg); }
.overview-status-strip .is-info svg { color: var(--co-status-info-fg); }
.overview-status-strip .is-success svg { color: var(--co-status-success-fg); }

.overview-grid {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.overview-panel {
  min-width: 0;
  padding: var(--co-space-4);
  border-right: 1px solid var(--co-border-default);
}
.overview-panel:last-child { border-right: 0; }
.overview-panel > header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-3);
  margin-bottom: var(--co-space-3);
}
.overview-panel > header > div { min-width: 0; }
.overview-panel > header span { color: var(--co-text-muted); font-size: 10px; }
.overview-panel h2 { margin: 2px 0 0; font-size: 15px; line-height: 1.35; }
.overview-entity-list,
.alert-list,
.delivery-list { display: grid; min-width: 0; gap: var(--co-space-1); }
.overview-entity-row,
.alert-row,
.delivery-row {
  display: grid;
  width: 100%;
  min-width: 0;
  min-height: 48px;
  justify-content: stretch;
  text-align: left;
}
.overview-entity-row {
  grid-template-columns: minmax(0, 1fr) auto auto;
  border-left: var(--co-severity-marker-width) solid var(--co-status-neutral-fg);
}
.overview-entity-row.is-critical { border-left-color: var(--co-status-critical-fg); }
.overview-entity-row.is-warning { border-left-color: var(--co-status-warning-fg); }
.overview-entity-row.is-info { border-left-color: var(--co-status-info-fg); }
.entity-copy,
.alert-row > span,
.delivery-identity { display: grid; min-width: 0; }
.entity-copy strong,
.alert-row strong,
.delivery-identity strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }
.entity-copy small,
.alert-row small,
.delivery-identity small { overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.entity-status { color: var(--co-text-secondary); font-size: 10px; }
.overview-healthy-state {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: var(--co-space-3);
  min-height: 76px;
  color: var(--co-status-success-fg);
}
.overview-healthy-state div { display: grid; }
.overview-healthy-state span { color: var(--co-text-muted); font-size: 11px; }
.recent-resolved { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-1); }
.recent-resolved > span { margin-right: var(--co-space-1); color: var(--co-text-muted); font-size: 10px; }
.unlinked-alerts { margin-top: var(--co-space-3); padding-top: var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.unlinked-alerts > header { display: flex; justify-content: space-between; color: var(--co-text-muted); font-size: 10px; }
.unlinked-alerts > p { margin: var(--co-space-2) 0 0; color: var(--co-text-muted); font-size: 11px; }
.alert-row { grid-template-columns: minmax(0, 1fr) auto; }
.agent-run-identity { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 10px; }
.agent-conclusion {
  margin: var(--co-space-3) 0;
  display: -webkit-box;
  overflow: hidden;
  color: var(--co-text-primary);
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  line-height: 1.55;
  font-size: 13px;
}
.agent-boundary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); margin: 0; border-block: 1px solid var(--co-border-default); }
.agent-boundary div { min-width: 0; padding: var(--co-space-2); border-right: 1px solid var(--co-border-default); }
.agent-boundary div:last-child { border-right: 0; }
.agent-boundary dt { color: var(--co-text-muted); font-size: 10px; }
.agent-boundary dd { margin: 2px 0 0; overflow-wrap: anywhere; font-size: 12px; font-weight: 700; }
.evidence-summary { margin: var(--co-space-3) 0; }
.evidence-summary h3 { margin: 0 0 var(--co-space-2); font-size: 12px; }
.evidence-summary ol { display: grid; gap: var(--co-space-1); margin: 0; padding-left: var(--co-space-5); }
.evidence-summary li { padding-left: var(--co-space-1); font-size: 11px; }
.evidence-summary li span,
.evidence-summary li small { display: block; overflow-wrap: anywhere; }
.evidence-summary li small,
.evidence-summary > p { color: var(--co-text-muted); font-size: 10px; }
.atlas-preview { position: relative; min-height: 228px; overflow: hidden; border: 1px solid var(--co-border-default); background: var(--co-bg-canvas); }
.atlas-preview-meta {
  position: absolute;
  right: var(--co-space-2);
  bottom: var(--co-space-2);
  left: var(--co-space-2);
  display: flex;
  min-width: 0;
  justify-content: space-between;
  gap: var(--co-space-2);
  padding: var(--co-space-2);
  border: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  background: color-mix(in srgb, var(--co-bg-surface) 88%, transparent);
  font-size: 10px;
  pointer-events: none;
}
.atlas-preview-meta span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.delivery-row { grid-template-columns: minmax(0, 1fr) auto; }
.delivery-row time { grid-column: 1 / -1; color: var(--co-text-muted); font-size: 10px; font-variant-numeric: tabular-nums; }

@media (max-width: 1100px) {
  .overview-status-strip { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .overview-status-strip > div:nth-child(3) { border-right: 0; }
  .overview-status-strip > div:nth-child(n + 4) { border-top: 1px solid var(--co-border-default); }
}
</style>
