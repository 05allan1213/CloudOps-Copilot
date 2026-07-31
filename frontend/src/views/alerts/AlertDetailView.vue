<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from "vue";
import type { LocationQueryRaw, RouteLocationRaw } from "vue-router";
import { useRoute, useRouter } from "vue-router";

import {
  acknowledgeAlert,
  alertCommandKey,
  alertContextRouteLinks,
  alertListRouteQuery,
  attachAlertToIncident,
  canonicalAlertResourceQuery,
  createAlertSilence,
  createIncidentFromAlert,
  expireAlertSilence,
  getAlert,
  isAlertPublicID,
  parseAlertListRouteQuery,
  startAlertInvestigation,
  type AlertCommandResult,
  type AlertDetail,
  type AlertIncidentLink,
  type AlertRouteQuery,
} from "../../api/alerts";
import AlertBadges from "../../components/alerts/AlertBadges.vue";
import ApiErrorNotice from "../../components/workspace/ApiErrorNotice.vue";
import WorkspaceHeader from "../../components/workspace/WorkspaceHeader.vue";
import WorkspaceState from "../../components/workspace/WorkspaceState.vue";

type DetailCommand =
  | "acknowledge"
  | "silence"
  | "expire-silence"
  | "create-incident"
  | "attach-incident"
  | "investigation";

type CommandColor = "primary" | "warning" | "error" | "info" | "neutral";

interface DetailCommandDefinition {
  title: string;
  description: string;
  confirmLabel: string;
  icon: string;
  color: CommandColor;
  needsReason: boolean;
}

interface AlertCommandFeedback {
  label: string;
  receivedAt: string;
  result: AlertCommandResult<unknown>;
}

const commandDefinitions: Record<DetailCommand, DetailCommandDefinition> = {
  acknowledge: {
    title: "确认已知悉此 Alert",
    description: "记录当前 recurrence 已被 Owner 看到；不会创建 Silence、解决 Alert 或关闭 Incident。",
    confirmLabel: "记录 Acknowledge",
    icon: "i-lucide-circle-check",
    color: "primary",
    needsReason: true,
  },
  silence: {
    title: "创建 Provider-backed Silence",
    description: "在选定时间内抑制匹配通知；Alert firing、acknowledgement 与 Incident 状态保持独立。",
    confirmLabel: "创建 Silence",
    icon: "i-lucide-volume-x",
    color: "warning",
    needsReason: true,
  },
  "expire-silence": {
    title: "提前结束当前 Silence",
    description: "Alertmanager 通知将恢复；Alert firing 状态不会因此改变。",
    confirmLabel: "结束 Silence",
    icon: "i-lucide-volume-2",
    color: "warning",
    needsReason: false,
  },
  "create-incident": {
    title: "创建或复用 active Incident",
    description: "按 correlation identity 显式升级到 Incident 生命周期；后续决策、交付与验证仅在 Incident 中执行。",
    confirmLabel: "创建 Incident",
    icon: "i-lucide-siren",
    color: "warning",
    needsReason: false,
  },
  "attach-incident": {
    title: "关联现有 Incident",
    description: "把此 Alert 加入指定 Incident；不会复制 Approval、Delivery 或 Verification 操作面。",
    confirmLabel: "关联 Incident",
    icon: "i-lucide-link-2",
    color: "warning",
    needsReason: false,
  },
  investigation: {
    title: "从 Alert 启动 Investigation",
    description: "仅在存在 active Incident 时提交 Agent 调查，并保留 Alert 与 Incident 上下文。",
    confirmLabel: "启动 Investigation",
    icon: "i-lucide-bot",
    color: "primary",
    needsReason: true,
  },
};

const silenceDurationItems = [
  { label: "5 分钟", value: 300 },
  { label: "15 分钟", value: 900 },
  { label: "30 分钟", value: 1800 },
  { label: "1 小时", value: 3600 },
  { label: "4 小时", value: 14400 },
  { label: "24 小时", value: 86400 },
];

const route = useRoute();
const router = useRouter();
const detail = ref<AlertDetail | null>(null);
const loading = ref(true);
const refreshing = ref(false);
const pageError = ref<unknown>(null);
const command = ref<DetailCommand | null>(null);
const commandReason = ref("");
const commandDuration = ref(1800);
const commandIncidentID = ref("");
const commandExpectedVersion = ref(0);
const commandIdempotencyKey = ref("");
const commandPending = ref(false);
const commandError = ref<unknown>(null);
const commandFeedback = ref<AlertCommandFeedback | null>(null);
let controller: AbortController | null = null;

