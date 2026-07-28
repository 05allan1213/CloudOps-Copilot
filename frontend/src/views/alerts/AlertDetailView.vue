<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  ArrowLeft,
  Bot,
  Check,
  ExternalLink,
  Link2,
  RefreshCw,
  Siren,
  Volume2,
  VolumeX,
} from "lucide-vue-next";
import { useRoute } from "vue-router";

import {
  acknowledgeAlert,
  attachAlertToIncident,
  createAlertSilence,
  createIncidentFromAlert,
  expireAlertSilence,
  getAlert,
  startAlertInvestigation,
  type AlertDetail,
  type AlertIncidentLink,
} from "../../api/alerts";
import { isApiError } from "../../api/client";
import AlertBadges from "../../components/alerts/AlertBadges.vue";

const route = useRoute();
const detail = ref<AlertDetail | null>(null);
const loading = ref(true);
const refreshing = ref(false);
const commandPending = ref(false);
const error = ref("");
const commandError = ref("");
const silenceDuration = ref(1800);
const attachIncidentID = ref("");
let controller: AbortController | null = null;

const alertID = computed(() => String(route.params.alertId ?? ""));
const alert = computed(() => detail.value?.alert ?? null);
const activeIncident = computed(() => alert.value?.incident_links.find((link) => isActiveIncident(link)) ?? null);
const canAcknowledge = computed(() => alert.value?.status === "firing" && !alert.value.acknowledgement);
const canSilence = computed(() => alert.value?.status === "firing" && !["pending", "active"].includes(alert.value.silence?.status ?? ""));
const canExpireSilence = computed(() => alert.value?.silence?.status === "active");
const canInvestigate = computed(() => Boolean(activeIncident.value));
const contextLinks = computed(() => {
  if (!alert.value) return [];
  const shared = {
    cluster: alert.value.cluster,
    namespace: alert.value.namespace,
    from: alert.value.starts_at,
    to: alert.value.resolved_at || new Date().toISOString(),
  };
  return [
    { label: "基础设施", path: "/infrastructure", query: { ...shared, resource: `${alert.value.target_kind}/${alert.value.namespace}/${alert.value.target_name}` } },
    { label: "监控", path: "/monitoring", query: { ...shared, resource: alert.value.target_name } },
    { label: "日志", path: "/logs", query: { ...shared, workload: alert.value.target_name } },
    { label: "链路", path: "/traces", query: { ...shared, workload: alert.value.target_name } },
  ];
});

function isActiveIncident(link: AlertIncidentLink): boolean {
  return ["detected", "investigating", "awaiting_approval", "delivering", "verifying"].includes(link.incident_status);
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" }).format(date);
}

function shortID(value: string): string {
  return value.length > 18 ? `${value.slice(0, 12)}…` : value;
}

