<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, toRaw, watch } from "vue";
import {
  CheckCircle2,
  FlaskConical,
  KeyRound,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  ShieldCheck,
  Trash2,
} from "lucide-vue-next";
import { onBeforeRouteLeave } from "vue-router";

import { isApiError } from "../../api/client";
import {
  applyConfiguration,
  configurationDraft,
  createSecret,
  getSettings,
  getStorageStatus,
  testProvider,
  validateSettings,
  type ConfigurationDraft,
  type ConfigurationRevision,
  type ConfigurationValidation,
  type EscalationPolicy,
  type ProviderIdentity,
  type ProviderResult,
  type ProviderState,
  type SettingsSnapshot,
  type StorageStatus,
} from "../../api/platform";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";

const settings = ref<SettingsSnapshot | null>(null);
const storage = ref<StorageStatus | null>(null);
const draft = ref<ConfigurationDraft | null>(null);
const baselineDraftJSON = ref("");
const validatedDraftJSON = ref("");
const validation = ref<ConfigurationValidation | null>(null);
const providerResults = ref<Partial<Record<ProviderIdentity, ProviderResult>>>({});
const loading = ref(true);
const validating = ref(false);
const applying = ref(false);
const testingProvider = ref<ProviderIdentity | null>(null);
const savingSecret = ref(false);
const error = ref("");
const statusMessage = ref("");
const secretProvider = ref<ProviderIdentity>("llm");
const secretPurpose = ref("api_key");
const secretValue = ref("");
const selectedScopeIndex = ref(0);
let mounted = true;

const providerLabels: Record<ProviderIdentity | "mysql", string> = {
  llm: "LLM",
  kubernetes: "Kubernetes",
  prometheus: "Prometheus",
  alertmanager: "Alertmanager",
  elasticsearch: "Elasticsearch",
  tempo: "Tempo",
  github: "GitHub",
  argocd: "Argo CD",
  mysql: "MySQL",
};
const providerOptions: ProviderIdentity[] = [
  "llm",
  "kubernetes",
  "prometheus",
  "alertmanager",
  "elasticsearch",
  "tempo",
  "github",
  "argocd",
];
const severityOptions: EscalationPolicy["severities"] = ["critical", "warning", "info", "unknown"];

const dateFormatter = new Intl.DateTimeFormat("zh-CN", { dateStyle: "medium", timeStyle: "medium" });
const numberFormatter = new Intl.NumberFormat("zh-CN");
const hasUnsavedChanges = computed(() => Boolean(draft.value && baselineDraftJSON.value && JSON.stringify(draft.value) !== baselineDraftJSON.value));
const validationStaleLocally = computed(() => Boolean(validation.value && draft.value && JSON.stringify(draft.value) !== validatedDraftJSON.value));
const activeRevision = computed(() => settings.value?.active_revision ?? null);

function stateLabel(state: ProviderState): string {
  return ({ available: "可用", partial: "部分可用", unavailable: "不可用", disabled: "已停用", not_configured: "未配置" } satisfies Record<ProviderState, string>)[state];
}

function activationLabel(status?: string): string {
  return ({ ready: "等待 Worker", running: "Worker 应用中", succeeded: "Worker 已生效", failed: "Worker 应用失败" } as Record<string, string>)[status ?? ""] ?? "暂无状态";
}

function formatTime(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : dateFormatter.format(date);
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "未知";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = value;
  let index = 0;
  while (amount >= 1024 && index < units.length - 1) {
    amount /= 1024;
    index += 1;
  }
  return `${new Intl.NumberFormat("zh-CN", { maximumFractionDigits: 1 }).format(amount)} ${units[index]}`;
}

function shortHash(value: string): string {
  return value.length > 16 ? `${value.slice(0, 12)}…` : value;
}

function providerField(provider: ProviderIdentity, field: string): string {
  return `providers.${provider}.${field}`;
}

function describeError(reason: unknown, fallback: string): string {
  if (!isApiError(reason)) return fallback;
  const next = reason.nextSteps.length ? `；下一步：${reason.nextSteps.join("；")}` : "";
  return `${reason.code || "REQUEST_FAILED"}：${reason.message}${next}`;
}

async function loadSettings(resetDraft = true) {
  loading.value = true;
  error.value = "";
  try {
    const [snapshot, storageStatus] = await Promise.all([getSettings(), getStorageStatus()]);
    if (!mounted) return;
    settings.value = snapshot;
    storage.value = storageStatus;
    if (resetDraft) setDraftFromRevision(snapshot.active_revision);
  } catch (reason) {
    error.value = describeError(reason, "Settings 读取失败，请检查本地 API 与 MySQL。 ");
  } finally {
    loading.value = false;
  }
}

function setDraftFromRevision(revision: ConfigurationRevision) {
  const next = configurationDraft(toRaw(revision));
  selectedScopeIndex.value = Math.max(0, next.scopes.findIndex((scope) => scope.id === next.scope.id));
  draft.value = next;
  syncDefaultScope();
  baselineDraftJSON.value = JSON.stringify(draft.value);
  validatedDraftJSON.value = "";
  validation.value = null;
  providerResults.value = {};
}

function syncDefaultScope() {
  const scopes = draft.value?.scopes;
  if (!draft.value || !scopes?.length) return;
  selectedScopeIndex.value = Math.min(Math.max(selectedScopeIndex.value, 0), scopes.length - 1);
  draft.value.scope = structuredClone(toRaw(scopes[selectedScopeIndex.value]));
}

function selectDefaultScope(index: number) {
  selectedScopeIndex.value = index;
  syncDefaultScope();
}

function isDefaultScope(index: number): boolean {
  return index === selectedScopeIndex.value;
}