const alertID = computed(() => String(route.params.alertId ?? ""));
const alert = computed(() => detail.value?.alert ?? null);
const activeIncident = computed(() => alert.value?.incident_links.find(isActiveIncident) ?? null);
const canAcknowledge = computed(() => alert.value?.status === "firing" && !alert.value.acknowledgement);
const canSilence = computed(() => alert.value?.status === "firing"
  && !["pending", "active"].includes(alert.value.silence?.status ?? ""));
const canExpireSilence = computed(() => alert.value?.silence?.status === "active");
const canCreateIncident = computed(() => alert.value?.status === "firing");
const canInvestigate = computed(() => Boolean(activeIncident.value));
const contextLinks = computed(() => alert.value ? alertContextRouteLinks(alert.value) : []);
const commandDefinition = computed(() => commandDefinitions[command.value ?? "acknowledge"]);
const commandReady = computed(() => {
  if (!command.value || !alert.value || commandExpectedVersion.value <= 0 || !commandIdempotencyKey.value) return false;
  if (commandDefinition.value.needsReason && !commandReason.value.trim()) return false;
  if (command.value === "attach-incident" && !isAlertPublicID(commandIncidentID.value.trim())) return false;
  if (command.value === "expire-silence" && !alert.value.silence?.id) return false;
  return true;
});
const listLocation = computed<RouteLocationRaw>(() => {
  const state = parseAlertListRouteQuery(route.query as unknown as AlertRouteQuery);
  const query = alertListRouteQuery({ ...state, selected: "" }, route.query as unknown as AlertRouteQuery);
  return { name: "alerts", query: query as LocationQueryRaw };
});
const agentLocation = computed<RouteLocationRaw>(() => {
  if (!alert.value) return { name: "agent" };
  const query: LocationQueryRaw = {
    alert: alert.value.id,
    cluster: alert.value.cluster,
    namespace: alert.value.namespace,
    resource: alert.value.target_name,
    from: alert.value.starts_at,
    to: alert.value.resolved_at || new Date().toISOString(),
  };
  if (activeIncident.value) query.incident = activeIncident.value.incident_id;
  return { name: "agent", query };
});

function isActiveIncident(link: AlertIncidentLink): boolean {
  return ["detected", "investigating", "awaiting_approval", "delivering", "verifying"].includes(link.incident_status);
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "medium",
  }).format(date);
}

function shortID(value: string): string {
  return value.length > 18 ? `${value.slice(0, 10)}...${value.slice(-6)}` : value;
}

function provenanceLabel(value: AlertIncidentLink["provenance"] | "signal_normalization"): string {
  return ({
    owner_created: "Owner 创建",
    owner_attached: "Owner 关联",
    escalation_policy: "Escalation Policy",
    legacy_automatic_ingress: "Legacy automatic ingress",
    signal_normalization: "Signal normalization",
  } as Record<string, string>)[value] ?? value;
}

async function canonicalizeRouteQuery() {
  const current = route.query as unknown as AlertRouteQuery;
  const canonical = canonicalAlertResourceQuery(current);
  const location = {
    path: route.path,
    query: canonical as LocationQueryRaw,
    hash: route.hash,
  };
  if (router.resolve(location).fullPath !== route.fullPath) await router.replace(location);
}

async function load(preserve: boolean) {
  controller?.abort();
  const requestController = new AbortController();
  controller = requestController;
  if (preserve && detail.value) refreshing.value = true;
  else loading.value = true;
  pageError.value = null;
  if (!isAlertPublicID(alertID.value)) {
    pageError.value = new Error("Alert ID 不是可读取的 public UUID。");
    loading.value = false;
    refreshing.value = false;
    return;
  }
  try {
    const next = await getAlert(alertID.value, requestController.signal);
    if (controller !== requestController) return;
    detail.value = next;
  } catch (error) {
    if (requestController.signal.aborted || controller !== requestController) return;
    pageError.value = error;
  } finally {
    if (controller === requestController) {
      loading.value = false;
      refreshing.value = false;
    }
  }
}