function describeError(reason: unknown, fallback: string): string {
  if (!isApiError(reason)) return fallback;
  const next = reason.nextSteps.length ? `；下一步：${reason.nextSteps.join("；")}` : "";
  return `${reason.code || "REQUEST_FAILED"}：${reason.message}${next}`;
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

async function load(preserve = false) {
  controller?.abort();
  const requestController = new AbortController();
  controller = requestController;
  if (preserve) refreshing.value = true;
  else loading.value = true;
  error.value = "";
  try {
    detail.value = await getAlert(alertID.value, requestController.signal);
  } catch (reason) {
    if (requestController.signal.aborted) return;
    error.value = describeError(reason, "Alert 详情读取失败。");
  } finally {
    if (controller === requestController) {
      loading.value = false;
      refreshing.value = false;
    }
  }
}

async function promptReason(title: string, initial = ""): Promise<string | null> {
  try {
    const result = await ElMessageBox.prompt("该原因会写入 Alert audit timeline。", title, {
      confirmButtonText: "提交",
      cancelButtonText: "取消",
      inputType: "textarea",
      inputValue: initial,
      inputPlaceholder: "输入 1 至 1024 个字符",
      inputValidator: (value) => {
        const text = value.trim();
        if (!text) return "请输入原因。";
        if (text.length > 1024) return "原因不得超过 1024 个字符。";
        return true;
      },
    });
    return result.value.trim();
  } catch {
    return null;
  }
}

async function runCommand(label: string, command: () => Promise<unknown>) {
  if (!alert.value || commandPending.value) return;
  commandPending.value = true;
  commandError.value = "";
  try {
    await command();
    await load(true);
    ElMessage.success(label);
  } catch (reason) {
    commandError.value = describeError(reason, `${label}失败。`);
  } finally {
    commandPending.value = false;
  }
}

async function acknowledge() {
  const reason = await promptReason("Acknowledge Alert", "Owner 已看到并开始 triage");
  if (reason === null || !alert.value) return;
  await runCommand("Alert 已 acknowledge", () => acknowledgeAlert(alertID.value, alert.value!.version, reason));
}

async function silence() {
  const reason = await promptReason("创建 bounded silence", "Owner triage 期间抑制重复 Provider 通知");
  if (reason === null || !alert.value) return;
  await runCommand("Alertmanager silence 已创建", () => createAlertSilence(alertID.value, alert.value!.version, silenceDuration.value, reason));
}

async function expireSilence() {
  if (!alert.value?.silence) return;
  await runCommand("Alertmanager silence 已结束", () => expireAlertSilence(alert.value!.silence!.id, alert.value!.version));
}

async function createIncident() {
  if (!alert.value) return;
  try {
    await ElMessageBox.confirm("创建或复用同一 correlation identity 的 active Incident？", "显式 Incident escalation", {
      confirmButtonText: "创建 Incident",
      cancelButtonText: "取消",
      type: "warning",
    });
  } catch {
    return;
  }
  await runCommand("Alert 已显式关联 Incident", () => createIncidentFromAlert(alertID.value, alert.value!.version));
}

async function attachIncident() {
  const incidentID = attachIncidentID.value.trim();
  if (!incidentID || !alert.value) return;
  await runCommand("Alert 已关联现有 Incident", () => attachAlertToIncident(alertID.value, incidentID, alert.value!.version));
  if (!commandError.value) attachIncidentID.value = "";
}

async function investigate() {
  const reason = await promptReason("从 Alert 启动 Investigation", "调查当前 firing condition");
  if (reason === null || !alert.value) return;
  await runCommand("Investigation 已提交", () => startAlertInvestigation(alertID.value, alert.value!.version, reason));
}

onMounted(() => void load(false));
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <article class="alert-detail-view">
    <RouterLink class="back-link" :to="{ name: 'alerts' }"><ArrowLeft :size="18" aria-hidden="true" />返回告警</RouterLink>

    <div v-if="loading && !detail" class="message-band" role="status" aria-live="polite">正在读取 Alert lifecycle…</div>
    <div v-else-if="error && !detail" class="message-band is-error" role="alert"><strong>Alert 不可用</strong><span>{{ error }}</span><button type="button" class="secondary-action" @click="load(false)"><RefreshCw :size="17" aria-hidden="true" />重试</button></div>

    <template v-if="detail && alert">
      <header class="detail-heading">
        <div>
          <p class="eyebrow">{{ alert.category }}</p>
          <h1>{{ alert.summary }}</h1>
          <p>{{ alert.cluster }} / {{ alert.namespace }} / {{ alert.target_kind }} {{ alert.target_name }}</p>
        </div>
        <div class="heading-state"><AlertBadges :status="alert.status" :severity="alert.severity" /><span class="mono-text">v{{ alert.version }}</span></div>
      </header>

      <div v-if="error" class="message-band is-warning" role="status"><strong>刷新失败</strong><span>{{ error }}</span></div>
      <div v-if="commandError" class="message-band is-error" role="alert"><strong>命令失败</strong><span>{{ commandError }}</span><button type="button" class="secondary-action" @click="load(true)"><RefreshCw :size="17" aria-hidden="true" />刷新状态</button></div>

      <section class="command-surface" aria-labelledby="owner-command-heading">
        <header><div><p class="eyebrow">Owner Commands</p><h2 id="owner-command-heading">Triage</h2></div><button type="button" class="icon-action" :disabled="refreshing" aria-label="刷新 Alert" title="刷新 Alert" @click="load(true)"><RefreshCw :size="18" aria-hidden="true" /></button></header>
        <div class="command-row">
          <button type="button" class="secondary-action" :disabled="!canAcknowledge || commandPending" @click="acknowledge"><Check :size="17" aria-hidden="true" />Acknowledge</button>
          <label class="duration-select"><span>Silence</span><select v-model.number="silenceDuration" name="silence_duration" autocomplete="off" :disabled="!canSilence || commandPending"><option :value="300">5 分钟</option><option :value="900">15 分钟</option><option :value="1800">30 分钟</option><option :value="3600">1 小时</option><option :value="14400">4 小时</option><option :value="86400">24 小时</option></select></label>
          <button type="button" class="secondary-action" :disabled="!canSilence || commandPending" @click="silence"><VolumeX :size="17" aria-hidden="true" />创建 Silence</button>
          <button type="button" class="secondary-action" :disabled="!canExpireSilence || commandPending" @click="expireSilence"><Volume2 :size="17" aria-hidden="true" />结束 Silence</button>
          <button type="button" class="primary-action" :disabled="alert.status !== 'firing' || commandPending" @click="createIncident"><Siren :size="17" aria-hidden="true" />创建 Incident</button>
          <button type="button" class="secondary-action" :disabled="!canInvestigate || commandPending" @click="investigate"><Bot :size="17" aria-hidden="true" />启动 Investigation</button>
        </div>
        <form class="attach-row" @submit.prevent="attachIncident">
          <label><span>现有 Incident ID</span><input v-model="attachIncidentID" name="incident_id" type="text" autocomplete="off" spellcheck="false" placeholder="例如：xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx…"></label>
          <button type="submit" class="secondary-action" :disabled="!attachIncidentID.trim() || commandPending"><Link2 :size="17" aria-hidden="true" />关联 Incident</button>
        </form>
      </section>

      <section class="facet-strip" aria-label="Alert lifecycle facets">
        <div><span>Acknowledgement</span><strong>{{ alert.acknowledgement ? `recurrence ${alert.acknowledgement.recurrence_no}` : "未 acknowledge" }}</strong><small>{{ alert.acknowledgement?.reason || "独立于 silence 与 resolution" }}</small></div>
        <div><span>Silence</span><strong>{{ alert.silence?.status || "无" }}</strong><small>{{ alert.silence ? `${formatTime(alert.silence.starts_at)} - ${formatTime(alert.silence.ends_at)}` : "Provider notification 未抑制" }}</small></div>
        <div><span>Incident links</span><strong>{{ alert.incident_links.length }}</strong><small>{{ activeIncident ? `active ${shortID(activeIncident.incident_id)}` : "无 active Incident" }}</small></div>
        <div><span>Investigation</span><strong>{{ alert.investigations.length }}</strong><small>{{ alert.investigations[0]?.status || "尚未启动" }}</small></div>
      </section>

      <section class="detail-section" aria-labelledby="identity-heading">
        <header><div><p class="eyebrow">Identity</p><h2 id="identity-heading">Alert aggregate</h2></div></header>
        <dl class="facts-grid">
          <div><dt>Alert ID</dt><dd class="mono-text">{{ alert.id }}</dd></div>
          <div><dt>Source</dt><dd>{{ alert.source }}</dd></div>
          <div><dt>Fingerprint</dt><dd class="mono-text">{{ alert.fingerprint }}</dd></div>
          <div><dt>Correlation key</dt><dd class="mono-text">{{ alert.correlation_key }}</dd></div>
          <div><dt>首次出现</dt><dd>{{ formatTime(alert.first_seen_at) }}</dd></div>
          <div><dt>最近出现</dt><dd>{{ formatTime(alert.last_seen_at) }}</dd></div>
          <div><dt>Resolved</dt><dd>{{ formatTime(alert.resolved_at) }}</dd></div>
          <div><dt>Legacy provenance</dt><dd>{{ alert.migrated_legacy ? "legacy_automatic_ingress" : "native Alert" }}</dd></div>
        </dl>
      </section>

      <section class="detail-section" aria-labelledby="context-links-heading">
        <header><div><p class="eyebrow">Context Links</p><h2 id="context-links-heading">继续调查</h2></div></header>
        <nav class="context-links" aria-label="Alert Context Links">
          <RouterLink v-for="link in contextLinks" :key="link.path" :to="{ path: link.path, query: link.query }">{{ link.label }}<ExternalLink :size="15" aria-hidden="true" /></RouterLink>
          <RouterLink v-for="link in alert.incident_links" :key="link.id" :to="{ name: 'incident-detail', params: { incidentId: link.incident_id } }">Incident {{ shortID(link.incident_id) }}<ExternalLink :size="15" aria-hidden="true" /></RouterLink>
        </nav>
      </section>

      <section class="detail-section" aria-labelledby="incident-links-heading">
        <header><div><p class="eyebrow">Relationships</p><h2 id="incident-links-heading">Investigation 与 Incident</h2></div></header>
        <p v-if="alert.incident_links.length === 0" class="empty-line">Alert 尚未关联 Incident。</p>
        <div v-else class="relation-table" role="region" aria-label="Alert Incident relationships" tabindex="0">
          <table><thead><tr><th>Incident</th><th>状态</th><th>Cycle</th><th>Provenance</th><th>Configuration Revision / Policy</th></tr></thead><tbody><tr v-for="link in alert.incident_links" :key="link.id"><td><RouterLink :to="{ name: 'incident-detail', params: { incidentId: link.incident_id } }">{{ link.incident_id }}</RouterLink></td><td>{{ link.incident_status }}</td><td>{{ link.incident_cycle }}</td><td>{{ provenanceLabel(link.provenance) }}</td><td><code>{{ link.configuration_revision_id || "Owner command" }}</code><code v-if="link.escalation_policy_id">{{ link.escalation_policy_id }}</code></td></tr></tbody></table>
        </div>
      </section>

      <section class="detail-section" aria-labelledby="signals-heading">
        <header><div><p class="eyebrow">Source Facts</p><h2 id="signals-heading">Signals</h2></div><span>{{ detail.signals.length }}</span></header>
        <ol class="signal-list">
          <li v-for="signal in detail.signals" :key="signal.id">
            <header><AlertBadges :status="signal.status" :severity="signal.severity" /><time :datetime="signal.occurred_at">{{ formatTime(signal.occurred_at) }}</time></header>
            <strong>{{ signal.summary }}</strong>
            <dl><div><dt>Signal ID</dt><dd>{{ signal.id }}</dd></div><div><dt>Source event</dt><dd>{{ signal.source_event_id }}</dd></div><div><dt>Alert instance</dt><dd>{{ signal.alert_instance_key }}</dd></div><div><dt>Provenance</dt><dd>{{ provenanceLabel(signal.provenance) }}</dd></div></dl>
            <details><summary>Labels 与 annotations</summary><pre>{{ JSON.stringify({ labels: signal.labels, annotations: signal.annotations }, null, 2) }}</pre></details>
          </li>
        </ol>
      </section>

      <section class="detail-section" aria-labelledby="timeline-heading">
        <header><div><p class="eyebrow">Audit</p><h2 id="timeline-heading">Timeline</h2></div><span>{{ detail.events.length }}</span></header>
        <ol class="timeline-list">
          <li v-for="event in detail.events" :key="event.id"><span class="timeline-mark" aria-hidden="true"></span><div><header><strong>{{ event.type }}</strong><time :datetime="event.occurred_at">{{ formatTime(event.occurred_at) }}</time></header><p>{{ event.summary }}</p><small>{{ event.actor_type }} / {{ event.actor_id }}</small><details><summary>Metadata</summary><pre>{{ JSON.stringify(event.metadata, null, 2) }}</pre></details></div></li>
        </ol>
      </section>
    </template>
  </article>
</template>

<style scoped>
.alert-detail-view { display: grid; width: min(100%, var(--co-content-max-width)); min-width: 0; margin: 0 auto; gap: var(--co-space-5); }
.back-link { display: inline-flex; width: fit-content; min-height: 42px; align-items: center; gap: var(--co-space-2); color: var(--co-action-primary); font-size: 13px; font-weight: 750; }
.back-link:hover { color: var(--co-action-hover); text-decoration: underline; }
.detail-heading, .command-surface > header, .detail-section > header { display: flex; min-width: 0; align-items: flex-end; justify-content: space-between; gap: var(--co-space-4); }
.detail-heading h1, .command-surface h2, .detail-section h2 { margin: 0; }
.detail-heading h1 { max-width: 34ch; overflow-wrap: anywhere; font-size: 30px; }
.detail-heading p:not(.eyebrow) { margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.heading-state { display: flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-3); color: var(--co-text-muted); }
.eyebrow { margin: 0 0 var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.primary-action, .secondary-action, .icon-action { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; gap: var(--co-space-2); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); cursor: pointer; font-size: 12px; font-weight: 750; }
.primary-action { padding: 0 var(--co-space-4); border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.secondary-action { padding: 0 var(--co-space-3); color: var(--co-text-primary); background: var(--co-bg-surface); }
.icon-action { width: 42px; flex: 0 0 42px; padding: 0; color: var(--co-text-secondary); background: var(--co-bg-surface); }
button:hover { border-color: var(--co-border-strong); }
button:disabled, select:disabled { cursor: not-allowed; opacity: .55; }
.message-band { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); color: var(--co-text-secondary); background: var(--co-bg-surface); overflow-wrap: anywhere; }
.message-band.is-error { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.message-band.is-warning { border-color: var(--co-status-warning-border); color: var(--co-status-warning-fg); background: var(--co-status-warning-bg); }
.message-band .secondary-action { margin-left: auto; }
.command-surface, .detail-section { display: grid; min-width: 0; gap: var(--co-space-4); padding-block: var(--co-space-5); border-block: 1px solid var(--co-border-default); }
.command-surface h2, .detail-section h2 { font-size: 20px; }
.command-row { display: flex; min-width: 0; flex-wrap: wrap; align-items: end; gap: var(--co-space-2); }
.duration-select { display: grid; min-width: 130px; gap: 4px; color: var(--co-text-muted); font-size: 10px; font-weight: 700; }
.duration-select select, .attach-row input { width: 100%; min-width: 0; min-height: 42px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); }
.attach-row { display: grid; grid-template-columns: minmax(260px, 520px) auto; align-items: end; gap: var(--co-space-2); }
.attach-row label { display: grid; gap: 4px; color: var(--co-text-muted); font-size: 10px; font-weight: 700; }
.facet-strip { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.facet-strip div { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-4); border-right: 1px solid var(--co-border-default); }
.facet-strip div:last-child { border-right: 0; }
.facet-strip span, .facet-strip small { color: var(--co-text-muted); font-size: 10px; }
.facet-strip strong, .facet-strip small { overflow-wrap: anywhere; }
.facts-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.facts-grid div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.facts-grid dt { color: var(--co-text-muted); font-size: 10px; }
.facts-grid dd { margin: 3px 0 0; overflow-wrap: anywhere; font-size: 11px; font-weight: 700; }
.context-links { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-2); }
.context-links a { display: inline-flex; min-height: 38px; align-items: center; gap: var(--co-space-2); padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-action-primary); background: var(--co-bg-surface); font-size: 12px; font-weight: 750; }
.context-links a:hover { border-color: var(--co-action-primary); background: var(--co-bg-active); }
.empty-line { margin: 0; padding: var(--co-space-5); color: var(--co-text-muted); text-align: center; }
.relation-table { min-width: 0; overflow-x: auto; }
table { width: 100%; min-width: 920px; border-collapse: collapse; background: var(--co-bg-surface); font-size: 11px; }
th, td { padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); text-align: left; vertical-align: top; }
th { color: var(--co-text-muted); font-size: 10px; text-transform: uppercase; }
td a { color: var(--co-action-primary); font-family: var(--co-font-mono); }
td code { display: block; max-width: 300px; overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; }
.detail-section > header > span { color: var(--co-text-muted); font-family: var(--co-font-mono); }
.signal-list, .timeline-list { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.signal-list > li { display: grid; min-width: 0; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.signal-list > li > header, .timeline-list header { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.signal-list time, .timeline-list time { color: var(--co-text-muted); font-size: 11px; }
.signal-list dl { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; }
.signal-list dl div { min-width: 0; padding: var(--co-space-2); border-left: 1px solid var(--co-border-default); }
.signal-list dt { color: var(--co-text-muted); font-size: 10px; }
.signal-list dd { margin: 2px 0 0; overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
details summary { width: fit-content; color: var(--co-action-primary); cursor: pointer; font-size: 11px; font-weight: 700; }
pre { max-height: 320px; margin: var(--co-space-2) 0 0; padding: var(--co-space-3); overflow: auto; border: 1px solid var(--co-border-default); color: var(--co-text-secondary); background: var(--co-bg-subtle); font-size: 10px; white-space: pre-wrap; overflow-wrap: anywhere; }
.timeline-list > li { display: grid; grid-template-columns: 18px minmax(0, 1fr); gap: var(--co-space-2); }
.timeline-list > li > div { min-width: 0; padding: var(--co-space-3) var(--co-space-4); border-left: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.timeline-mark { width: 9px; height: 9px; margin-top: 17px; border: 2px solid var(--co-action-primary); border-radius: 50%; background: var(--co-bg-canvas); }
.timeline-list p { margin: var(--co-space-1) 0; color: var(--co-text-secondary); overflow-wrap: anywhere; }
.timeline-list small { color: var(--co-text-muted); }
@media (max-width: 1050px) { .facet-strip, .facts-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .facet-strip div:nth-child(2) { border-right: 0; } .facet-strip div:nth-child(-n+2) { border-bottom: 1px solid var(--co-border-default); } .signal-list dl { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 767px) {
  .detail-heading { align-items: flex-start; flex-direction: column; }
  .detail-heading h1 { font-size: 24px; }
  .command-row { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .command-row > * { width: 100%; min-width: 0; }
  .duration-select select, .attach-row input { font-size: 16px; }
  .attach-row { grid-template-columns: 1fr; }
  .facet-strip, .facts-grid, .signal-list dl { grid-template-columns: 1fr; }
  .facet-strip div { border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .signal-list dl div { border-top: 1px solid var(--co-border-default); border-left: 0; }
  .signal-list > li > header, .timeline-list header { align-items: flex-start; flex-direction: column; }
}
@media (max-width: 420px) { .command-row { grid-template-columns: 1fr; } }
</style>