function updateScopeNamespaces(index: number, event: Event) {
  const scope = draft.value?.scopes[index];
  if (!scope) return;
  scope.namespaces = (event.currentTarget as HTMLInputElement).value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

async function addScope() {
  if (!draft.value || draft.value.scopes.length >= 10) return;
  const current = draft.value.scopes[selectedScopeIndex.value] ?? draft.value.scope;
  draft.value.scopes.push({
    name: "",
    cluster_id: "",
    environment: current.environment || "local",
    namespaces: [...current.namespaces],
    active: false,
  });
  const index = draft.value.scopes.length - 1;
  await nextTick();
  document.querySelector<HTMLInputElement>(`[data-field="scopes.${index}.name"]`)?.focus();
}

function removeScope(index: number) {
  if (!draft.value || draft.value.scopes.length <= 1) return;
  const scope = draft.value.scopes[index];
  if (!window.confirm(`从当前草稿删除集群 ${scope.cluster_id || index + 1}？`)) return;
  draft.value.scopes.splice(index, 1);
  if (selectedScopeIndex.value > index) selectedScopeIndex.value -= 1;
  else if (selectedScopeIndex.value === index) selectedScopeIndex.value = Math.min(index, draft.value.scopes.length - 1);
  syncDefaultScope();
}

function scopeErrors(index: number) {
  const prefix = `scopes.${index}.`;
  return validation.value?.errors.filter((item) => item.field.startsWith(prefix)) ?? [];
}

async function addEscalationPolicy() {
  if (!draft.value || draft.value.escalation_policies.length >= 50) return;
  draft.value.escalation_policies.push({
    name: "",
    enabled: true,
    severities: ["critical"],
    namespaces: [],
    label_matchers: {},
    minimum_firing_seconds: 0,
    minimum_recurrence_count: 1,
    create_incident: true,
  });
  const index = draft.value.escalation_policies.length - 1;
  await nextTick();
  document.querySelector<HTMLInputElement>(`[data-field="escalation_policies.${index}.name"]`)?.focus();
}

function removeEscalationPolicy(index: number) {
  if (!draft.value) return;
  const policy = draft.value.escalation_policies[index];
  if (!window.confirm(`从当前草稿删除 Policy ${policy.name || index + 1}？`)) return;
  draft.value.escalation_policies.splice(index, 1);
}

function updatePolicyNamespaces(index: number, event: Event) {
  const policy = draft.value?.escalation_policies[index];
  if (!policy) return;
  policy.namespaces = (event.currentTarget as HTMLInputElement).value
    .split(",")
    .map((item) => item.trim())
    .filter(Boolean);
}

function policyMatchersText(policy: EscalationPolicy): string {
  return Object.entries(policy.label_matchers)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${name}=${value}`)
    .join("\n");
}

function updatePolicyMatchers(index: number, event: Event) {
  const policy = draft.value?.escalation_policies[index];
  if (!policy) return;
  const result: Record<string, string> = {};
  for (const line of (event.currentTarget as HTMLTextAreaElement).value.split("\n")) {
    const separator = line.indexOf("=");
    if (separator < 1) continue;
    const name = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (name && value) result[name] = value;
  }
  policy.label_matchers = result;
}

function policyErrors(index: number) {
  const prefix = `escalation_policies.${index}.`;
  return validation.value?.errors.filter((item) => item.field.startsWith(prefix)) ?? [];
}

function draftSnapshot(): ConfigurationDraft | null {
  syncDefaultScope();
  return draft.value ? structuredClone(toRaw(draft.value)) : null;
}

function resetDraft() {
  if (!activeRevision.value) return;
  setDraftFromRevision(activeRevision.value);
  error.value = "";
  statusMessage.value = "草稿已恢复为当前活动 Revision。";
}

async function focusFirstError() {
  await nextTick();
  const first = validation.value?.errors[0];
  if (!first) return;
  let field = first.field;
  if (field === "scope") field = `scopes.${selectedScopeIndex.value}.cluster_id`;
  else if (field.startsWith("scope.")) field = `scopes.${selectedScopeIndex.value}.${field.slice("scope.".length)}`;
  document.querySelector<HTMLElement>(`[data-field="${field}"]`)?.focus();
}

async function runValidation() {
  const snapshot = draftSnapshot();
  if (!snapshot || validating.value) return;
  validating.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const result = await validateSettings(snapshot);
    validation.value = result;
    validatedDraftJSON.value = JSON.stringify(draft.value);
    statusMessage.value = result.valid
      ? `验证通过，有效期至 ${formatTime(result.expires_at)}。`
      : `验证未通过，共 ${numberFormatter.format(result.errors.length)} 个字段错误。`;
    if (!result.valid) await focusFirstError();
  } catch (reason) {
    error.value = describeError(reason, "配置验证失败。 ");
  } finally {
    validating.value = false;
  }
}

async function runProviderTest(provider: ProviderIdentity) {
  const snapshot = draftSnapshot();
  if (!snapshot || testingProvider.value) return;
  const configuration = snapshot.providers.find((item) => item.provider === provider);
  if (!configuration) return;
  testingProvider.value = provider;
  error.value = "";
  try {
    const result = await testProvider(
      configuration,
      snapshot.secret_references.filter((item) => item.provider === provider),
      snapshot.scope.cluster_id,
    );
    providerResults.value = { ...providerResults.value, [provider]: result };
  } catch (reason) {
    error.value = describeError(reason, `${providerLabels[provider]} 连接测试失败。`);
  } finally {
    testingProvider.value = null;
  }
}

async function saveSecret() {
  if (!draft.value || !secretValue.value || savingSecret.value) return;
  savingSecret.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const secret = await createSecret({ provider: secretProvider.value, purpose: secretPurpose.value, value: secretValue.value });
    draft.value.secret_references = draft.value.secret_references.filter(
      (item) => item.provider !== secret.provider || item.purpose !== secret.purpose,
    );
    draft.value.secret_references.push({
      provider: secret.provider,
      purpose: secret.purpose,
      secret_version_id: secret.id,
      state: secret.state,
      fingerprint: secret.fingerprint,
    });
    secretValue.value = "";
    statusMessage.value = `${providerLabels[secret.provider]} secret version 已写入并加入当前草稿。`;
    try {
      storage.value = await getStorageStatus();
    } catch {
      // The secret write succeeded; a later refresh can recover storage diagnostics.
    }
  } catch (reason) {
    secretValue.value = "";
    error.value = describeError(reason, "Secret version 写入失败。 ");
  } finally {
    savingSecret.value = false;
  }
}

async function applyDraft() {
  const snapshot = draftSnapshot();
  if (!snapshot || !validation.value?.valid || applying.value) return;
  applying.value = true;
  error.value = "";
  statusMessage.value = "";
  try {
    const revision = await applyConfiguration(validation.value.id, snapshot);
    statusMessage.value = `Configuration Revision #${revision.number} 已发布，正在等待 Worker 边界确认。`;
    await loadSettings(true);
    window.dispatchEvent(new Event("cloudops:configuration-applied"));
    await waitForWorkerBoundary(revision.id);
  } catch (reason) {
    error.value = describeError(reason, "Configuration Revision 发布失败。 ");
    try {
      settings.value = await getSettings();
    } catch {
      // Keep the publish error as the primary failure.
    }
  } finally {
    applying.value = false;
  }
}