function commandTargetID(nextCommand: DetailCommand): string {
  if (nextCommand === "expire-silence") return alert.value?.silence?.id ?? alertID.value;
  return alertID.value;
}

function openCommand(nextCommand: DetailCommand) {
  if (!alert.value || commandPending.value) return;
  command.value = nextCommand;
  commandReason.value = nextCommand === "acknowledge"
    ? "Owner 已看到并开始 triage"
    : nextCommand === "silence"
      ? "Owner triage 期间抑制重复 Provider 通知"
      : nextCommand === "investigation"
        ? "调查当前 firing condition"
        : "";
  commandIncidentID.value = "";
  commandExpectedVersion.value = alert.value.version;
  commandIdempotencyKey.value = alertCommandKey(nextCommand, commandTargetID(nextCommand));
  commandError.value = null;
}

function closeCommand() {
  if (commandPending.value) return;
  command.value = null;
  commandError.value = null;
}

function updateCommandOpen(value: boolean) {
  if (!value) closeCommand();
}

async function submitCommand() {
  const current = alert.value;
  const nextCommand = command.value;
  if (!current || !nextCommand || !commandReady.value || commandPending.value) return;
  commandPending.value = true;
  commandError.value = null;
  try {
    const options = { idempotencyKey: commandIdempotencyKey.value };
    const result = nextCommand === "acknowledge"
      ? await acknowledgeAlert(current.id, commandExpectedVersion.value, commandReason.value.trim(), options)
      : nextCommand === "silence"
        ? await createAlertSilence(current.id, commandExpectedVersion.value, commandDuration.value, commandReason.value.trim(), options)
        : nextCommand === "expire-silence"
          ? await expireAlertSilence(current.silence!.id, commandExpectedVersion.value, options)
          : nextCommand === "create-incident"
            ? await createIncidentFromAlert(current.id, commandExpectedVersion.value, options)
            : nextCommand === "attach-incident"
              ? await attachAlertToIncident(current.id, commandIncidentID.value.trim(), commandExpectedVersion.value, options)
              : await startAlertInvestigation(current.id, commandExpectedVersion.value, commandReason.value.trim(), options);
    commandFeedback.value = {
      label: `${commandDefinition.value.confirmLabel} 已返回`,
      receivedAt: new Date().toISOString(),
      result,
    };
    command.value = null;
    await load(true);
  } catch (error) {
    commandError.value = error;
  } finally {
    commandPending.value = false;
  }
}

function resetForAlert() {
  detail.value = null;
  pageError.value = null;
  command.value = null;
  commandError.value = null;
  commandFeedback.value = null;
  void load(false);
}