async function waitForWorkerBoundary(revisionID: string) {
  for (let attempt = 0; attempt < 20 && mounted; attempt += 1) {
    let snapshot: SettingsSnapshot;
    try {
      snapshot = await getSettings();
    } catch {
      statusMessage.value = "Revision 已发布；Worker 边界状态暂时无法读取。";
      return;
    }
    if (!mounted) return;
    settings.value = snapshot;
    const revision = snapshot.history.find((item) => item.id === revisionID) ?? snapshot.active_revision;
    const status = revision.worker_boundary?.status;
    if (status === "succeeded") {
      statusMessage.value = `Configuration Revision #${revision.number} 已在 Worker 边界生效。`;
      return;
    }
    if (status === "failed") {
      error.value = `WORKER_ACTIVATION_FAILED：${revision.worker_boundary?.last_error || "Worker 未能应用配置"}`;
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 500));
  }
  if (mounted) statusMessage.value = "Revision 已发布；Worker 边界仍在等待确认。";
}

function guardUnsavedChanges(): boolean {
  return !hasUnsavedChanges.value || window.confirm("当前 Settings 草稿尚未发布，确定离开吗？");
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!hasUnsavedChanges.value) return;
  event.preventDefault();
  event.returnValue = "";
}

async function handleBrowserNotificationToggle() {
  if (!draft.value?.general.browser_notifications_enabled) return;
  if (!("Notification" in window)) {
    draft.value.general.browser_notifications_enabled = false;
    error.value = "BROWSER_NOTIFICATIONS_UNAVAILABLE：当前浏览器不支持 system notification。";
    return;
  }
  const permission = window.Notification.permission === "default"
    ? await window.Notification.requestPermission()
    : window.Notification.permission;
  if (permission !== "granted") {
    draft.value.general.browser_notifications_enabled = false;
    error.value = "BROWSER_NOTIFICATION_PERMISSION_DENIED：浏览器未授予通知权限。";
  }
}

watch(secretProvider, (provider) => {
  secretPurpose.value = provider === "llm" ? "api_key" : provider === "kubernetes" ? "credential" : "token";
});

watch(() => draft.value?.scopes, syncDefaultScope, { deep: true });

function handleOperationalScopeChanged() {
  void loadSettings(false);
}

onBeforeRouteLeave(guardUnsavedChanges);
onMounted(() => {
  window.addEventListener("beforeunload", handleBeforeUnload);
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  void loadSettings(true);
});
onBeforeUnmount(() => {
  mounted = false;
  window.removeEventListener("beforeunload", handleBeforeUnload);
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
});
</script>

<template>
  <article class="settings-view">
    <header class="page-heading">
      <div>
        <p class="eyebrow">Operational Configuration</p>
        <h1>设置</h1>
        <p>活动配置、Provider 边界与本地存储状态。</p>
      </div>
      <button type="button" class="secondary-action" :disabled="loading" @click="loadSettings(!hasUnsavedChanges)">
        <RefreshCw :size="18" aria-hidden="true" />{{ loading ? "刷新中…" : "刷新状态" }}
      </button>
    </header>

    <div class="action-bar" aria-label="配置发布操作">
      <span :class="{ 'is-dirty': hasUnsavedChanges }">{{ hasUnsavedChanges ? "有未发布修改" : "草稿与活动 Revision 一致" }}</span>
      <div>
        <button type="button" class="secondary-action" :disabled="!hasUnsavedChanges || validating || applying" @click="resetDraft">
          <RotateCcw :size="17" aria-hidden="true" />恢复
        </button>
        <button type="submit" form="settings-form" class="secondary-action" :disabled="!draft || validating || applying">
          <FlaskConical :size="17" aria-hidden="true" />{{ validating ? "验证中…" : "验证草稿" }}
        </button>
        <button type="button" class="primary-action" :disabled="!validation?.valid || applying" @click="applyDraft">
          <Save :size="17" aria-hidden="true" />{{ applying ? "发布中…" : "发布 Revision" }}
        </button>
      </div>
    </div>

    <div v-if="error" class="message-band is-error" role="alert"><strong>操作失败</strong><span>{{ error }}</span></div>
    <div v-if="statusMessage" class="message-band is-success" role="status" aria-live="polite"><CheckCircle2 :size="18" aria-hidden="true" /><span>{{ statusMessage }}</span></div>
    <div v-if="loading && !settings" class="message-band" role="status" aria-live="polite">正在读取 Settings…</div>

    <template v-if="settings && draft">
      <section class="revision-summary" aria-labelledby="active-revision-title">
        <div>
          <p class="eyebrow">Active Revision</p>
          <h2 id="active-revision-title">Configuration Revision #{{ settings.active_revision.number }}</h2>
          <p>{{ settings.active_revision.summary }}</p>
        </div>
        <dl>
          <div><dt>Hash</dt><dd class="mono-text" :title="settings.active_revision.hash">{{ shortHash(settings.active_revision.hash) }}</dd></div>
          <div><dt>创建时间</dt><dd>{{ formatTime(settings.active_revision.created_at) }}</dd></div>
          <div><dt>Worker</dt><dd :class="`activation-${settings.active_revision.worker_boundary?.status || 'none'}`">{{ activationLabel(settings.active_revision.worker_boundary?.status) }}</dd></div>
          <div><dt>Worker ID</dt><dd class="mono-text">{{ settings.active_revision.worker_boundary?.worker_id || "无" }}</dd></div>
        </dl>
      </section>

      <form id="settings-form" autocomplete="off" novalidate @submit.prevent="runValidation">
        <section id="operational-scope" class="settings-section" aria-labelledby="scope-heading">
          <header>
            <div><p class="eyebrow">Scope</p><h2 id="scope-heading">Operational Scope</h2></div>
            <button type="button" class="secondary-action" data-field="scopes" :disabled="draft.scopes.length >= 10" @click="addScope">
              <Plus :size="17" aria-hidden="true" />添加集群
            </button>
          </header>
          <div class="form-grid scope-summary">
            <label class="span-two"><span>变更摘要</span><input v-model="draft.summary" data-field="summary" name="summary" type="text" autocomplete="off" maxlength="255" placeholder="例如：更新集群连接与 Provider 配置…"></label>
          </div>
          <div class="scope-list">
            <article v-for="(scope, index) in draft.scopes" :key="scope.id || `draft-scope-${index}`" class="scope-item" :class="{ 'is-default': isDefaultScope(index) }">
              <header>
                <label class="scope-default">
                  <input
                  type="radio"
                  autocomplete="off"
                    name="default_scope"
                    :checked="isDefaultScope(index)"
                    :value="index"
                    @change="selectDefaultScope(index)"
                  >
                  <span>Revision 默认</span>
                </label>
                <span v-if="scope.active" class="active-scope-state">当前活动</span>
                <button
                  type="button"
                  class="scope-remove"
                  :disabled="draft.scopes.length <= 1"
                  :aria-label="`删除集群 ${scope.cluster_id || index + 1}`"
                  title="删除集群"
                  @click="removeScope(index)"
                >
                  <Trash2 :size="17" aria-hidden="true" />
                </button>
              </header>
              <div class="form-grid">
                <label><span>Scope 名称</span><input v-model="scope.name" :data-field="`scopes.${index}.name`" :name="`scope_${index}_name`" type="text" autocomplete="off" maxlength="128" placeholder="例如：本地 CloudOps…"></label>
                <label><span>Cluster identity</span><input v-model="scope.cluster_id" :data-field="`scopes.${index}.cluster_id`" :name="`scope_${index}_cluster_id`" type="text" autocomplete="off" maxlength="128" spellcheck="false" placeholder="例如：cloudops-local…"></label>
                <label><span>Environment</span><input v-model="scope.environment" :data-field="`scopes.${index}.environment`" :name="`scope_${index}_environment`" type="text" autocomplete="off" maxlength="64" spellcheck="false" placeholder="例如：local…"></label>
                <label><span>Namespaces（逗号分隔）</span><input :value="scope.namespaces.join(', ')" :data-field="`scopes.${index}.namespaces`" :name="`scope_${index}_namespaces`" type="text" autocomplete="off" spellcheck="false" placeholder="例如：demo, monitoring…" @input="updateScopeNamespaces(index, $event)"></label>
              </div>
              <ul v-if="scopeErrors(index).length" class="field-error-list">
                <li v-for="item in scopeErrors(index)" :key="`${item.field}-${item.code}`"><code>{{ item.code }}</code> {{ item.message }}</li>
              </ul>
            </article>
          </div>
          <ul v-if="validation?.errors.some((item) => item.field === 'summary' || item.field === 'scope' || item.field === 'scopes' || item.field.startsWith('scope.'))" class="field-error-list">
            <li v-for="item in validation.errors.filter((candidate) => candidate.field === 'summary' || candidate.field === 'scope' || candidate.field === 'scopes' || candidate.field.startsWith('scope.'))" :key="`${item.field}-${item.code}`"><code>{{ item.code }}</code> {{ item.message }}</li>
          </ul>
        </section>

        <section class="settings-section" aria-labelledby="bounds-heading">
          <header><div><p class="eyebrow">Bounds</p><h2 id="bounds-heading">查询与保留</h2></div></header>
          <div class="form-grid form-grid--three">
            <label><span>最大回看时间（秒）</span><input v-model.number="draft.general.query_max_lookback_seconds" data-field="general.query_max_lookback_seconds" name="query_max_lookback_seconds" type="number" inputmode="numeric" autocomplete="off" min="60" max="2592000"></label>
            <label><span>查询结果上限</span><input v-model.number="draft.general.query_max_results" data-field="general.query_max_results" name="query_max_results" type="number" inputmode="numeric" autocomplete="off" min="1" max="10000"></label>
            <label><span>Telemetry 保留天数</span><input v-model.number="draft.general.telemetry_retention_days" data-field="general.telemetry_retention_days" name="telemetry_retention_days" type="number" inputmode="numeric" autocomplete="off" min="1" max="365"></label>
          </div>
          <div class="toggle-row">
            <label><input v-model="draft.general.browser_notifications_enabled" name="browser_notifications_enabled" type="checkbox" autocomplete="off" @change="handleBrowserNotificationToggle"><span>浏览器提醒</span></label>
            <label><input v-model="draft.general.automatic_escalation_enabled" data-field="general.automatic_escalation_enabled" name="automatic_escalation_enabled" type="checkbox" autocomplete="off"><span>自动 escalation</span></label>
          </div>
          <ul v-if="validation?.errors.some((item) => item.field.startsWith('general.'))" class="field-error-list">
            <li v-for="item in validation.errors.filter((candidate) => candidate.field.startsWith('general.'))" :key="`${item.field}-${item.code}`"><code>{{ item.code }}</code> {{ item.message }}</li>
          </ul>
        </section>

        <section id="escalation-policies" class="settings-section" aria-labelledby="escalation-heading">
          <header>
            <div>
              <p class="eyebrow">Alert Escalation</p>
              <h2 id="escalation-heading">Escalation Policies</h2>
            </div>
            <button type="button" class="secondary-action" data-field="escalation_policies" :disabled="draft.escalation_policies.length >= 50" @click="addEscalationPolicy">
              <Plus :size="17" aria-hidden="true" />添加 Policy
            </button>
          </header>
          <div class="policy-revision">
            <span>源 Configuration Revision #{{ activeRevision?.number }}</span>
            <code>{{ activeRevision?.id }}</code>
          </div>
          <p v-if="draft.escalation_policies.length === 0" class="empty-line">当前 Revision 没有 Escalation Policy，自动 escalation 保持关闭。</p>
          <div v-else class="policy-list">
            <article v-for="(policy, index) in draft.escalation_policies" :key="policy.id || `draft-policy-${index}`" class="policy-item">
              <header>
                <label class="policy-toggle">
                  <input v-model="policy.enabled" :name="`policy_${index}_enabled`" type="checkbox" autocomplete="off">
                  <span>{{ policy.enabled ? "已启用" : "已停用" }}</span>
                </label>
                <span class="policy-action">创建 Incident</span>
                <button type="button" class="scope-remove" :aria-label="`删除 Policy ${policy.name || index + 1}`" title="删除 Policy" @click="removeEscalationPolicy(index)">
                  <Trash2 :size="17" aria-hidden="true" />
                </button>
              </header>
              <div class="form-grid policy-grid">
                <label class="span-two"><span>Policy 名称</span><input v-model="policy.name" :data-field="`escalation_policies.${index}.name`" :name="`policy_${index}_name`" type="text" maxlength="128" autocomplete="off" placeholder="例如：production critical…"></label>
                <fieldset class="span-two severity-picker" :data-field="`escalation_policies.${index}.severities`">
                  <legend>Severity</legend>
                  <label v-for="severity in severityOptions" :key="severity"><input v-model="policy.severities" :name="`policy_${index}_severity`" type="checkbox" autocomplete="off" :value="severity"><span>{{ severity }}</span></label>
                </fieldset>
                <label><span>持续 firing（秒）</span><input v-model.number="policy.minimum_firing_seconds" :data-field="`escalation_policies.${index}.minimum_firing_seconds`" :name="`policy_${index}_duration`" type="number" inputmode="numeric" autocomplete="off" min="0" max="604800"></label>
                <label><span>最小复发次数</span><input v-model.number="policy.minimum_recurrence_count" :data-field="`escalation_policies.${index}.minimum_recurrence_count`" :name="`policy_${index}_recurrence`" type="number" inputmode="numeric" autocomplete="off" min="1" max="100"></label>
                <label class="span-two"><span>Namespaces（逗号分隔，留空表示不限）</span><input :value="policy.namespaces.join(', ')" :data-field="`escalation_policies.${index}.namespaces`" :name="`policy_${index}_namespaces`" type="text" autocomplete="off" spellcheck="false" placeholder="例如：production, payments…" @input="updatePolicyNamespaces(index, $event)"></label>
                <label class="span-two"><span>Exact label matchers（每行 name=value）</span><textarea :value="policyMatchersText(policy)" :data-field="`escalation_policies.${index}.label_matchers`" :name="`policy_${index}_matchers`" rows="3" autocomplete="off" spellcheck="false" placeholder="team=platform&#10;service=checkout" @input="updatePolicyMatchers(index, $event)"></textarea></label>
              </div>
              <ul v-if="policyErrors(index).length" class="field-error-list">
                <li v-for="item in policyErrors(index)" :key="`${item.field}-${item.code}`"><code>{{ item.code }}</code> {{ item.message }}</li>
              </ul>
            </article>
          </div>
          <ul v-if="validation?.errors.some((item) => item.field === 'escalation_policies')" class="field-error-list">
            <li v-for="item in validation.errors.filter((candidate) => candidate.field === 'escalation_policies')" :key="`${item.field}-${item.code}`"><code>{{ item.code }}</code> {{ item.message }}</li>
          </ul>
        </section>

        <section id="providers" class="settings-section" aria-labelledby="providers-heading">
          <header><div><p class="eyebrow">Connections</p><h2 id="providers-heading">Provider 配置</h2></div></header>
          <div class="provider-list">
            <article v-for="item in draft.providers" :key="item.provider" class="provider-item">
              <header>
                <div><strong>{{ providerLabels[item.provider] }}</strong><span class="mono-text">{{ item.provider }}</span></div>
                <label class="provider-toggle"><input v-model="item.enabled" :name="`${item.provider}_enabled`" type="checkbox" autocomplete="off"><span>{{ item.enabled ? "已启用" : "已停用" }}</span></label>
              </header>
              <div class="form-grid form-grid--provider">
                <label class="span-two"><span>Endpoint</span><input v-model="item.endpoint" :data-field="providerField(item.provider, 'endpoint')" :name="`${item.provider}_endpoint`" type="url" inputmode="url" autocomplete="off" spellcheck="false" placeholder="例如：https://provider.example…"></label>
                <label v-if="item.provider === 'llm'" class="span-two"><span>Model</span><input v-model="item.model" :data-field="providerField(item.provider, 'model')" name="llm_model" type="text" autocomplete="off" spellcheck="false" placeholder="例如：deepseek-chat…"></label>
                <label><span>Timeout（ms）</span><input v-model.number="item.timeout_ms" :data-field="providerField(item.provider, 'timeout_ms')" :name="`${item.provider}_timeout_ms`" type="number" inputmode="numeric" autocomplete="off" min="1000" max="60000"></label>
                <label><span>结果上限</span><input v-model.number="item.max_results" :data-field="providerField(item.provider, 'max_results')" :name="`${item.provider}_max_results`" type="number" inputmode="numeric" autocomplete="off" min="1" max="10000"></label>
                <label class="span-two"><span>Context Link base</span><input v-model="item.context_link_base" :data-field="providerField(item.provider, 'context_link_base')" :name="`${item.provider}_context_link_base`" type="url" inputmode="url" autocomplete="off" spellcheck="false" placeholder="例如：https://console.example…"></label>
              </div>
              <ul v-if="validation?.errors.some((errorItem) => errorItem.field.startsWith(`providers.${item.provider}`))" class="field-error-list">
                <li v-for="errorItem in validation.errors.filter((candidate) => candidate.field.startsWith(`providers.${item.provider}`))" :key="`${errorItem.field}-${errorItem.code}`"><code>{{ errorItem.code }}</code> {{ errorItem.message }}</li>
              </ul>
              <footer>
                <span v-if="providerResults[item.provider]" class="provider-result" :class="`is-${providerResults[item.provider]?.state}`">
                  {{ stateLabel(providerResults[item.provider]!.state) }} · {{ providerResults[item.provider]!.detail }}
                </span>
                <span v-else class="provider-result">尚未测试当前草稿</span>
                <button type="button" class="secondary-action" :disabled="testingProvider !== null" @click="runProviderTest(item.provider)">
                  <ShieldCheck :size="17" aria-hidden="true" />{{ testingProvider === item.provider ? "测试中…" : "测试连接" }}
                </button>
              </footer>
            </article>
          </div>
        </section>

        <section class="settings-section" aria-labelledby="secrets-heading">
          <header><div><p class="eyebrow">Write-only</p><h2 id="secrets-heading">Secret versions</h2></div></header>
          <div class="secret-editor">
            <label><span>Provider</span><select v-model="secretProvider" name="secret_provider" autocomplete="off"><option v-for="identity in providerOptions" :key="identity" :value="identity">{{ providerLabels[identity] }}</option></select></label>
            <label><span>Purpose</span><input v-model="secretPurpose" name="secret_purpose" type="text" autocomplete="off" spellcheck="false" pattern="[a-z][a-z0-9_]{0,63}" placeholder="例如：api_key…"></label>
            <label class="secret-value"><span>新 secret value</span><input v-model="secretValue" name="secret_value" type="password" autocomplete="off" spellcheck="false" placeholder="输入新 secret…"></label>
            <button type="button" class="primary-action" :disabled="!secretValue || savingSecret" @click="saveSecret"><KeyRound :size="17" aria-hidden="true" />{{ savingSecret ? "写入中…" : "写入 version" }}</button>
          </div>
          <ul v-if="draft.secret_references.length" class="secret-references">
            <li v-for="reference in draft.secret_references" :key="`${reference.provider}-${reference.purpose}`">
              <strong>{{ providerLabels[reference.provider] }} / {{ reference.purpose }}</strong>
              <span>{{ reference.state || "configured" }}</span>
              <code>{{ reference.fingerprint || reference.secret_version_id }}</code>
            </li>
          </ul>
          <p v-else class="empty-line">当前 Revision 没有 secret reference。</p>
        </section>

        <section class="settings-section validation-section" aria-labelledby="validation-heading">
          <header><div><p class="eyebrow">Review</p><h2 id="validation-heading">验证与发布检查</h2></div></header>
          <div v-if="!validation" class="empty-line">当前草稿尚未验证。</div>
          <template v-else>
            <div class="validation-identity" :class="validation.valid ? 'is-valid' : 'is-invalid'">
              <strong>{{ validation.valid ? "验证通过" : "验证未通过" }}</strong>
              <code :title="validation.draft_hash">{{ shortHash(validation.draft_hash) }}</code>
              <span v-if="validationStaleLocally">草稿在验证后已变化，发布将由后端执行 stale 校验。</span>
              <span v-else>当前草稿与本次验证一致。</span>
            </div>
            <ul v-if="validation.provider_results.length" class="validation-providers">
              <li v-for="result in validation.provider_results" :key="result.provider"><strong>{{ providerLabels[result.provider] }}</strong><span :class="`is-${result.state}`">{{ stateLabel(result.state) }}</span><p>{{ result.detail }}</p></li>
            </ul>
          </template>
        </section>
      </form>

      <section class="settings-section" aria-labelledby="history-heading">
        <header><div><p class="eyebrow">History</p><h2 id="history-heading">Configuration Revisions</h2></div></header>
        <div class="table-scroll" role="region" aria-label="Configuration Revision 历史" tabindex="0">
          <table>
            <thead><tr><th>Revision</th><th>状态</th><th>摘要</th><th>Hash</th><th>Worker</th><th>创建时间</th></tr></thead>
            <tbody><tr v-for="revision in settings.history" :key="revision.id"><td class="mono-text">#{{ revision.number }}</td><td>{{ revision.active ? "Active" : "Historical" }}</td><td>{{ revision.summary }}</td><td class="mono-text" :title="revision.hash">{{ shortHash(revision.hash) }}</td><td>{{ activationLabel(revision.worker_boundary?.status) }}</td><td><time :datetime="revision.created_at">{{ formatTime(revision.created_at) }}</time></td></tr></tbody>
          </table>
        </div>
      </section>

      <section class="settings-section" aria-labelledby="storage-heading">
        <header><div><p class="eyebrow">Durability</p><h2 id="storage-heading">存储与备份</h2></div></header>
        <dl v-if="storage" class="facts-grid">
          <div><dt>数据库表</dt><dd>{{ numberFormatter.format(storage.database_tables) }}</dd></div>
          <div><dt>配置 Revision</dt><dd>{{ numberFormatter.format(storage.configuration_count) }}</dd></div>
          <div><dt>通知记录</dt><dd>{{ numberFormatter.format(storage.notification_count) }}</dd></div>
          <div><dt>Secret versions</dt><dd>{{ numberFormatter.format(storage.secret_version_count) }}</dd></div>
          <div><dt>Data capacity</dt><dd>{{ formatBytes(storage.data_capacity_bytes) }}</dd></div>
          <div><dt>Data available</dt><dd>{{ formatBytes(storage.data_available_bytes) }}</dd></div>
          <div><dt>最近备份</dt><dd>{{ storage.latest_backup_name || "无记录" }}</dd></div>
          <div><dt>备份时间</dt><dd>{{ formatTime(storage.latest_backup_at) }}</dd></div>
        </dl>
      </section>

      <section class="settings-section" aria-labelledby="bootstrap-heading">
        <header><div><p class="eyebrow">Read-only</p><h2 id="bootstrap-heading">Bootstrap diagnostics</h2></div></header>
        <dl class="facts-grid facts-grid--bootstrap">
          <div><dt>Listen boundary</dt><dd class="mono-text">{{ settings.bootstrap.listen_boundary }}</dd></div>
          <div><dt>MySQL database</dt><dd class="mono-text">{{ settings.bootstrap.mysql_database }}</dd></div>
          <div><dt>Data directory</dt><dd class="mono-text">{{ settings.bootstrap.data_directory }}</dd></div>
          <div><dt>Worker target</dt><dd class="mono-text">{{ settings.bootstrap.worker_management_target }}</dd></div>
          <div><dt>Lifecycle</dt><dd class="mono-text">{{ settings.bootstrap.lifecycle }}</dd></div>
        </dl>
      </section>
    </template>
  </article>