watch(
  () => [route.query.workload, route.query.resource],
  () => void canonicalizeRouteQuery(),
  { immediate: true },
);
watch(alertID, resetForAlert, { immediate: true });
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <article
    class="alert-detail-view"
    data-testid="alert-detail-route"
  >
    <WorkspaceHeader
      :title="alert?.summary ?? '告警详情'"
      eyebrow="CloudOps Alert"
      :description="alert ? `${alert.cluster} / ${alert.namespace} / ${alert.target_kind} ${alert.target_name}` : '读取可分享的 Alert lifecycle 深链接。'"
    >
      <template
        v-if="alert"
        #context
      >
        <AlertBadges
          :status="alert.status"
          :severity="alert.severity"
        />
        <UBadge
          color="neutral"
          variant="outline"
          :label="`v${alert.version}`"
        />
        <UBadge
          color="neutral"
          variant="soft"
          icon="i-lucide-radio-tower"
          :label="alert.source"
        />
      </template>
      <template #actions>
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-arrow-left"
          label="返回告警"
          :to="listLocation"
        />
        <UTooltip text="刷新当前 Alert 投影">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            square
            aria-label="刷新 Alert"
            :loading="refreshing"
            :disabled="loading || refreshing || Boolean(command)"
            @click="load(true)"
          />
        </UTooltip>
      </template>
    </WorkspaceHeader>

    <WorkspaceState
      v-if="loading && !detail"
      kind="loading"
      title="正在读取 Alert lifecycle"
      description="深链接与 canonical resource 上下文保持稳定。"
    />
    <ApiErrorNotice
      v-else-if="pageError && !detail"
      :error="pageError"
      fallback="Alert 详情读取失败。"
      title="Alert 不可用"
      retryable
      @retry="load(false)"
    />

    <template v-if="detail && alert">
      <ApiErrorNotice
        v-if="pageError"
        :error="pageError"
        fallback="刷新失败；当前 Alert 投影保持可读。"
        title="刷新失败"
        retryable
        @retry="load(true)"
      />

      <UAlert
        v-if="commandFeedback"
        color="success"
        variant="soft"
        icon="i-lucide-circle-check"
        :title="commandFeedback.label"
      >
        <template #description>
          <dl class="alert-command-identity">
            <div>
              <dt>HTTP</dt>
              <dd>{{ commandFeedback.result.httpStatus }}</dd>
            </div>
            <div>
              <dt>Expected version</dt>
              <dd>{{ commandFeedback.result.expectedVersion }}</dd>
            </div>
            <div>
              <dt>Idempotent replay</dt>
              <dd>{{ commandFeedback.result.idempotentReplay ? "YES" : "NO" }}</dd>
            </div>
            <div>
              <dt>Request ID</dt>
              <dd>{{ commandFeedback.result.requestID || "未返回" }}</dd>
            </div>
            <div>
              <dt>Trace ID</dt>
              <dd>{{ commandFeedback.result.traceID || "未返回" }}</dd>
            </div>
            <div>
              <dt>Idempotency Key</dt>
              <dd>{{ commandFeedback.result.idempotencyKey }}</dd>
            </div>
            <div>
              <dt>客户端收到 UTC</dt>
              <dd>{{ commandFeedback.receivedAt }}</dd>
            </div>
          </dl>
        </template>
      </UAlert>

      <section
        class="alert-command-surface"
        aria-labelledby="alert-owner-commands"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Owner Commands</span>
            <h2 id="alert-owner-commands">
              Alert 本地处置
            </h2>
          </div>
          <p>Incident 生命周期动作保留在 Incident Workspace。</p>
        </header>
        <div class="alert-command-grid">
          <UButton
            color="primary"
            variant="soft"
            icon="i-lucide-circle-check"
            label="Acknowledge"
            :disabled="!canAcknowledge || commandPending"
            @click="openCommand('acknowledge')"
          />
          <UButton
            color="warning"
            variant="soft"
            icon="i-lucide-volume-x"
            label="创建 Silence"
            :disabled="!canSilence || commandPending"
            @click="openCommand('silence')"
          />
          <UButton
            color="warning"
            variant="outline"
            icon="i-lucide-volume-2"
            label="结束 Silence"
            :disabled="!canExpireSilence || commandPending"
            @click="openCommand('expire-silence')"
          />
          <UButton
            color="warning"
            variant="soft"
            icon="i-lucide-siren"
            label="创建 Incident"
            :disabled="!canCreateIncident || commandPending"
            @click="openCommand('create-incident')"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-link-2"
            label="关联 Incident"
            :disabled="commandPending"
            @click="openCommand('attach-incident')"
          />
          <UButton
            color="primary"
            variant="outline"
            icon="i-lucide-bot"
            label="启动 Investigation"
            :disabled="!canInvestigate || commandPending"
            @click="openCommand('investigation')"
          />
        </div>
      </section>

      <dl
        class="alert-facet-strip"
        aria-label="Alert lifecycle facets"
      >
        <div>
          <dt>Acknowledgement</dt>
          <dd>{{ alert.acknowledgement ? `recurrence ${alert.acknowledgement.recurrence_no}` : "未 acknowledge" }}</dd>
          <small>{{ alert.acknowledgement?.reason || "独立于 silence 与 resolution" }}</small>
        </div>
        <div>
          <dt>Silence</dt>
          <dd>{{ alert.silence?.status || "无" }}</dd>
          <small>{{ alert.silence ? `${formatTime(alert.silence.starts_at)} - ${formatTime(alert.silence.ends_at)}` : "Provider notification 未抑制" }}</small>
        </div>
        <div>
          <dt>Incident links</dt>
          <dd>{{ alert.incident_links.length }}</dd>
          <small>{{ activeIncident ? `active ${shortID(activeIncident.incident_id)}` : "无 active Incident" }}</small>
        </div>
        <div>
          <dt>Investigation</dt>
          <dd>{{ alert.investigations.length }}</dd>
          <small>{{ alert.investigations[0]?.status || "尚未启动" }}</small>
        </div>
      </dl>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-identity"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Identity</span>
            <h2 id="alert-identity">
              Alert aggregate
            </h2>
          </div>
        </header>
        <dl class="alert-facts-grid">
          <div>
            <dt>Alert ID</dt>
            <dd>{{ alert.id }}</dd>
          </div>
          <div>
            <dt>Source</dt>
            <dd>{{ alert.source }}</dd>
          </div>
          <div>
            <dt>Fingerprint</dt>
            <dd>{{ alert.fingerprint }}</dd>
          </div>
          <div>
            <dt>Correlation key</dt>
            <dd>{{ alert.correlation_key }}</dd>
          </div>
          <div>
            <dt>首次出现</dt>
            <dd>{{ formatTime(alert.first_seen_at) }}</dd>
          </div>
          <div>
            <dt>最近出现</dt>
            <dd>{{ formatTime(alert.last_seen_at) }}</dd>
          </div>
          <div>
            <dt>Resolved</dt>
            <dd>{{ formatTime(alert.resolved_at) }}</dd>
          </div>
          <div>
            <dt>Provenance</dt>
            <dd>{{ alert.migrated_legacy ? "legacy_automatic_ingress" : "native Alert" }}</dd>
          </div>
        </dl>
      </section>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-context-links"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Context</span>
            <h2 id="alert-context-links">
              继续调查
            </h2>
          </div>
        </header>
        <nav
          class="alert-link-grid"
          aria-label="Alert context links"
        >
          <UButton
            v-for="link in contextLinks"
            :key="link.path"
            color="neutral"
            variant="outline"
            trailing-icon="i-lucide-arrow-up-right"
            :label="link.label"
            :to="{ path: link.path, query: link.query }"
          />
          <UButton
            color="primary"
            variant="outline"
            icon="i-lucide-bot"
            label="Agent Workspace"
            :to="agentLocation"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-settings-2"
            label="Alertmanager 配置"
            to="/settings#providers"
          />
        </nav>
      </section>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-relations"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Relationships</span>
            <h2 id="alert-relations">
              Incident 与 Investigation
            </h2>
          </div>
        </header>
        <WorkspaceState
          v-if="!alert.incident_links.length && !alert.investigations.length"
          kind="empty"
          title="尚无 Incident 或 Investigation"
          description="Alert 仍可独立 acknowledge 或 silence。"
        />
        <div
          v-else
          class="alert-relations"
        >
          <div
            v-if="alert.incident_links.length"
            class="alert-relation-table"
            role="region"
            aria-label="Alert Incident relationships"
            tabindex="0"
          >
            <table>
              <thead>
                <tr>
                  <th>Incident</th>
                  <th>状态</th>
                  <th>Cycle</th>
                  <th>Provenance</th>
                  <th>Configuration Revision / Policy</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="link in alert.incident_links"
                  :key="link.id"
                >
                  <td>
                    <UButton
                      color="neutral"
                      variant="link"
                      trailing-icon="i-lucide-arrow-up-right"
                      :label="shortID(link.incident_id)"
                      :to="{ name: 'incident-detail', params: { incidentId: link.incident_id } }"
                    />
                  </td>
                  <td>{{ link.incident_status }}</td>
                  <td>{{ link.incident_cycle }}</td>
                  <td>{{ provenanceLabel(link.provenance) }}</td>
                  <td>
                    <code>{{ link.configuration_revision_id || "Owner command" }}</code>
                    <code v-if="link.escalation_policy_id">{{ link.escalation_policy_id }}</code>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div
            v-if="alert.investigations.length"
            class="alert-investigation-links"
          >
            <UButton
              v-for="run in alert.investigations"
              :key="run.id"
              color="neutral"
              variant="outline"
              icon="i-lucide-bot"
              :label="`Investigation ${shortID(run.id)} · ${run.status}`"
              :to="{ name: 'agent', query: { investigation: run.id, alert: alert.id, incident: run.incident_id, resource: alert.target_name } }"
            />
          </div>
        </div>
      </section>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-provider-facts"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Provider</span>
            <h2 id="alert-provider-facts">
              Alertmanager facts
            </h2>
          </div>
        </header>
        <dl class="alert-facts-grid">
          <div>
            <dt>Provider source</dt>
            <dd>{{ alert.source }}</dd>
          </div>
          <div>
            <dt>Silence ID</dt>
            <dd>{{ alert.silence?.id || "无" }}</dd>
          </div>
          <div>
            <dt>Provider silence ID</dt>
            <dd>{{ alert.silence?.provider_silence_id || "未返回" }}</dd>
          </div>
          <div>
            <dt>Configuration revision</dt>
            <dd>{{ alert.silence?.configuration_revision_id || "未关联" }}</dd>
          </div>
        </dl>
      </section>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-signals"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Source Facts</span>
            <h2 id="alert-signals">
              Signals
            </h2>
          </div>
          <UBadge
            color="neutral"
            variant="outline"
            :label="String(detail.signals.length)"
          />
        </header>
        <WorkspaceState
          v-if="!detail.signals.length"
          kind="empty"
          title="没有 Signal 投影"
          description="当前 Alert 详情未返回来源 Signal。"
        />
        <ol
          v-else
          class="alert-signal-list"
        >
          <li
            v-for="signal in detail.signals"
            :key="signal.id"
          >
            <header>
              <AlertBadges
                :status="signal.status"
                :severity="signal.severity"
              />
              <time :datetime="signal.occurred_at">{{ formatTime(signal.occurred_at) }}</time>
            </header>
            <strong>{{ signal.summary }}</strong>
            <dl>
              <div>
                <dt>Signal ID</dt>
                <dd>{{ signal.id }}</dd>
              </div>
              <div>
                <dt>Source event</dt>
                <dd>{{ signal.source_event_id }}</dd>
              </div>
              <div>
                <dt>Alert instance</dt>
                <dd>{{ signal.alert_instance_key }}</dd>
              </div>
              <div>
                <dt>Provenance</dt>
                <dd>{{ provenanceLabel(signal.provenance) }}</dd>
              </div>
            </dl>
            <details>
              <summary>Labels 与 annotations</summary>
              <pre>{{ JSON.stringify({ labels: signal.labels, annotations: signal.annotations }, null, 2) }}</pre>
            </details>
          </li>
        </ol>
      </section>

      <section
        class="alert-detail-section"
        aria-labelledby="alert-timeline"
      >
        <header>
          <div>
            <span class="alert-section-eyebrow">Audit</span>
            <h2 id="alert-timeline">
              Timeline
            </h2>
          </div>
          <UBadge
            color="neutral"
            variant="outline"
            :label="String(detail.events.length)"
          />
        </header>
        <WorkspaceState
          v-if="!detail.events.length"
          kind="empty"
          title="没有 Alert event"
          description="当前详情投影没有可展示的状态历史。"
        />
        <ol
          v-else
          class="alert-timeline"
        >
          <li
            v-for="event in detail.events"
            :key="event.id"
          >
            <span
              class="alert-timeline-mark"
              aria-hidden="true"
            />
            <div>
              <header>
                <strong>{{ event.type }}</strong>
                <time :datetime="event.occurred_at">{{ formatTime(event.occurred_at) }}</time>
              </header>
              <p>{{ event.summary }}</p>
              <small>{{ event.actor_type }} / {{ event.actor_id }}</small>
              <details>
                <summary>Metadata</summary>
                <pre>{{ JSON.stringify(event.metadata, null, 2) }}</pre>
              </details>
            </div>
          </li>
        </ol>
      </section>
    </template>

    <UModal
      :open="Boolean(command)"
      :title="commandDefinition.title"
      :description="commandDefinition.description"
      :dismissible="!commandPending"
      :close="!commandPending"
      @update:open="updateCommandOpen"
    >
      <template #body>
        <div class="alert-command-dialog">
          <UAlert
            :color="commandDefinition.color === 'primary' ? 'info' : commandDefinition.color"
            variant="soft"
            :icon="commandDefinition.icon"
            :title="commandDefinition.title"
            :description="commandDefinition.description"
          />
          <dl>
            <div>
              <dt>Target</dt>
              <dd>{{ alert?.target_kind }}/{{ alert?.namespace }}/{{ alert?.target_name }}</dd>
            </div>
            <div>
              <dt>Expected version</dt>
              <dd><code translate="no">{{ commandExpectedVersion }}</code></dd>
            </div>
            <div>
              <dt>Idempotency Key</dt>
              <dd><code translate="no">{{ commandIdempotencyKey }}</code></dd>
            </div>
            <div v-if="command === 'silence'">
              <dt>恢复</dt>
              <dd>到期自动解除，也可提前结束；不会改变 firing 状态。</dd>
            </div>
            <div v-if="command === 'expire-silence'">
              <dt>后果</dt>
              <dd>Provider 通知恢复；当前 Alert 与 Incident 状态保持不变。</dd>
            </div>
          </dl>
          <UForm
            :state="{ reason: commandReason, duration: commandDuration, incident: commandIncidentID }"
            @submit="submitCommand"
          >
            <UFormField
              v-if="command === 'silence'"
              label="Silence 时长"
              name="duration"
            >
              <USelect
                v-model="commandDuration"
                :items="silenceDurationItems"
                value-key="value"
              />
            </UFormField>
            <UFormField
              v-if="command === 'attach-incident'"
              label="Incident ID"
              name="incident"
              required
              :error="commandIncidentID.trim() && !isAlertPublicID(commandIncidentID.trim()) ? '请输入完整 public UUID。' : undefined"
            >
              <UInput
                v-model="commandIncidentID"
                icon="i-lucide-siren"
                maxlength="128"
                autocomplete="off"
                spellcheck="false"
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </UFormField>
            <UFormField
              v-if="commandDefinition.needsReason"
              label="审计原因"
              name="reason"
              required
            >
              <UTextarea
                v-model="commandReason"
                :rows="4"
                maxlength="1024"
                autoresize
              />
            </UFormField>
          </UForm>
          <ApiErrorNotice
            v-if="commandError"
            :error="commandError"
            fallback="Alert 命令失败；输入、Expected version 与 Idempotency Key 已保留。"
            title="命令未完成"
          />
        </div>
      </template>
      <template #footer>
        <div class="alert-command-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="取消"
            :disabled="commandPending"
            @click="closeCommand"
          />
          <UButton
            :color="commandDefinition.color"
            :icon="commandDefinition.icon"
            :label="commandError ? `使用同一 Idempotency Key 重试` : commandDefinition.confirmLabel"
            :loading="commandPending"
            :disabled="!commandReady"
            @click="submitCommand"
          />
        </div>
      </template>
    </UModal>
  </article>
</template>

<style scoped>
.alert-detail-view {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: var(--co-space-4);
}

.alert-command-surface,
.alert-detail-section {
  display: grid;
  min-width: 0;
  gap: var(--co-space-4);
  padding-block: var(--co-space-4);
  border-block: 1px solid var(--co-border-default);
}

.alert-command-surface > header,
.alert-detail-section > header,
.alert-signal-list > li > header,
.alert-timeline header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--co-space-3);
}

.alert-command-surface h2,
.alert-detail-section h2 {
  margin: 0;
  font-size: 16px;
}

.alert-command-surface > header p {
  margin: 0;
  color: var(--co-text-muted);
  font-size: 11px;
}

.alert-section-eyebrow {
  display: block;
  margin-bottom: var(--co-space-1);
  color: var(--co-text-muted);
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
}

.alert-command-grid,
.alert-link-grid,
.alert-investigation-links {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: var(--co-space-2);
}

.alert-facet-strip {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
  border-block: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.alert-facet-strip > div {
  display: grid;
  min-width: 0;
  gap: 2px;
  padding: var(--co-space-3) var(--co-space-4);
  border-right: 1px solid var(--co-border-default);
}

.alert-facet-strip > div:last-child { border-right: 0; }
.alert-facet-strip dt,
.alert-facet-strip small { color: var(--co-text-muted); font-size: 10px; }
.alert-facet-strip dd { margin: 0; font-weight: 700; }
.alert-facet-strip dd,
.alert-facet-strip small { overflow-wrap: anywhere; }

.alert-facts-grid,
.alert-command-identity,
.alert-command-dialog dl {
  display: grid;
  min-width: 0;
  margin: 0;
}

.alert-facts-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); }
.alert-facts-grid > div {
  min-width: 0;
  padding: var(--co-space-3);
  border-right: 1px solid var(--co-border-default);
  border-bottom: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}