</template>

<style scoped>
.settings-view { display: grid; gap: var(--co-space-6); width: min(100%, 1320px); margin: 0 auto; }
.page-heading, .settings-section > header, .revision-summary { display: flex; align-items: flex-end; justify-content: space-between; gap: var(--co-space-4); }
.page-heading h1, .settings-section h2, .revision-summary h2 { margin: 0; }
.page-heading h1 { font-size: 30px; }
.page-heading p:not(.eyebrow), .revision-summary p { margin: var(--co-space-2) 0 0; color: var(--co-text-secondary); }
.eyebrow { margin: 0 0 var(--co-space-2); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 11px; font-weight: 750; text-transform: uppercase; }
.primary-action, .secondary-action { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; gap: var(--co-space-2); padding: 0 var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); cursor: pointer; font-weight: 750; }
.secondary-action { color: var(--co-text-primary); background: var(--co-bg-surface); }
.primary-action { border-color: var(--co-action-primary); color: var(--co-text-on-action); background: var(--co-action-primary); }
.secondary-action:hover { border-color: var(--co-border-strong); background: var(--co-bg-hover); }
.primary-action:hover { border-color: var(--co-action-hover); background: var(--co-action-hover); }
button:disabled { cursor: not-allowed; opacity: 0.55; }
.action-bar { position: sticky; top: var(--co-header-height); z-index: var(--co-z-sticky); display: flex; min-height: 62px; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-2) var(--co-space-3); border-block: 1px solid var(--co-border-default); background: color-mix(in srgb, var(--co-bg-canvas) 96%, transparent); backdrop-filter: blur(10px); }
.action-bar > span { color: var(--co-text-muted); font-size: 12px; font-weight: 700; }
.action-bar > span.is-dirty { color: var(--co-status-warning-fg); }
.action-bar > div { display: flex; gap: var(--co-space-2); }
.message-band { display: flex; align-items: center; gap: var(--co-space-2); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); color: var(--co-text-secondary); background: var(--co-bg-surface); overflow-wrap: anywhere; }
.message-band.is-error { align-items: flex-start; flex-direction: column; border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.message-band.is-success { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.revision-summary { align-items: stretch; padding: var(--co-space-5); border-block: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.revision-summary > div { align-self: center; }
.revision-summary h2 { font-size: 20px; }
.revision-summary dl { display: grid; min-width: min(100%, 620px); grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; border-left: 1px solid var(--co-border-default); }
.revision-summary dl div { min-width: 0; padding: var(--co-space-3) var(--co-space-4); }
.revision-summary dt, .facts-grid dt { color: var(--co-text-muted); font-size: 11px; }
.revision-summary dd, .facts-grid dd { margin: 3px 0 0; overflow-wrap: anywhere; font-size: 12px; font-weight: 750; }
.activation-succeeded { color: var(--co-status-success-fg); }
.activation-failed { color: var(--co-status-critical-fg); }
#settings-form { display: grid; gap: var(--co-space-8); }
.settings-section { display: grid; gap: var(--co-space-4); padding-block: var(--co-space-5); border-block: 1px solid var(--co-border-default); }
.settings-section h2 { font-size: 20px; }
.scope-summary { max-width: 900px; }
.scope-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.scope-item { display: grid; min-width: 0; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.scope-item.is-default { border-color: var(--co-status-info-border); }
.scope-item > header { display: flex; min-height: 36px; align-items: center; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.scope-default { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); color: var(--co-text-primary); cursor: pointer; font-size: 12px; font-weight: 750; }
.active-scope-state { margin-left: auto; padding: 3px var(--co-space-2); border: 1px solid var(--co-status-success-border); border-radius: var(--co-radius-pill); color: var(--co-status-success-fg); background: var(--co-status-success-bg); font-size: 10px; font-weight: 750; }
.scope-remove { display: grid; width: 36px; height: 36px; flex: 0 0 36px; place-items: center; margin-left: auto; padding: 0; border: 1px solid transparent; border-radius: var(--co-radius-control); color: var(--co-text-muted); background: transparent; cursor: pointer; }
.active-scope-state + .scope-remove { margin-left: 0; }
.scope-remove:hover { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.form-grid label, .secret-editor label { display: grid; min-width: 0; gap: 6px; color: var(--co-text-secondary); font-size: 12px; font-weight: 700; }
.form-grid .span-two { grid-column: span 2; }
input, select, textarea { width: 100%; min-width: 0; min-height: 42px; padding: 0 var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); color: var(--co-text-primary); background: var(--co-bg-surface); }
textarea { padding-block: var(--co-space-2); resize: vertical; }
input:hover, select:hover, textarea:hover { border-color: var(--co-border-strong); }
input[type="checkbox"], input[type="radio"] { width: 18px; min-height: 18px; padding: 0; accent-color: var(--co-action-primary); }
.toggle-row { display: flex; flex-wrap: wrap; gap: var(--co-space-5); }
.toggle-row label, .provider-toggle { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); color: var(--co-text-secondary); cursor: pointer; font-size: 13px; font-weight: 700; }
.policy-revision { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-4); color: var(--co-text-muted); font-size: 12px; }
.policy-revision code { overflow-wrap: anywhere; color: var(--co-text-secondary); }
.policy-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.policy-item { display: grid; min-width: 0; align-content: start; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); }
.policy-item > header { display: flex; min-height: 36px; align-items: center; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.policy-toggle { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); color: var(--co-text-primary); cursor: pointer; font-size: 12px; font-weight: 750; }
.policy-action { margin-left: auto; padding: 3px var(--co-space-2); border: 1px solid var(--co-status-info-border); border-radius: var(--co-radius-pill); color: var(--co-status-info-fg); background: var(--co-status-info-bg); font-size: 10px; font-weight: 750; }
.policy-action + .scope-remove { margin-left: 0; }
.severity-picker { min-width: 0; margin: 0; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); }
.severity-picker legend { padding: 0 var(--co-space-1); color: var(--co-text-secondary); font-size: 12px; font-weight: 700; }
.severity-picker label { display: inline-flex; min-height: 44px; align-items: center; gap: var(--co-space-2); margin-right: var(--co-space-4); color: var(--co-text-secondary); cursor: pointer; font-family: var(--co-font-mono); font-size: 12px; }
.field-error-list { display: grid; gap: var(--co-space-1); margin: 0; padding: var(--co-space-3) var(--co-space-4); border: 1px solid var(--co-status-critical-border); border-radius: var(--co-radius-panel); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); list-style-position: inside; font-size: 12px; }
.field-error-list code { color: inherit; }
.provider-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.provider-item { display: grid; min-width: 0; align-content: start; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); content-visibility: auto; contain-intrinsic-size: 520px; }
.provider-item > header, .provider-item > footer { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.provider-item > header > div { display: grid; }
.provider-item > header span { color: var(--co-text-muted); font-size: 11px; }
.provider-item > footer { align-items: flex-end; margin-top: auto; padding-top: var(--co-space-3); border-top: 1px solid var(--co-border-default); }
.form-grid--provider { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.provider-result { min-width: 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 11px; }
.provider-result.is-available { color: var(--co-status-success-fg); }
.provider-result.is-unavailable, .provider-result.is-partial { color: var(--co-status-warning-fg); }
.secret-editor { display: grid; grid-template-columns: 1fr 1fr minmax(220px, 2fr) auto; align-items: end; gap: var(--co-space-3); }
.secret-references { display: grid; gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.secret-references li { display: grid; grid-template-columns: minmax(180px, 1fr) auto minmax(220px, 1fr); gap: var(--co-space-3); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.secret-references span { color: var(--co-status-success-fg); font-size: 12px; }
.secret-references code { min-width: 0; overflow: hidden; color: var(--co-text-muted); text-overflow: ellipsis; white-space: nowrap; }
.empty-line { margin: 0; padding: var(--co-space-5); border-block: 1px solid var(--co-border-default); color: var(--co-text-muted); text-align: center; }
.validation-identity { display: grid; grid-template-columns: auto auto minmax(0, 1fr); align-items: center; gap: var(--co-space-3); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); }
.validation-identity.is-valid { border-color: var(--co-status-success-border); color: var(--co-status-success-fg); background: var(--co-status-success-bg); }
.validation-identity.is-invalid { border-color: var(--co-status-critical-border); color: var(--co-status-critical-fg); background: var(--co-status-critical-bg); }
.validation-identity span { overflow-wrap: anywhere; font-size: 12px; }
.validation-providers { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-2); margin: 0; padding: 0; list-style: none; }
.validation-providers li { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: var(--co-space-2); padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); }
.validation-providers span { color: var(--co-text-muted); font-size: 11px; font-weight: 750; }
.validation-providers span.is-available { color: var(--co-status-success-fg); }
.validation-providers span.is-unavailable, .validation-providers span.is-partial { color: var(--co-status-warning-fg); }
.validation-providers p { grid-column: 1 / -1; margin: 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 11px; }
.table-scroll { overflow-x: auto; overscroll-behavior: contain; }
table { width: 100%; min-width: 860px; border-collapse: collapse; background: var(--co-bg-surface); font-size: 12px; }
th, td { padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); text-align: left; vertical-align: top; }
th { color: var(--co-text-muted); font-size: 11px; }
td { overflow-wrap: anywhere; }
.facts-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border: 1px solid var(--co-border-default); background: var(--co-bg-surface); }
.facts-grid div { min-width: 0; padding: var(--co-space-4); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.facts-grid--bootstrap { grid-template-columns: repeat(2, minmax(0, 1fr)); }
@media (max-width: 1050px) { .scope-list, .policy-list, .provider-list { grid-template-columns: 1fr; } .secret-editor { grid-template-columns: repeat(2, minmax(0, 1fr)); } .secret-value { grid-column: 1 / -1; } .facts-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 767px) { .settings-view { gap: var(--co-space-5); } .page-heading { align-items: flex-start; flex-direction: column; } .page-heading h1 { font-size: 25px; } .action-bar { align-items: stretch; flex-direction: column; } .action-bar > div { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); } .action-bar button { min-width: 0; padding-inline: var(--co-space-2); font-size: 11px; } .revision-summary { flex-direction: column; } .revision-summary dl { min-width: 0; border-top: 1px solid var(--co-border-default); border-left: 0; } .form-grid, .form-grid--three, .form-grid--provider { grid-template-columns: 1fr; } .form-grid .span-two { grid-column: auto; } .provider-item > footer { align-items: stretch; flex-direction: column; } .provider-item > footer button { width: 100%; } .secret-editor { grid-template-columns: 1fr; } .secret-value { grid-column: auto; } .secret-references li { grid-template-columns: 1fr; } .validation-identity { grid-template-columns: 1fr; } .validation-providers { grid-template-columns: 1fr; } }
@media (max-width: 420px) { .action-bar > div { grid-template-columns: 1fr; } .revision-summary dl, .facts-grid, .facts-grid--bootstrap { grid-template-columns: 1fr; } }
</style>