.alert-facts-grid dt,
.alert-command-identity dt,
.alert-command-dialog dt { color: var(--co-text-muted); font-size: 10px; }
.alert-facts-grid dd,
.alert-command-identity dd,
.alert-command-dialog dd {
  min-width: 0;
  margin: var(--co-space-1) 0 0;
  overflow-wrap: anywhere;
}
.alert-facts-grid dd,
.alert-command-identity dd { font-family: var(--co-font-mono); font-size: 10px; }

.alert-command-identity { gap: var(--co-space-1); }
.alert-command-identity > div,
.alert-command-dialog dl > div {
  display: grid;
  min-width: 0;
  grid-template-columns: 132px minmax(0, 1fr);
  gap: var(--co-space-2);
  padding: var(--co-space-2) 0;
  border-bottom: 1px solid var(--co-border-default);
}

.alert-relations { display: grid; min-width: 0; gap: var(--co-space-3); }
.alert-relation-table { min-width: 0; overflow-x: auto; }
.alert-relation-table table {
  width: 100%;
  min-width: 900px;
  border-collapse: collapse;
  background: var(--co-bg-surface);
  font-size: 11px;
}
.alert-relation-table th,
.alert-relation-table td {
  padding: var(--co-space-2) var(--co-space-3);
  border-bottom: 1px solid var(--co-border-default);
  text-align: left;
  vertical-align: middle;
}
.alert-relation-table th { color: var(--co-text-muted); font-size: 10px; }
.alert-relation-table code { display: block; max-width: 320px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.alert-signal-list,
.alert-timeline {
  display: grid;
  margin: 0;
  padding: 0;
  list-style: none;
}

.alert-signal-list { gap: var(--co-space-2); }
.alert-signal-list > li {
  display: grid;
  min-width: 0;
  gap: var(--co-space-3);
  padding: var(--co-space-4);
  border: 1px solid var(--co-border-default);
  border-radius: var(--co-radius-panel);
  background: var(--co-bg-surface);
}
.alert-signal-list time,
.alert-timeline time,
.alert-timeline small { color: var(--co-text-muted); font-size: 10px; }
.alert-signal-list dl {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin: 0;
}
.alert-signal-list dl > div { min-width: 0; padding: var(--co-space-2); border-left: 1px solid var(--co-border-default); }
.alert-signal-list dt { color: var(--co-text-muted); font-size: 10px; }
.alert-signal-list dd { margin: 2px 0 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }

.alert-timeline > li {
  display: grid;
  grid-template-columns: 16px minmax(0, 1fr);
  gap: var(--co-space-2);
}
.alert-timeline > li > div {
  min-width: 0;
  padding: var(--co-space-3) var(--co-space-4);
  border-left: 1px solid var(--co-border-default);
  background: var(--co-bg-surface);
}
.alert-timeline-mark {
  width: 9px;
  height: 9px;
  margin-top: 16px;
  border: 2px solid var(--co-action-primary);
  border-radius: 50%;
  background: var(--co-bg-canvas);
}
.alert-timeline p { margin: var(--co-space-1) 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }

details summary { width: fit-content; color: var(--co-action-primary); cursor: pointer; font-size: 11px; font-weight: 700; }
pre {
  max-height: 320px;
  margin: var(--co-space-2) 0 0;
  padding: var(--co-space-3);
  overflow: auto;
  border: 1px solid var(--co-border-default);
  color: var(--co-text-secondary);
  background: var(--co-bg-subtle);
  font-size: 10px;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.alert-command-dialog { display: grid; min-width: 0; gap: var(--co-space-4); }
.alert-command-dialog dl { gap: var(--co-space-1); }
.alert-command-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }

@media (max-width: 1024px) {
  .alert-facet-strip,
  .alert-facts-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .alert-facet-strip > div:nth-child(2) { border-right: 0; }
  .alert-facet-strip > div:nth-child(-n+2) { border-bottom: 1px solid var(--co-border-default); }
  .alert-signal-list dl { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}

@media (max-width: 767px) {
  .alert-command-surface > header,
  .alert-detail-section > header,
  .alert-signal-list > li > header,
  .alert-timeline header { flex-direction: column; }
  .alert-facet-strip,
  .alert-facts-grid,
  .alert-signal-list dl { grid-template-columns: minmax(0, 1fr); }
  .alert-facet-strip > div { border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .alert-signal-list dl > div { border-top: 1px solid var(--co-border-default); border-left: 0; }
  .alert-command-actions { display: grid; grid-template-columns: minmax(0, 1fr); }
}
</style>
