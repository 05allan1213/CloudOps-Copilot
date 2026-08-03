<script setup lang="ts">
import type { FormError, TableColumn } from "@nuxt/ui";
import UAlert from "@nuxt/ui/components/Alert.vue";
import UBadge from "@nuxt/ui/components/Badge.vue";
import UButton from "@nuxt/ui/components/Button.vue";
import UCheckbox from "@nuxt/ui/components/Checkbox.vue";
import UForm from "@nuxt/ui/components/Form.vue";
import UFormField from "@nuxt/ui/components/FormField.vue";
import UInput from "@nuxt/ui/components/Input.vue";
import UInputNumber from "@nuxt/ui/components/InputNumber.vue";
import UModal from "@nuxt/ui/components/Modal.vue";
import USelect from "@nuxt/ui/components/Select.vue";
import USlideover from "@nuxt/ui/components/Slideover.vue";
import USwitch from "@nuxt/ui/components/Switch.vue";
import UTable from "@nuxt/ui/components/Table.vue";
import UTextarea from "@nuxt/ui/components/Textarea.vue";
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch, type Component } from "vue";
import { onBeforeRouteLeave, useRoute, useRouter } from "vue-router";

import { isApiError } from "../../api/client";
import {
  applyConfiguration,
  createSecret,
  getSettings,
  getStorageStatus,
  testProvider,
  validateSettings,
  type ConfigurationRevision,
  type ConfigurationValidation,
  type EscalationPolicy,
  type GeneralConfiguration,
  type ProviderConfiguration,
  type ProviderIdentity,
  type ProviderResult,
  type ProviderState,
  type SecretReference,
  type SettingsSnapshot,
  type StorageStatus,
} from "../../api/platform";
import WorkspaceTechnicalDetails from "../../components/workspace/WorkspaceTechnicalDetails.vue";
import { invalidateQueryDomain } from "../../composables/queryCache";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";
import SettingsSectionPanel from "./SettingsSectionPanel.vue";
import {
  SETTINGS_DRAFT_STORAGE_KEY,
  buildSectionConfigurationDraft,
  classifySettingsApplyOutcome,
  createPersistedSettingsDrafts,
  createSettingsSectionDraft,
  createSettingsSectionDrafts,
  isSettingsSectionDirty,
  parsePersistedSettingsDrafts,
  persistedSettingsDraftConflicts,
  rebaseSettingsSection,
  resetSettingsSection,
  restorePersistedSettingsDrafts,
  settingsSectionChanges,
  settingsSectionFingerprint,
  settingsSectionKeys,
  settingsSectionLabel,
  validateSettingsSectionLocally,
  type ScopeSectionValue,
  type SettingsApplyOutcome,
  type SettingsDraftRecovery,
  type SettingsSectionDraft,
  type SettingsSectionDrafts,
  type SettingsSectionKey,
} from "./settingsDraft";
import {
  filterSettingsSearchEntries,
  resolveSettingsViewSection,
  shouldBlockSettingsLeave,
  type SettingsSearchEntry,
  type SettingsViewSection,
} from "./settingsWorkspace";

interface SectionRuntime {
  validation: ConfigurationValidation | null;
  validatedFingerprint: string;
  validating: boolean;
  applying: boolean;
  error: string;
  outcome: SettingsApplyOutcome | null;
  outcomeRevisionID: string;
}

interface RevisionRow {
  revision: string;
  state: string;
  summary: string;
  hash: string;
  worker: string;
  createdAt: string;
}

interface SettingsSectionLink {
  key: SettingsViewSection;
  label: string;
  hash: string;
  icon: string;
  description: string;
}

interface SettingsSectionGroup {
  label: string;
  items: SettingsSectionLink[];
}

interface SettingsRecoveryDiff {
  key: SettingsSectionKey;
  label: string;
  baseRevisionNumber: number;
  summary: string;
  changes: string[];
}

type SettingsSearchResult = SettingsSearchEntry;

const route = useRoute();
const router = useRouter();
const settings = ref<SettingsSnapshot | null>(null);
const storage = ref<StorageStatus | null>(null);
const drafts = ref<SettingsSectionDrafts | null>(null);
const loading = ref(true);
const loadError = ref("");
const refreshMessage = ref("");
const providerResults = ref<Partial<Record<ProviderIdentity, ProviderResult>>>({});
const providerTestOpen = ref(false);
const providerTestTarget = ref<ProviderIdentity | null>(null);
const providerTesting = ref(false);
const providerTestError = ref("");
const secretModalOpen = ref(false);
const secretSaving = ref(false);
const secretError = ref("");
const secretState = reactive({ provider: "llm" as ProviderIdentity, purpose: "api_key", value: "" });
const applyConfirmationOpen = ref(false);
const pendingApplySection = ref<SettingsSectionKey | null>(null);
const leaveModalOpen = ref(false);
const pendingRoute = ref("");
const advancedSettingsOpen = ref(false);
const settingsSearch = ref("");
const selectedProvider = ref<ProviderIdentity | null>(null);
const providerEditorOpen = ref(false);
const selectedScopeIndex = ref(0);
const selectedPolicyIndex = ref(0);
const draftRecovery = ref<SettingsDraftRecovery | null>(null);
const draftRecoveryOpen = ref(false);
const draftRecoveryDiffVisible = ref(false);
const draftPersistenceError = ref("");
const settingsNavElement = ref<HTMLElement | null>(null);
const settingsNavIndicatorStyle = ref<Record<string, string>>({ opacity: "0" });
const providerSlideoverUI = {
  overlay: "settings-provider-overlay",
  content: "settings-provider-slideover",
  header: "settings-provider-slideover__header",
  body: "settings-provider-slideover__body",
  footer: "settings-provider-slideover__footer",
};
const providerTestModalUI = {
  overlay: "settings-provider-test-overlay",
  content: "settings-provider-test-modal",
};
let mounted = true;
let draftRecoveryChecked = false;
let draftPersistenceReady = false;
let allowNextSettingsLeave = false;
let draftPersistenceTimer: ReturnType<typeof setTimeout> | undefined;

function emptyRuntime(): SectionRuntime {
  return {
    validation: null,
    validatedFingerprint: "",
    validating: false,
    applying: false,
    error: "",
    outcome: null,
    outcomeRevisionID: "",
  };
}

const runtimes = reactive(Object.fromEntries(
  settingsSectionKeys.map((key) => [key, emptyRuntime()]),
) as Record<SettingsSectionKey, SectionRuntime>);

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
const providerIcons: Record<ProviderIdentity, string> = {
  llm: "i-lucide-sparkles",
  kubernetes: "i-lucide-box",
  prometheus: "i-lucide-chart-no-axes-combined",
  alertmanager: "i-lucide-bell-ring",
  elasticsearch: "i-lucide-file-search",
  tempo: "i-lucide-git-branch",
  github: "i-lucide-github",
  argocd: "i-lucide-git-pull-request-arrow",
};
const providerDescriptions: Record<ProviderIdentity, string> = {
  llm: "模型、Tool Calling 与结构化输出连接",
  kubernetes: "集群资源读取与受控操作边界",
  prometheus: "指标查询、Label 过滤与 Explore 跳转",
  alertmanager: "告警读取、Silence 与 Receiver 上下文",
  elasticsearch: "日志检索、结果边界与 Discover 跳转",
  tempo: "Trace 搜索、链路读取与详情跳转",
  github: "仓库、分支策略与 GitHub App 访问",
  argocd: "Project、Application 与 Sync 权限",
};
const providerEditorComponents: Record<ProviderIdentity, Component> = {
  llm: defineAsyncComponent(() => import("../../components/settings/LLMProviderSettings.vue")),
  kubernetes: defineAsyncComponent(() => import("../../components/settings/KubernetesProviderSettings.vue")),
  prometheus: defineAsyncComponent(() => import("../../components/settings/PrometheusProviderSettings.vue")),
  alertmanager: defineAsyncComponent(() => import("../../components/settings/AlertmanagerProviderSettings.vue")),
  elasticsearch: defineAsyncComponent(() => import("../../components/settings/ElasticsearchProviderSettings.vue")),
  tempo: defineAsyncComponent(() => import("../../components/settings/TempoProviderSettings.vue")),
  github: defineAsyncComponent(() => import("../../components/settings/GitHubProviderSettings.vue")),
  argocd: defineAsyncComponent(() => import("../../components/settings/ArgoCDProviderSettings.vue")),
};
const providerOptions = (Object.keys(providerLabels) as Array<ProviderIdentity | "mysql">)
  .filter((provider): provider is ProviderIdentity => provider !== "mysql")
  .map((provider) => ({ label: providerLabels[provider], value: provider }));
const severityOptions: EscalationPolicy["severities"] = ["critical", "warning", "info", "unknown"];
const sectionLinks: SettingsSectionLink[] = [
  { key: "system", label: "系统", hash: "#system", icon: "i-lucide-gauge", description: "查询边界与通知" },
  { key: "scopes", label: "运行范围", hash: "#operational-scope", icon: "i-lucide-scan-search", description: "Cluster Scope" },
  { key: "policies", label: "升级策略", hash: "#escalation-policies", icon: "i-lucide-bell-ring", description: "Alert escalation" },
  { key: "providers", label: "Provider", hash: "#providers", icon: "i-lucide-plug-zap", description: "连接摘要与详情" },
  { key: "secret-references", label: "Secret 引用", hash: "#secret-references", icon: "i-lucide-key-round", description: "Write-only references" },
  { key: "revisions", label: "Revision 历史", hash: "#revision-history", icon: "i-lucide-history", description: "只读版本与存储" },
];

const sectionGroups: SettingsSectionGroup[] = [
  { label: "边界与行为", items: sectionLinks.slice(0, 3) },
  { label: "连接与凭据", items: sectionLinks.slice(3, 5) },
  { label: "审计", items: sectionLinks.slice(5) },
];

const activeRevision = computed(() => settings.value?.active_revision ?? null);
const systemValue = computed(() => drafts.value?.system.value as GeneralConfiguration | undefined);
const scopesValue = computed(() => drafts.value?.scopes.value as ScopeSectionValue | undefined);
const policiesValue = computed(() => drafts.value?.policies.value as EscalationPolicy[] | undefined);
const providersValue = computed(() => drafts.value?.providers.value as ProviderConfiguration[] | undefined);
const secretReferencesValue = computed(() => drafts.value?.["secret-references"].value as SecretReference[] | undefined);
const selectedScope = computed(() => (
  scopesValue.value?.scopes[Math.min(selectedScopeIndex.value, Math.max(0, scopesValue.value.scopes.length - 1))]
));
const selectedPolicy = computed(() => (
  policiesValue.value?.[Math.min(selectedPolicyIndex.value, Math.max(0, policiesValue.value.length - 1))]
));
const hasUnsavedChanges = computed(() => Boolean(
  drafts.value && settingsSectionKeys.some((key) => isSettingsSectionDirty(drafts.value![key])),
));
const dirtySectionCount = computed(() => (
  drafts.value ? settingsSectionKeys.filter((key) => isSettingsSectionDirty(drafts.value![key])).length : 0
));
const activeSectionKey = computed<SettingsViewSection>(() => (
  resolveSettingsViewSection(route.hash)
));
const activeSectionLink = computed(() => (
  sectionLinks.find((item) => item.key === activeSectionKey.value) ?? sectionLinks[0]
));
const activeEditableSectionKey = computed<SettingsSectionKey | null>(() => (
  activeSectionKey.value === "revisions" ? null : activeSectionKey.value
));
const activeSectionDraft = computed<SettingsSectionDraft | null>(() => {
  const key = activeEditableSectionKey.value;
  return key && drafts.value ? drafts.value[key] : null;
});
const activeSectionRuntime = computed<SectionRuntime | null>(() => {
  const key = activeEditableSectionKey.value;
  return key ? runtimes[key] : null;
});
const activeSectionChanges = computed(() => (
  activeSectionDraft.value ? settingsSectionChanges(activeSectionDraft.value) : []
));
const activeSectionRevisionDrift = computed(() => Boolean(
  activeSectionDraft.value
  && activeRevision.value
  && (activeSectionDraft.value.baseRevisionID !== activeRevision.value.id
    || activeSectionDraft.value.baseRevisionHash !== activeRevision.value.hash),
));
const activeSectionValidationStale = computed(() => {
  const section = activeSectionDraft.value;
  const runtime = activeSectionRuntime.value;
  if (!section || !runtime?.validation) return true;
  const expiresAt = Date.parse(runtime.validation.expires_at);
  return runtime.validatedFingerprint !== settingsSectionFingerprint(section)
    || activeSectionRevisionDrift.value
    || (Number.isFinite(expiresAt) && expiresAt <= Date.now());
});
const activeSectionCanValidate = computed(() => Boolean(
  activeSectionDraft.value
  && activeSectionChanges.value.length
  && !activeSectionRevisionDrift.value
  && !activeSectionRuntime.value?.validating
  && !activeSectionRuntime.value?.applying,
));
const activeSectionCanApply = computed(() => Boolean(
  activeSectionCanValidate.value
  && activeSectionRuntime.value?.validation?.valid
  && !activeSectionValidationStale.value,
));
const activeSectionIsDirty = computed(() => Boolean(
  activeSectionDraft.value && isSettingsSectionDirty(activeSectionDraft.value),
));
const draftRecoveryConflicts = computed(() => Boolean(
  draftRecovery.value
  && activeRevision.value
  && persistedSettingsDraftConflicts(draftRecovery.value.payload, activeRevision.value),
));
const draftRecoveryDiffs = computed<SettingsRecoveryDiff[]>(() => {
  const sections = draftRecovery.value?.payload.sections;
  if (!sections) return [];
  return settingsSectionKeys.flatMap((key) => {
    const section = sections[key];
    if (!section) return [];
    const changes = settingsSectionChanges(section);
    if (!changes.length && section.summary.trim()) changes.push(`发布摘要：${section.summary.trim()}`);
    return [{
      key,
      label: settingsSectionLabel(key),
      baseRevisionNumber: section.baseRevisionNumber,
      summary: section.summary,
      changes,
    }];
  });
});
const activeSectionStatus = computed(() => {
  if (!activeSectionDraft.value || !activeSectionRuntime.value) return "";
  if (activeSectionRuntime.value.applying) return "正在应用配置";
  if (activeSectionRuntime.value.validating) return "正在验证配置";
  if (activeSectionCanApply.value) return `验证通过 · ${activeSectionChanges.value.length} 项变更`;
  if (activeSectionRuntime.value.validation && !activeSectionRuntime.value.validation.valid) return "验证未通过";
  return `已修改 ${activeSectionChanges.value.length} 项`;
});
const providerHealthByID = computed(() => new Map(
  (settings.value?.provider_health ?? []).map((item) => [item.provider, item]),
));
const selectedProviderConfiguration = computed<ProviderConfiguration | undefined>({
  get: () => providersValue.value?.find((item) => item.provider === selectedProvider.value) ?? providersValue.value?.[0],
  set: (configuration) => {
    if (!configuration || !providersValue.value) return;
    const index = providersValue.value.findIndex((item) => item.provider === configuration.provider);
    if (index >= 0) providersValue.value.splice(index, 1, configuration);
  },
});
const selectedProviderEditor = computed(() => (
  selectedProviderConfiguration.value ? providerEditorComponents[selectedProviderConfiguration.value.provider] : null
));
const sectionErrorCounts = computed<Record<SettingsViewSection, number>>(() => {
  const counts = Object.fromEntries(sectionLinks.map((item) => [item.key, 0])) as Record<SettingsViewSection, number>;
  if (!drafts.value) return counts;
  for (const key of settingsSectionKeys) {
    counts[key] = isSettingsSectionDirty(drafts.value[key])
      ? sectionFormErrors(key).length + (runtimes[key].validation?.errors.length ?? 0)
      : (runtimes[key].validation?.errors.length ?? 0);
  }
  return counts;
});
const searchResults = computed<SettingsSearchResult[]>(() => {
  const query = settingsSearch.value.trim().toLowerCase();
  if (!query) return [];
  const results: SettingsSearchResult[] = [];
  for (const item of sectionLinks) {
    const haystack = `${item.label} ${item.description}`.toLowerCase();
    if (haystack.includes(query)) results.push({ key: item.key, label: item.label, text: item.description });
  }
  const fields: SettingsSearchResult[] = [
    { key: "system", label: "最大回看时间", field: "system.query_max_lookback_seconds", text: "查询边界与时间窗口" },
    { key: "system", label: "查询结果上限", field: "system.query_max_results", text: "单次查询返回条数" },
    { key: "system", label: "Telemetry 保留", field: "system.telemetry_retention_days", text: "数据保留天数" },
    { key: "system", label: "浏览器提醒", field: "system.browser_notifications_enabled", text: "Owner 浏览器通知权限" },
    { key: "system", label: "自动 escalation", field: "system.automatic_escalation_enabled", text: "服务端升级行为" },
  ];

  scopesValue.value?.scopes.forEach((scope, index) => fields.push({
    key: "scopes",
    label: scope.name || `Scope ${index + 1}`,
    field: `scopes.${index}.name`,
    text: `${scope.cluster_id || "未设置 Cluster"} · ${scope.environment || "未设置环境"}`,
  }));
  policiesValue.value?.forEach((policy, index) => fields.push({
    key: "policies",
    label: policy.name || `Policy ${index + 1}`,
    field: `policies.${index}.name`,
    text: `${policy.severities.join(", ") || "未选择 Severity"} · ${policy.enabled ? "已启用" : "已停用"}`,
  }));
  providersValue.value?.forEach((provider) => fields.push({
    key: "providers",
    label: providerLabels[provider.provider],
    field: `providers.${provider.provider}.endpoint`,
    text: `${provider.endpoint || "服务端默认 Endpoint"} · ${provider.enabled ? "已启用" : "已停用"}`,
  }));
  secretReferencesValue.value?.forEach((reference) => fields.push({
    key: "secret-references",
    label: `${providerLabels[reference.provider]} / ${reference.purpose}`,
    field: "secret-references",
    text: "Write-only Secret version reference",
  }));
  return results.concat(filterSettingsSearchEntries(query, fields));
});
const providerTestConfiguration = computed(() => (
  providersValue.value?.find((item) => item.provider === providerTestTarget.value) ?? null
));
const pendingApplyDraft = computed(() => (
  pendingApplySection.value && drafts.value ? drafts.value[pendingApplySection.value] : null
));
const revisionColumns: TableColumn<RevisionRow>[] = [
  { accessorKey: "revision", header: "Revision" },
  { accessorKey: "state", header: "状态" },
  { accessorKey: "summary", header: "摘要" },
  { accessorKey: "hash", header: "Hash" },
  { accessorKey: "worker", header: "Worker" },
  { accessorKey: "createdAt", header: "Created at (UTC)" },
];
const revisionRows = computed<RevisionRow[]>(() => (settings.value?.history ?? []).map((revision) => ({
  revision: `#${revision.number}`,
  state: revision.active ? "Active" : "Historical",
  summary: revision.summary,
  hash: revision.hash,
  worker: activationLabel(revision.worker_boundary?.status),
  createdAt: formatISO(revision.created_at),
})));

function stateLabel(state: ProviderState): string {
  return ({
    available: "可用",
    partial: "部分可用",
    unavailable: "不可用",
    disabled: "已停用",
    not_configured: "未配置",
  } satisfies Record<ProviderState, string>)[state];
}

function stateColor(state: ProviderState): "success" | "warning" | "error" | "neutral" {
  if (state === "available") return "success";
  if (state === "partial") return "warning";
  if (state === "unavailable") return "error";
  return "neutral";
}

function activationLabel(status?: string): string {
  return ({
    ready: "等待 Worker",
    running: "Worker 应用中",
    succeeded: "Worker 已报告成功",
    failed: "Worker 应用失败",
  } as Record<string, string>)[status ?? ""] ?? "暂无状态";
}

function formatISO(value?: string): string {
  if (!value) return "无";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
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

function selectSection(key: SettingsViewSection) {
  providerEditorOpen.value = false;
  advancedSettingsOpen.value = false;
  const link = sectionLinks.find((item) => item.key === key);
  if (link) void router.push({ path: "/settings", hash: link.hash });
}

async function updateSettingsNavIndicator() {
  await nextTick();
  const navigation = settingsNavElement.value;
  const active = navigation?.querySelector<HTMLElement>(`[data-settings-section="${activeSectionKey.value}"]`);
  if (!navigation || !active) {
    settingsNavIndicatorStyle.value = { opacity: "0" };
    return;
  }
  const navigationRect = navigation.getBoundingClientRect();
  const activeRect = active.getBoundingClientRect();
  settingsNavIndicatorStyle.value = {
    height: `${activeRect.height}px`,
    opacity: "1",
    transform: `translate3d(0, ${activeRect.top - navigationRect.top}px, 0)`,
  };
}

function openProviderEditor(provider: ProviderIdentity) {
  selectedProvider.value = provider;
  providerEditorOpen.value = true;
}

function closeProviderEditor(value: boolean) {
  providerEditorOpen.value = value;
}

function updateSelectedProviderConfiguration(configuration: ProviderConfiguration) {
  selectedProviderConfiguration.value = configuration;
}

function sectionInventory(key: SettingsViewSection): string {
  if (key === "system") return "5 项独立设置";
  if (key === "scopes") return `${scopesValue.value?.scopes.length ?? 0} 个 Cluster Scope`;
  if (key === "policies") return `${policiesValue.value?.length ?? 0} 条 escalation policy`;
  if (key === "providers") return `${providersValue.value?.length ?? 0} 个 Provider`;
  if (key === "secret-references") return `${secretReferencesValue.value?.length ?? 0} 个 write-only reference`;
  return `${revisionRows.value.length} 个只读 Revision`;
}

function jumpToSearchResult(result: SettingsSearchResult) {
  settingsSearch.value = "";
  selectSection(result.key);
  void nextTick(() => {
    if (result.field) focusField(result.field);
    else void focusRouteAnchor(sectionLinks.find((item) => item.key === result.key)?.hash);
  });
}

function providerState(provider: ProviderIdentity): ProviderState {
  return providerHealthByID.value.get(provider)?.state ?? "not_configured";
}

function providerSecretCount(provider: ProviderIdentity): number {
  return secretReferencesValue.value?.filter((item) => item.provider === provider).length ?? 0;
}

function providerCheckedAt(provider: ProviderIdentity): string {
  const checkedAt = providerHealthByID.value.get(provider)?.checked_at;
  return checkedAt ? formatISO(checkedAt) : "尚未检查";
}

function secondsToHours(value: number): number {
  return Math.max(1, Math.round(value / 3600));
}

function hoursToSeconds(value: unknown): number {
  return Math.max(60, Number(value || 1) * 3600);
}

function secondsToMinutes(value: number): number {
  return Math.max(0, Math.round(value / 60));
}

function minutesToSeconds(value: unknown): number {
  return Math.max(0, Number(value || 0) * 60);
}

function describeError(reason: unknown, fallback: string): string {
  if (!isApiError(reason)) return fallback;
  const next = reason.nextSteps.length ? `；下一步：${reason.nextSteps.join("；")}` : "";
  return `${reason.code || "REQUEST_FAILED"}：${reason.message}${next}`;
}

function clearSectionRuntime(key: SettingsSectionKey, keepOutcome = false) {
  const outcome = keepOutcome ? runtimes[key].outcome : null;
  const outcomeRevisionID = keepOutcome ? runtimes[key].outcomeRevisionID : "";
  Object.assign(runtimes[key], emptyRuntime(), { outcome, outcomeRevisionID });
}

function initializeDrafts(revision: ConfigurationRevision) {
  drafts.value = createSettingsSectionDrafts(revision);
  selectedProvider.value = revision.providers[0]?.provider ?? null;
  selectedScopeIndex.value = Math.max(0, (revision.scopes?.length ? revision.scopes : [revision.scope]).findIndex((scope) => scope.active));
  selectedPolicyIndex.value = 0;
  for (const key of settingsSectionKeys) clearSectionRuntime(key);
  providerResults.value = {};
}

function readPersistedDraftRecovery(revision: ConfigurationRevision) {
  if (draftRecoveryChecked) return;
  draftRecoveryChecked = true;
  try {
    const raw = window.localStorage.getItem(SETTINGS_DRAFT_STORAGE_KEY);
    const recovery = parsePersistedSettingsDrafts(raw);
    if (!recovery) {
      if (raw) window.localStorage.removeItem(SETTINGS_DRAFT_STORAGE_KEY);
      draftPersistenceReady = true;
      return;
    }
    draftRecovery.value = recovery;
    draftRecoveryOpen.value = true;
    draftRecoveryDiffVisible.value = false;
    if (persistedSettingsDraftConflicts(recovery.payload, revision)) {
      refreshMessage.value = "检测到活动 Revision 已变化；恢复后必须逐 section 处理冲突，页面不会自动 rebase 或 apply。";
    }
  } catch {
    draftPersistenceReady = true;
    draftPersistenceError.value = "DRAFT_STORAGE_UNAVAILABLE：浏览器未开放本地草稿存储；当前修改仍只保留在页面会话中。";
  }
}

function persistSettingsDraftsNow(): boolean {
  if (!draftPersistenceReady || !drafts.value) return false;
  if (draftPersistenceTimer !== undefined) {
    clearTimeout(draftPersistenceTimer);
    draftPersistenceTimer = undefined;
  }
  try {
    const payload = createPersistedSettingsDrafts(drafts.value);
    if (Object.keys(payload.sections).length) {
      window.localStorage.setItem(SETTINGS_DRAFT_STORAGE_KEY, JSON.stringify(payload));
    } else {
      window.localStorage.removeItem(SETTINGS_DRAFT_STORAGE_KEY);
    }
    draftPersistenceError.value = "";
    return true;
  } catch {
    draftPersistenceError.value = "DRAFT_STORAGE_UNAVAILABLE：24 小时草稿保存失败；请继续留在本页或放弃修改。";
    return false;
  }
}

function scheduleSettingsDraftPersistence() {
  if (!draftPersistenceReady || !drafts.value) return;
  if (draftPersistenceTimer !== undefined) clearTimeout(draftPersistenceTimer);
  draftPersistenceTimer = setTimeout(() => {
    draftPersistenceTimer = undefined;
    persistSettingsDraftsNow();
  }, 250);
}

function discardPersistedDraftRecovery() {
  try {
    window.localStorage.removeItem(SETTINGS_DRAFT_STORAGE_KEY);
    draftPersistenceError.value = "";
  } catch {
    draftPersistenceError.value = "DRAFT_STORAGE_UNAVAILABLE：无法清理浏览器中的草稿记录。";
    return;
  }
  draftRecovery.value = null;
  draftRecoveryOpen.value = false;
  draftRecoveryDiffVisible.value = false;
  draftPersistenceReady = true;
}

function restorePersistedDraftRecovery() {
  const recovery = draftRecovery.value;
  if (!recovery || recovery.status !== "fresh" || !activeRevision.value) return;
  const conflicts = persistedSettingsDraftConflicts(recovery.payload, activeRevision.value);
  drafts.value = restorePersistedSettingsDrafts(activeRevision.value, recovery.payload);
  selectedProvider.value = (drafts.value.providers.value as ProviderConfiguration[])[0]?.provider ?? null;
  selectedScopeIndex.value = 0;
  selectedPolicyIndex.value = 0;
  for (const key of settingsSectionKeys) clearSectionRuntime(key);
  providerResults.value = {};
  draftRecovery.value = null;
  draftRecoveryOpen.value = false;
  draftRecoveryDiffVisible.value = false;
  draftPersistenceReady = true;
  persistSettingsDraftsNow();
  refreshMessage.value = conflicts
    ? "草稿已按原始 base Revision 恢复；冲突 section 必须明确放弃或 rebase 后重新验证。"
    : "非敏感 Settings 草稿已恢复；Secret value、Token 与 apply 状态未被持久化。";
}

function reconcileCleanDrafts(revision: ConfigurationRevision) {
  if (!drafts.value) {
    initializeDrafts(revision);
    return;
  }
  if (!selectedProvider.value) selectedProvider.value = revision.providers[0]?.provider ?? null;
  for (const key of settingsSectionKeys) {
    if (isSettingsSectionDirty(drafts.value[key])) continue;
    drafts.value[key] = createSettingsSectionDraft(revision, key);
    clearSectionRuntime(key, true);
  }
}

async function loadSettings(initial = false, force = false) {
  if (force) invalidateQueryDomain("platform");
  loading.value = true;
  loadError.value = "";
  try {
    const [snapshot, storageStatus] = await Promise.all([getSettings(), getStorageStatus()]);
    if (!mounted) return;
    settings.value = snapshot;
    storage.value = storageStatus;
    if (initial || !drafts.value) initializeDrafts(snapshot.active_revision);
    else reconcileCleanDrafts(snapshot.active_revision);
    readPersistedDraftRecovery(snapshot.active_revision);
    await focusRouteAnchor();
  } catch (reason) {
    loadError.value = describeError(reason, "Settings 读取失败，请检查本地 API 与持久化状态。");
  } finally {
    loading.value = false;
  }
}

function setSectionSummary(key: SettingsSectionKey, value: string) {
  if (drafts.value) drafts.value[key].summary = value;
}

function resetSection(key: SettingsSectionKey) {
  if (!drafts.value) return;
  drafts.value[key] = resetSettingsSection(drafts.value[key]);
  clearSectionRuntime(key, true);
  refreshMessage.value = `${settingsSectionLabel(key)}已恢复为 base Revision #${drafts.value[key].baseRevisionNumber}。`;
}

function rebaseSection(key: SettingsSectionKey, preserveLocalValue: boolean) {
  if (!drafts.value || !activeRevision.value) return;
  drafts.value[key] = rebaseSettingsSection(drafts.value[key], activeRevision.value, preserveLocalValue);
  clearSectionRuntime(key, true);
  refreshMessage.value = preserveLocalValue
    ? `${settingsSectionLabel(key)}已保留本地值并 rebase 到 Revision #${activeRevision.value.number}；请重新核对和验证。`
    : `${settingsSectionLabel(key)}已放弃本地值并刷新到 Revision #${activeRevision.value.number}。`;
}

function sectionFormErrors(key: SettingsSectionKey): FormError[] {
  if (!drafts.value) return [];
  return validateSettingsSectionLocally(drafts.value[key]).map((item) => ({
    name: item.name,
    message: item.message,
  }));
}

function selectorForField(field: string): string {
  const escaped = typeof CSS !== "undefined" && CSS.escape ? CSS.escape(field) : field.replace(/["\\]/g, "\\$&");
  return `[data-field="${escaped}"]`;
}

async function focusFirstError(errors: FormError[]) {
  const first = errors[0]?.name;
  if (!first) return;
  await nextTick();
  focusField(first);
}

function focusField(field: string) {
  const container = document.querySelector<HTMLElement>(selectorForField(field));
  const control = container?.matches("input, textarea, button, [tabindex]")
    ? container
    : container?.querySelector<HTMLElement>("input, textarea, button, [tabindex]");
  control?.focus();
}

function serverFieldTarget(key: SettingsSectionKey, field: string): string {
  if (field === "summary") return `${key}.summary`;
  if (key === "system" && field.startsWith("general.")) return `system.${field.slice("general.".length)}`;
  if (key === "scopes" && field === "scope") return `scopes.${scopesValue.value?.defaultIndex ?? 0}.cluster_id`;
  if (key === "scopes" && field.startsWith("scope.")) return `scopes.${scopesValue.value?.defaultIndex ?? 0}.${field.slice("scope.".length)}`;
  if (key === "policies" && field.startsWith("escalation_policies.")) return `policies.${field.slice("escalation_policies.".length)}`;
  return field;
}

async function validateSection(key: SettingsSectionKey) {
  if (!drafts.value || runtimes[key].validating || runtimes[key].applying) return;
  const section = drafts.value[key];
  const localErrors = sectionFormErrors(key);
  if (localErrors.length) {
    await focusFirstError(localErrors);
    return;
  }
  runtimes[key].validating = true;
  runtimes[key].error = "";
  try {
    const result = await validateSettings(buildSectionConfigurationDraft(section));
    runtimes[key].validation = result;
    runtimes[key].validatedFingerprint = settingsSectionFingerprint(section);
    if (!result.valid && result.errors[0]) {
      await nextTick();
      focusField(serverFieldTarget(key, result.errors[0].field));
    }
  } catch (reason) {
    runtimes[key].error = describeError(reason, `${settingsSectionLabel(key)}验证失败。`);
  } finally {
    runtimes[key].validating = false;
  }
}

function requestApply(key: SettingsSectionKey) {
  pendingApplySection.value = key;
  applyConfirmationOpen.value = true;
}

async function confirmApply() {
  const key = pendingApplySection.value;
  applyConfirmationOpen.value = false;
  pendingApplySection.value = null;
  if (key) await applySection(key);
}

async function applySection(key: SettingsSectionKey) {
  if (!drafts.value || runtimes[key].applying) return;
  const section = drafts.value[key];
  const validation = runtimes[key].validation;
  const expiresAt = validation ? new Date(validation.expires_at).getTime() : 0;
  if (!validation?.valid
    || runtimes[key].validatedFingerprint !== settingsSectionFingerprint(section)
    || (Number.isFinite(expiresAt) && expiresAt <= Date.now())) {
    runtimes[key].error = "VALIDATION_STALE：本区草稿必须使用当前内容重新验证。";
    return;
  }

  runtimes[key].applying = true;
  runtimes[key].error = "";
  try {
    let preflight: SettingsSnapshot;
    try {
      invalidateQueryDomain("platform");
      preflight = await getSettings();
    } catch (reason) {
      runtimes[key].error = describeError(reason, "CONCURRENT_REVISION_PREFLIGHT_FAILED：无法读取当前 Revision，apply 保持阻止。 ");
      return;
    }
    settings.value = preflight;
    if (preflight.active_revision.id !== section.baseRevisionID
      || preflight.active_revision.hash !== section.baseRevisionHash) {
      runtimes[key].error = `REVISION_CONFLICT：base #${section.baseRevisionNumber} 与活动 Revision #${preflight.active_revision.number} 不一致。请选择放弃或 rebase。`;
      return;
    }

    const applied = await applyConfiguration(validation.id, buildSectionConfigurationDraft(section), {
      id: section.baseRevisionID,
      hash: section.baseRevisionHash,
    });
    runtimes[key].outcomeRevisionID = applied.id;
    runtimes[key].outcome = classifySettingsApplyOutcome(applied, []);

    let observed: SettingsSnapshot | null = null;
    try {
      observed = await getSettings();
    } catch {
      // The apply response remains authoritative for acceptance; observation can be retried read-only.
    }
    const nextSnapshot = observed?.history.some((revision) => revision.id === applied.id)
      ? observed
      : {
          ...preflight,
          active_revision: applied,
          history: [applied, ...preflight.history.filter((revision) => revision.id !== applied.id)],
        };
    settings.value = nextSnapshot;
    const observedRevision = nextSnapshot.history.find((revision) => revision.id === applied.id) ?? applied;
    runtimes[key].outcome = classifySettingsApplyOutcome(observedRevision, nextSnapshot.provider_health);

    const previous = drafts.value;
    for (const sectionKey of settingsSectionKeys) {
      if (sectionKey === key || !isSettingsSectionDirty(previous[sectionKey])) {
        previous[sectionKey] = createSettingsSectionDraft(nextSnapshot.active_revision, sectionKey);
        clearSectionRuntime(sectionKey, sectionKey === key);
      }
    }
    window.dispatchEvent(new Event("cloudops:configuration-applied"));
    try {
      storage.value = await getStorageStatus();
    } catch {
      // Revision acceptance is not downgraded by a read-only storage refresh failure.
    }
  } catch (reason) {
    runtimes[key].error = describeError(reason, `${settingsSectionLabel(key)} apply 失败。`);
    try {
      invalidateQueryDomain("platform");
      const snapshot = await getSettings();
      settings.value = snapshot;
    } catch {
      // Preserve the primary apply error.
    }
  } finally {
    runtimes[key].applying = false;
  }
}

async function refreshApplyOutcome(key: SettingsSectionKey) {
  const revisionID = runtimes[key].outcomeRevisionID;
  if (!revisionID) return;
  runtimes[key].error = "";
  try {
    invalidateQueryDomain("platform");
    const snapshot = await getSettings();
    settings.value = snapshot;
    reconcileCleanDrafts(snapshot.active_revision);
    const revision = snapshot.history.find((item) => item.id === revisionID);
    if (!revision) {
      runtimes[key].error = `REVISION_NOT_OBSERVED：历史投影尚未返回 ${revisionID}；未重放 apply。`;
      return;
    }
    runtimes[key].outcome = classifySettingsApplyOutcome(revision, snapshot.provider_health);
  } catch (reason) {
    runtimes[key].error = describeError(reason, "Revision 观测刷新失败；未重放 apply。 ");
  }
}

function addScope() {
  if (!scopesValue.value || scopesValue.value.scopes.length >= 10) return;
  const current = scopesValue.value.scopes[scopesValue.value.defaultIndex] ?? scopesValue.value.scopes[0];
  scopesValue.value.scopes.push({
    name: "",
    cluster_id: "",
    environment: current?.environment || "local",
    namespaces: [...(current?.namespaces ?? [])],
    active: false,
  });
  selectedScopeIndex.value = scopesValue.value.scopes.length - 1;
  void nextTick(() => focusField(`scopes.${scopesValue.value!.scopes.length - 1}.name`));
}

function removeScope(index: number) {
  if (!scopesValue.value || scopesValue.value.scopes.length <= 1) return;
  scopesValue.value.scopes.splice(index, 1);
  if (scopesValue.value.defaultIndex > index) scopesValue.value.defaultIndex -= 1;
  else if (scopesValue.value.defaultIndex === index) {
    scopesValue.value.defaultIndex = Math.min(index, scopesValue.value.scopes.length - 1);
  }
  selectedScopeIndex.value = Math.min(selectedScopeIndex.value, scopesValue.value.scopes.length - 1);
}

function setDefaultScope(index: number, selected: boolean) {
  if (scopesValue.value && selected) scopesValue.value.defaultIndex = index;
}

function setScopeNamespaces(index: number, value: string) {
  const scope = scopesValue.value?.scopes[index];
  if (scope) scope.namespaces = splitList(value);
}

function addPolicy() {
  if (!policiesValue.value || policiesValue.value.length >= 50) return;
  policiesValue.value.push({
    name: "",
    enabled: true,
    severities: ["critical"],
    namespaces: [],
    label_matchers: {},
    minimum_firing_seconds: 0,
    minimum_recurrence_count: 1,
    create_incident: true,
  });
  selectedPolicyIndex.value = policiesValue.value.length - 1;
  void nextTick(() => focusField(`policies.${policiesValue.value!.length - 1}.name`));
}

function removePolicy(index: number) {
  policiesValue.value?.splice(index, 1);
  selectedPolicyIndex.value = Math.min(selectedPolicyIndex.value, Math.max(0, (policiesValue.value?.length ?? 1) - 1));
}

function togglePolicySeverity(index: number, severity: EscalationPolicy["severities"][number], selected: boolean) {
  const policy = policiesValue.value?.[index];
  if (!policy) return;
  policy.severities = selected
    ? [...new Set([...policy.severities, severity])]
    : policy.severities.filter((item) => item !== severity);
}

function setPolicyNamespaces(index: number, value: string) {
  const policy = policiesValue.value?.[index];
  if (policy) policy.namespaces = splitList(value);
}

function policyMatchersText(policy: EscalationPolicy): string {
  return Object.entries(policy.label_matchers)
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([name, value]) => `${name}=${value}`)
    .join("\n");
}

function setPolicyMatchers(index: number, value: string) {
  const policy = policiesValue.value?.[index];
  if (!policy) return;
  const matchers: Record<string, string> = {};
  for (const line of value.split("\n")) {
    const separator = line.indexOf("=");
    if (separator < 1) continue;
    const name = line.slice(0, separator).trim();
    const matcherValue = line.slice(separator + 1).trim();
    if (name && matcherValue) matchers[name] = matcherValue;
  }
  policy.label_matchers = matchers;
}

function splitList(value: string): string[] {
  return value.split(",").map((item) => item.trim()).filter(Boolean);
}

function openProviderTest(provider: ProviderIdentity) {
  providerTestTarget.value = provider;
  providerTestError.value = "";
  providerTestOpen.value = true;
}

function closeProviderTest(value: boolean) {
  if (!value && !providerTesting.value) {
    providerTestOpen.value = false;
    providerTestTarget.value = null;
    providerTestError.value = "";
  }
}

async function confirmProviderTest() {
  const configuration = providerTestConfiguration.value;
  if (!configuration || providerTesting.value) return;
  providerTesting.value = true;
  providerTestError.value = "";
  const references = (secretReferencesValue.value ?? []).filter((item) => item.provider === configuration.provider);
  const scope = scopesValue.value?.scopes[scopesValue.value.defaultIndex];
  try {
    const result = await testProvider(configuration, references, scope?.cluster_id ?? "");
    providerResults.value = { ...providerResults.value, [configuration.provider]: result };
    providerTestOpen.value = false;
    providerTestTarget.value = null;
  } catch (reason) {
    providerTestError.value = describeError(reason, `${providerLabels[configuration.provider]} Provider test 失败。`);
  } finally {
    providerTesting.value = false;
  }
}

function openSecretModal() {
  secretState.value = "";
  secretError.value = "";
  secretModalOpen.value = true;
}

function closeSecretModal(value: boolean) {
  if (!value && !secretSaving.value) {
    secretState.value = "";
    secretError.value = "";
    secretModalOpen.value = false;
  }
}

async function saveSecret() {
  if (secretSaving.value || !secretState.value) return;
  if (!/^[a-z][a-z0-9_]{0,63}$/.test(secretState.purpose)) {
    secretError.value = "Purpose 必须以小写字母开头，并且只包含小写字母、数字或下划线。";
    return;
  }
  secretSaving.value = true;
  secretError.value = "";
  const request = createSecret({
    provider: secretState.provider,
    purpose: secretState.purpose,
    value: secretState.value,
  });
  secretState.value = "";
  try {
    const secret = await request;
    if (!secretReferencesValue.value) return;
    const next: SecretReference = {
      provider: secret.provider,
      purpose: secret.purpose,
      secret_version_id: secret.id,
      state: secret.state,
      fingerprint: secret.fingerprint,
    };
    const index = secretReferencesValue.value.findIndex((item) => (
      item.provider === secret.provider && item.purpose === secret.purpose
    ));
    if (index >= 0) secretReferencesValue.value.splice(index, 1, next);
    else secretReferencesValue.value.push(next);
    secretModalOpen.value = false;
    refreshMessage.value = `${providerLabels[secret.provider]} secret version 已创建；仅 reference 已加入本地 section 草稿。`;
    try {
      storage.value = await getStorageStatus();
    } catch {
      // Secret creation succeeded; storage diagnostics can be refreshed later.
    }
  } catch (reason) {
    secretState.value = "";
    secretError.value = describeError(reason, "Secret version 创建失败；值已从页面状态清除。 ");
  } finally {
    secretState.value = "";
    secretSaving.value = false;
  }
}

function removeSecretReference(index: number) {
  secretReferencesValue.value?.splice(index, 1);
}

async function handleBrowserNotificationToggle() {
  await nextTick();
  if (!systemValue.value?.browser_notifications_enabled) return;
  if (!("Notification" in window)) {
    systemValue.value.browser_notifications_enabled = false;
    runtimes.system.error = "BROWSER_NOTIFICATIONS_UNAVAILABLE：当前浏览器不支持 system notification。";
    return;
  }
  const permission = window.Notification.permission === "default"
    ? await window.Notification.requestPermission()
    : window.Notification.permission;
  if (permission !== "granted") {
    systemValue.value.browser_notifications_enabled = false;
    runtimes.system.error = "BROWSER_NOTIFICATION_PERMISSION_DENIED：浏览器未授予通知权限。";
  }
}

async function focusRouteAnchor(hash = route.hash) {
  if (!hash) return;
  const id = decodeURIComponent(hash.slice(1));
  if (!sectionLinks.some((item) => item.hash === `#${id}`)) return;
  await nextTick();
  window.requestAnimationFrame(() => {
    const target = document.getElementById(id);
    if (!target) return;
    target.scrollIntoView({ block: "start" });
    target.focus({ preventScroll: true });
  });
}

function handleBeforeUnload(event: BeforeUnloadEvent) {
  if (!shouldBlockSettingsLeave(hasUnsavedChanges.value)) return;
  persistSettingsDraftsNow();
  event.preventDefault();
  event.returnValue = "";
}

function discardAllDrafts() {
  if (!drafts.value) return;
  for (const key of settingsSectionKeys) {
    drafts.value[key] = resetSettingsSection(drafts.value[key]);
    clearSectionRuntime(key, true);
  }
  try {
    window.localStorage.removeItem(SETTINGS_DRAFT_STORAGE_KEY);
  } catch {
    draftPersistenceError.value = "DRAFT_STORAGE_UNAVAILABLE：无法清理浏览器中的草稿记录。";
  }
}

function stayOnSettings() {
  leaveModalOpen.value = false;
  pendingRoute.value = "";
}

async function discardAndLeave() {
  const target = pendingRoute.value;
  discardAllDrafts();
  leaveModalOpen.value = false;
  pendingRoute.value = "";
  if (target) await router.push(target);
}

async function preserveAndLeave() {
  const target = pendingRoute.value;
  if (!target || !persistSettingsDraftsNow()) return;
  allowNextSettingsLeave = true;
  leaveModalOpen.value = false;
  pendingRoute.value = "";
  try {
    await router.push(target);
  } finally {
    allowNextSettingsLeave = false;
  }
}

function handleOperationalScopeChanged() {
  void loadSettings(false);
}

onBeforeRouteLeave((to) => {
  if (allowNextSettingsLeave) {
    allowNextSettingsLeave = false;
    return true;
  }
  if (!shouldBlockSettingsLeave(hasUnsavedChanges.value)) return true;
  pendingRoute.value = to.fullPath;
  leaveModalOpen.value = true;
  return false;
});

watch(() => route.hash, (hash) => {
  void focusRouteAnchor(hash);
  void updateSettingsNavIndicator();
});
watch([settings, drafts], () => void updateSettingsNavIndicator());
watch(drafts, scheduleSettingsDraftPersistence, { deep: true });
watch(() => secretState.provider, (provider) => {
  secretState.purpose = provider === "llm" ? "api_key" : provider === "kubernetes" ? "credential" : "token";
  secretState.value = "";
});

onMounted(() => {
  window.addEventListener("beforeunload", handleBeforeUnload);
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  window.addEventListener("resize", updateSettingsNavIndicator);
  void loadSettings(true);
  void updateSettingsNavIndicator();
});

onBeforeUnmount(() => {
  mounted = false;
  if (draftPersistenceReady && hasUnsavedChanges.value) persistSettingsDraftsNow();
  if (draftPersistenceTimer !== undefined) clearTimeout(draftPersistenceTimer);
  secretState.value = "";
  window.removeEventListener("beforeunload", handleBeforeUnload);
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  window.removeEventListener("resize", updateSettingsNavIndicator);
});
</script>

<template>
  <article
    :class="['settings-view', { 'is-provider-editor-open': providerEditorOpen }]"
    aria-labelledby="settings-title"
  >
    <header class="settings-page-heading">
      <div class="settings-heading-copy">
        <p class="settings-eyebrow">
          CloudOps configuration
        </p>
        <h1 id="settings-title">
          设置
        </h1>
        <p>管理运行边界、外部连接与配置版本。每次只处理一个清晰的设置任务。</p>
      </div>
      <div class="settings-page-actions">
        <UBadge
          :color="hasUnsavedChanges ? 'warning' : 'success'"
          variant="soft"
          :icon="hasUnsavedChanges ? 'i-lucide-pencil-line' : 'i-lucide-circle-check'"
          :label="hasUnsavedChanges ? `${dirtySectionCount} 个分区有未保存修改` : activeRevision ? `Revision #${activeRevision.number} · 已生效` : '正在读取配置'"
        />
        <UButton
          color="neutral"
          variant="ghost"
          icon="i-lucide-refresh-cw"
          label="刷新"
          :loading="loading"
          @click="loadSettings(false, true)"
        />
      </div>
    </header>

    <div class="settings-workbench">
      <aside class="settings-navigation">
        <div class="settings-navigation__heading">
          <strong>设置导航</strong>
          <span>{{ sectionLinks.length }} 项</span>
        </div>
        <div class="settings-search-wrap">
          <UInput
            v-model="settingsSearch"
            icon="i-lucide-search"
            placeholder="搜索设置"
            aria-label="搜索设置"
            class="settings-search"
          />
          <div
            v-if="settingsSearch && searchResults.length"
            class="settings-search-results"
            role="listbox"
            aria-label="设置搜索结果"
          >
            <button
              v-for="result in searchResults"
              :key="`${result.key}-${result.field ?? result.label}`"
              type="button"
              role="option"
              @click="jumpToSearchResult(result)"
            >
              <span><strong>{{ result.label }}</strong><small>{{ result.text }}</small></span>
              <UIcon
                name="i-lucide-arrow-up-right"
                aria-hidden="true"
              />
            </button>
          </div>
          <p
            v-else-if="settingsSearch"
            class="settings-search-empty"
          >
            没有匹配的设置项
          </p>
        </div>
        <nav
          ref="settingsNavElement"
          class="settings-section-nav"
          aria-label="Settings 分区"
        >
          <span
            class="settings-nav-indicator"
            :style="settingsNavIndicatorStyle"
            aria-hidden="true"
          />
          <div
            v-for="group in sectionGroups"
            :key="group.label"
            class="settings-section-nav-group"
          >
            <span class="settings-section-nav-group__label">{{ group.label }}</span>
            <button
              v-for="item in group.items"
              :key="item.key"
              type="button"
              :data-settings-section="item.key"
              :class="['settings-section-link', { 'is-active': activeSectionKey === item.key }]"
              :aria-current="activeSectionKey === item.key ? 'page' : undefined"
              @click="selectSection(item.key)"
            >
              <UIcon
                :name="item.icon"
                aria-hidden="true"
              />
              <span><strong>{{ item.label }}</strong><small>{{ sectionInventory(item.key) }}</small></span>
              <UBadge
                v-if="sectionErrorCounts[item.key]"
                color="error"
                variant="soft"
                :label="String(sectionErrorCounts[item.key])"
              />
            </button>
          </div>
        </nav>
        <div
          v-if="activeRevision"
          class="settings-navigation__baseline"
        >
          <span
            class="settings-baseline-dot"
            aria-hidden="true"
          />
          <div>
            <strong>Revision #{{ activeRevision.number }}</strong>
            <small>{{ activationLabel(activeRevision.worker_boundary?.status) }}</small>
          </div>
        </div>
      </aside>

      <main class="settings-current">
        <UAlert
          v-if="loadError"
          color="error"
          variant="soft"
          icon="i-lucide-circle-x"
          title="Settings 无法读取"
          :description="loadError"
        >
          <template #actions>
            <UButton
              color="error"
              variant="outline"
              icon="i-lucide-refresh-cw"
              label="重试读取"
              @click="loadSettings(false, true)"
            />
          </template>
        </UAlert>
        <UAlert
          v-else-if="loading && !settings"
          color="neutral"
          variant="soft"
          icon="i-lucide-loader-circle"
          title="正在读取 Settings"
          description="读取活动 Revision、Provider health、storage 与 bootstrap diagnostics。"
        />
        <UAlert
          v-if="refreshMessage"
          color="info"
          variant="soft"
          icon="i-lucide-info"
          title="本地状态已更新"
          :description="refreshMessage"
          :close="true"
          @update:open="refreshMessage = ''"
        />
        <UAlert
          v-if="draftPersistenceError"
          color="warning"
          variant="soft"
          icon="i-lucide-hard-drive-download"
          title="本地草稿存储不可用"
          :description="draftPersistenceError"
        />

        <template v-if="settings && drafts && activeRevision">
          <div class="settings-context-bar">
            <div>
              <strong>{{ activeSectionLink.label }}</strong>
              <span>Revision #{{ activeRevision.number }} · {{ hasUnsavedChanges ? `${dirtySectionCount} 个分区有未保存修改` : "已生效" }}</span>
            </div>
            <UButton
              color="neutral"
              variant="ghost"
              :icon="advancedSettingsOpen ? 'i-lucide-chevron-up' : 'i-lucide-sliders-horizontal'"
              :label="advancedSettingsOpen ? '收起高级设置' : '显示高级设置'"
              @click="advancedSettingsOpen = !advancedSettingsOpen"
            />
          </div>
          <WorkspaceTechnicalDetails
            v-if="advancedSettingsOpen"
            :fields="[
              { label: 'Revision ID', value: activeRevision.id, code: true, copyValue: activeRevision.id },
              { label: 'Exact hash', value: activeRevision.hash, code: true, copyValue: activeRevision.hash },
              { label: 'Created at (UTC)', value: formatISO(activeRevision.created_at), code: true },
              { label: 'Worker boundary', value: activationLabel(activeRevision.worker_boundary?.status) },
            ]"
          />

          <Transition
            name="settings-panel"
            mode="out-in"
          >
            <div
              :key="activeSectionKey"
              class="settings-panel"
            >
              <UForm
                v-if="activeSectionKey === 'system' && systemValue"
                id="settings-form-system"
                :state="drafts.system"
                :validate="() => sectionFormErrors('system')"
                :validate-on="['blur', 'change']"
                @submit="validateSection('system')"
                @error="focusFirstError($event.errors)"
              >
                <SettingsSectionPanel
                  external-actions
                  anchor="system"
                  title="查询、保留与通知"
                  eyebrow="System boundaries"
                  description="控制查询边界、Telemetry 保留和 Owner 浏览器行为。每项 apply 仍创建完整 Revision。"
                  form-id="settings-form-system"
                  :section="drafts.system"
                  :active-revision="activeRevision"
                  :validation="runtimes.system.validation"
                  :validated-fingerprint="runtimes.system.validatedFingerprint"
                  :validating="runtimes.system.validating"
                  :applying="runtimes.system.applying"
                  :error="runtimes.system.error"
                  :outcome="runtimes.system.outcome"
                  @update-summary="setSectionSummary('system', $event)"
                  @reset="resetSection('system')"
                  @rebase="rebaseSection('system', $event)"
                  @apply="requestApply('system')"
                  @refresh-outcome="refreshApplyOutcome('system')"
                >
                  <div class="settings-component-group">
                    <header class="settings-component-group__heading">
                      <div>
                        <p class="settings-eyebrow">
                          查询与 Telemetry
                        </p>
                        <h3>读取边界</h3>
                        <p>把后端边界转换成人类可读单位；技术值仍保留在详情与 Revision 中。</p>
                      </div>
                      <UBadge
                        color="neutral"
                        variant="soft"
                        label="只影响读取"
                      />
                    </header>
                    <div class="settings-setting-list">
                      <div class="settings-setting-row">
                        <div class="settings-setting-copy">
                          <strong>最大回看时间</strong>
                          <p>限制指标、日志和链路查询可以跨越的时间范围。</p>
                          <small>推荐 24 小时 · 原始值 {{ systemValue.query_max_lookback_seconds }} 秒</small>
                        </div>
                        <UFormField
                          label="小时"
                          name="system.query_max_lookback_seconds"
                          :data-field="'system.query_max_lookback_seconds'"
                          class="settings-setting-field"
                        >
                          <UInputNumber
                            :model-value="secondsToHours(systemValue.query_max_lookback_seconds)"
                            :min="1"
                            :max="720"
                            :step="1"
                            class="settings-setting-control"
                            @update:model-value="systemValue.query_max_lookback_seconds = hoursToSeconds($event)"
                          />
                        </UFormField>
                      </div>
                      <div class="settings-setting-row">
                        <div class="settings-setting-copy">
                          <strong>查询结果上限</strong>
                          <p>为单次查询设置结果数量上限，避免大范围读取拖垮 Provider。</p>
                          <small>推荐 1,000 条</small>
                        </div>
                        <UFormField
                          label="条"
                          name="system.query_max_results"
                          :data-field="'system.query_max_results'"
                          class="settings-setting-field"
                        >
                          <UInputNumber
                            v-model="systemValue.query_max_results"
                            :min="1"
                            :max="10000"
                            :step="1"
                            class="settings-setting-control"
                          />
                        </UFormField>
                      </div>
                      <div class="settings-setting-row">
                        <div class="settings-setting-copy">
                          <strong>Telemetry 保留天数</strong>
                          <p>控制本地 Telemetry 数据的保留窗口，变更会影响存储容量。</p>
                          <small>当前存储状态可在 Revision 历史中查看</small>
                        </div>
                        <UFormField
                          label="天"
                          name="system.telemetry_retention_days"
                          :data-field="'system.telemetry_retention_days'"
                          class="settings-setting-field"
                        >
                          <UInputNumber
                            v-model="systemValue.telemetry_retention_days"
                            :min="1"
                            :max="365"
                            class="settings-setting-control"
                          />
                        </UFormField>
                      </div>
                    </div>
                  </div>
                  <div class="settings-component-group settings-component-group--behavior">
                    <header class="settings-component-group__heading">
                      <div>
                        <p class="settings-eyebrow">
                          通知与升级
                        </p>
                        <h3>浏览器与告警行为</h3>
                        <p>每个开关独立形成草稿变更；服务端 Policy 和权限边界仍然优先。</p>
                      </div>
                    </header>
                    <div class="settings-setting-list settings-setting-list--toggles">
                      <div class="settings-setting-row settings-setting-row--toggle">
                        <div class="settings-setting-copy">
                          <strong>浏览器提醒</strong>
                          <p>启用时由浏览器单独请求 Notification 权限。</p>
                        </div>
                        <USwitch
                          v-model="systemValue.browser_notifications_enabled"
                          aria-label="浏览器提醒"
                          data-field="system.browser_notifications_enabled"
                          @change="handleBrowserNotificationToggle"
                        />
                      </div>
                      <div class="settings-setting-row settings-setting-row--toggle">
                        <div class="settings-setting-copy">
                          <strong>自动 escalation</strong>
                          <p>仅在活动 Escalation Policy 和服务端边界允许时生效。</p>
                        </div>
                        <USwitch
                          v-model="systemValue.automatic_escalation_enabled"
                          aria-label="自动 escalation"
                          data-field="system.automatic_escalation_enabled"
                        />
                      </div>
                    </div>
                  </div>
                </SettingsSectionPanel>
              </UForm>

              <UForm
                v-if="activeSectionKey === 'scopes' && scopesValue"
                id="settings-form-scopes"
                :state="drafts.scopes"
                :validate="() => sectionFormErrors('scopes')"
                :validate-on="['blur', 'change']"
                @submit="validateSection('scopes')"
                @error="focusFirstError($event.errors)"
              >
                <SettingsSectionPanel
                  external-actions
                  anchor="operational-scope"
                  title="Operational Scope"
                  eyebrow="Cluster boundaries"
                  description="维护 Revision 内的 Cluster Scope 和默认 Scope；不会在编辑时激活真实集群。"
                  form-id="settings-form-scopes"
                  :section="drafts.scopes"
                  :active-revision="activeRevision"
                  :validation="runtimes.scopes.validation"
                  :validated-fingerprint="runtimes.scopes.validatedFingerprint"
                  :validating="runtimes.scopes.validating"
                  :applying="runtimes.scopes.applying"
                  :error="runtimes.scopes.error"
                  :outcome="runtimes.scopes.outcome"
                  @update-summary="setSectionSummary('scopes', $event)"
                  @reset="resetSection('scopes')"
                  @rebase="rebaseSection('scopes', $event)"
                  @apply="requestApply('scopes')"
                  @refresh-outcome="refreshApplyOutcome('scopes')"
                >
                  <div class="settings-domain-editor">
                    <aside
                      class="settings-object-list"
                      aria-label="运行范围列表"
                    >
                      <header>
                        <div><strong>范围规则</strong><span>{{ scopesValue.scopes.length }} / 10</span></div>
                        <UButton
                          color="neutral"
                          variant="ghost"
                          icon="i-lucide-plus"
                          square
                          aria-label="添加运行范围"
                          :disabled="scopesValue.scopes.length >= 10"
                          @click="addScope"
                        />
                      </header>
                      <button
                        v-for="(scope, index) in scopesValue.scopes"
                        :key="scope.id || `scope-${index}`"
                        type="button"
                        :class="['settings-object-button', { 'is-active': selectedScopeIndex === index }]"
                        @click="selectedScopeIndex = index"
                      >
                        <span class="settings-object-button__icon"><UIcon name="i-lucide-box" /></span>
                        <span>
                          <strong>{{ scope.name || `范围 ${index + 1}` }}</strong>
                          <small>{{ scope.cluster_id || '未设置 Cluster' }}</small>
                        </span>
                        <span class="settings-object-button__badges">
                          <UBadge
                            v-if="scopesValue.defaultIndex === index"
                            color="primary"
                            variant="soft"
                            label="默认"
                          />
                          <UBadge
                            v-if="scope.active"
                            color="success"
                            variant="soft"
                            label="活动"
                          />
                        </span>
                      </button>
                    </aside>

                    <article
                      v-if="selectedScope"
                      class="settings-object-editor"
                    >
                      <header>
                        <div>
                          <p class="settings-eyebrow">
                            Scope rule {{ selectedScopeIndex + 1 }}
                          </p>
                          <h3>{{ selectedScope.name || '未命名运行范围' }}</h3>
                          <p>定义可见 Cluster 与 Namespace 边界，不在编辑时激活真实集群。</p>
                        </div>
                        <div class="settings-object-editor__actions">
                          <UCheckbox
                            :model-value="scopesValue.defaultIndex === selectedScopeIndex"
                            label="Revision 默认范围"
                            @update:model-value="setDefaultScope(selectedScopeIndex, Boolean($event))"
                          />
                          <UButton
                            color="error"
                            variant="ghost"
                            icon="i-lucide-trash-2"
                            square
                            :aria-label="`从草稿移除 ${selectedScope.name || `范围 ${selectedScopeIndex + 1}`}`"
                            :disabled="scopesValue.scopes.length <= 1"
                            @click="removeScope(selectedScopeIndex)"
                          />
                        </div>
                      </header>
                      <div class="settings-form-grid settings-form-grid--scope">
                        <UFormField
                          label="范围名称"
                          :name="`scopes.${selectedScopeIndex}.name`"
                          :data-field="`scopes.${selectedScopeIndex}.name`"
                        >
                          <UInput
                            v-model="selectedScope.name"
                            class="settings-control"
                            autocomplete="off"
                          />
                        </UFormField>
                        <UFormField
                          label="Cluster identity"
                          :name="`scopes.${selectedScopeIndex}.cluster_id`"
                          :data-field="`scopes.${selectedScopeIndex}.cluster_id`"
                        >
                          <UInput
                            v-model="selectedScope.cluster_id"
                            class="settings-control"
                            autocomplete="off"
                            spellcheck="false"
                          />
                        </UFormField>
                        <UFormField
                          label="Environment"
                          :name="`scopes.${selectedScopeIndex}.environment`"
                          :data-field="`scopes.${selectedScopeIndex}.environment`"
                        >
                          <UInput
                            v-model="selectedScope.environment"
                            class="settings-control"
                            autocomplete="off"
                          />
                        </UFormField>
                        <UFormField
                          label="Namespaces"
                          :name="`scopes.${selectedScopeIndex}.namespaces`"
                          :data-field="`scopes.${selectedScopeIndex}.namespaces`"
                          help="逗号分隔；应用后共同构成允许范围。"
                        >
                          <UInput
                            :model-value="selectedScope.namespaces.join(', ')"
                            class="settings-control"
                            autocomplete="off"
                            spellcheck="false"
                            @update:model-value="setScopeNamespaces(selectedScopeIndex, String($event))"
                          />
                        </UFormField>
                      </div>
                      <footer class="scope-effective-preview">
                        <div><span>最终生效范围</span><strong>{{ selectedScope.cluster_id || '未设置 Cluster' }} / {{ selectedScope.environment || '未设置环境' }}</strong></div>
                        <div class="scope-namespace-list">
                          <UBadge
                            v-for="namespace in selectedScope.namespaces"
                            :key="namespace"
                            color="neutral"
                            variant="soft"
                            :label="namespace"
                          />
                          <span v-if="!selectedScope.namespaces.length">尚未选择 Namespace</span>
                        </div>
                      </footer>
                    </article>
                  </div>
                </SettingsSectionPanel>
              </UForm>

              <UForm
                v-if="activeSectionKey === 'policies' && policiesValue"
                id="settings-form-policies"
                :state="drafts.policies"
                :validate="() => sectionFormErrors('policies')"
                :validate-on="['blur', 'change']"
                @submit="validateSection('policies')"
                @error="focusFirstError($event.errors)"
              >
                <SettingsSectionPanel
                  external-actions
                  anchor="escalation-policies"
                  title="Escalation Policies"
                  eyebrow="Alert escalation"
                  description="定义哪些服务端告警条件可以创建 Incident；create_incident 始终保持真实契约值。"
                  form-id="settings-form-policies"
                  :section="drafts.policies"
                  :active-revision="activeRevision"
                  :validation="runtimes.policies.validation"
                  :validated-fingerprint="runtimes.policies.validatedFingerprint"
                  :validating="runtimes.policies.validating"
                  :applying="runtimes.policies.applying"
                  :error="runtimes.policies.error"
                  :outcome="runtimes.policies.outcome"
                  @update-summary="setSectionSummary('policies', $event)"
                  @reset="resetSection('policies')"
                  @rebase="rebaseSection('policies', $event)"
                  @apply="requestApply('policies')"
                  @refresh-outcome="refreshApplyOutcome('policies')"
                >
                  <div class="settings-list-heading">
                    <span>{{ policiesValue.length }} / 50 policies</span>
                    <UButton
                      color="neutral"
                      variant="outline"
                      icon="i-lucide-plus"
                      label="添加 Policy"
                      :disabled="policiesValue.length >= 50"
                      @click="addPolicy"
                    />
                  </div>
                  <UAlert
                    v-if="policiesValue.length === 0"
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-list-x"
                    title="当前 Revision 没有 Escalation Policy"
                    description="自动 escalation 不会因为前端空状态而被推断为开启。"
                  />
                  <div
                    v-else
                    class="settings-domain-editor"
                  >
                    <aside
                      class="settings-object-list"
                      aria-label="升级策略列表"
                    >
                      <button
                        v-for="(policy, index) in policiesValue"
                        :key="policy.id || `policy-${index}`"
                        type="button"
                        :class="['settings-object-button', { 'is-active': selectedPolicyIndex === index }]"
                        @click="selectedPolicyIndex = index"
                      >
                        <span class="settings-object-button__icon"><UIcon name="i-lucide-route" /></span>
                        <span>
                          <strong>{{ policy.name || `策略 ${index + 1}` }}</strong>
                          <small>{{ policy.severities.join(' · ') || '未选择 Severity' }}</small>
                        </span>
                        <UBadge
                          :color="policy.enabled ? 'success' : 'neutral'"
                          variant="soft"
                          :label="policy.enabled ? '启用' : '停用'"
                        />
                      </button>
                    </aside>

                    <article
                      v-if="selectedPolicy"
                      class="settings-object-editor"
                    >
                      <header>
                        <div>
                          <p class="settings-eyebrow">
                            Escalation policy {{ selectedPolicyIndex + 1 }}
                          </p>
                          <h3>{{ selectedPolicy.name || '未命名升级策略' }}</h3>
                          <p>定义告警进入 Incident 的条件链；不会绕过服务端权限与运行边界。</p>
                        </div>
                        <div class="settings-object-editor__actions">
                          <USwitch
                            v-model="selectedPolicy.enabled"
                            :label="selectedPolicy.enabled ? '已启用' : '已停用'"
                          />
                          <UButton
                            color="error"
                            variant="ghost"
                            icon="i-lucide-trash-2"
                            square
                            :aria-label="`从草稿移除 ${selectedPolicy.name || `策略 ${selectedPolicyIndex + 1}`}`"
                            @click="removePolicy(selectedPolicyIndex)"
                          />
                        </div>
                      </header>
                      <div
                        class="policy-stage-preview"
                        aria-label="升级条件链"
                      >
                        <div><span>01</span><strong>Severity 匹配</strong><small>{{ selectedPolicy.severities.join(', ') || '未设置' }}</small></div>
                        <UIcon
                          name="i-lucide-arrow-right"
                          aria-hidden="true"
                        />
                        <div><span>02</span><strong>持续观察</strong><small>{{ secondsToMinutes(selectedPolicy.minimum_firing_seconds) }} 分钟</small></div>
                        <UIcon
                          name="i-lucide-arrow-right"
                          aria-hidden="true"
                        />
                        <div><span>03</span><strong>创建 Incident</strong><small>至少 {{ selectedPolicy.minimum_recurrence_count }} 次</small></div>
                      </div>
                      <div class="settings-form-grid settings-form-grid--policy">
                        <UFormField
                          class="settings-span-two"
                          label="策略名称"
                          :name="`policies.${selectedPolicyIndex}.name`"
                          :data-field="`policies.${selectedPolicyIndex}.name`"
                        >
                          <UInput
                            v-model="selectedPolicy.name"
                            class="settings-control"
                            autocomplete="off"
                          />
                        </UFormField>
                        <UFormField
                          class="settings-span-two"
                          label="Severity"
                          :name="`policies.${selectedPolicyIndex}.severities`"
                          :data-field="`policies.${selectedPolicyIndex}.severities`"
                        >
                          <div class="settings-checkbox-row">
                            <UCheckbox
                              v-for="severity in severityOptions"
                              :key="severity"
                              :model-value="selectedPolicy.severities.includes(severity)"
                              :label="severity"
                              @update:model-value="togglePolicySeverity(selectedPolicyIndex, severity, Boolean($event))"
                            />
                          </div>
                        </UFormField>
                        <UFormField
                          label="持续 firing（分钟）"
                          :name="`policies.${selectedPolicyIndex}.minimum_firing_seconds`"
                          :data-field="`policies.${selectedPolicyIndex}.minimum_firing_seconds`"
                        >
                          <UInputNumber
                            :model-value="secondsToMinutes(selectedPolicy.minimum_firing_seconds)"
                            :min="0"
                            :max="10080"
                            :step="1"
                            class="settings-control"
                            @update:model-value="selectedPolicy.minimum_firing_seconds = minutesToSeconds($event)"
                          />
                        </UFormField>
                        <UFormField
                          label="最小复发次数"
                          :name="`policies.${selectedPolicyIndex}.minimum_recurrence_count`"
                          :data-field="`policies.${selectedPolicyIndex}.minimum_recurrence_count`"
                        >
                          <UInputNumber
                            v-model="selectedPolicy.minimum_recurrence_count"
                            :min="1"
                            :max="100"
                            class="settings-control"
                          />
                        </UFormField>
                        <UFormField
                          class="settings-span-two"
                          label="Namespaces"
                          :name="`policies.${selectedPolicyIndex}.namespaces`"
                          :data-field="`policies.${selectedPolicyIndex}.namespaces`"
                          help="逗号分隔；留空表示不限。"
                        >
                          <UInput
                            :model-value="selectedPolicy.namespaces.join(', ')"
                            class="settings-control"
                            autocomplete="off"
                            spellcheck="false"
                            @update:model-value="setPolicyNamespaces(selectedPolicyIndex, String($event))"
                          />
                        </UFormField>
                        <UFormField
                          class="settings-span-two"
                          label="Exact label matchers"
                          :name="`policies.${selectedPolicyIndex}.label_matchers`"
                          :data-field="`policies.${selectedPolicyIndex}.label_matchers`"
                          help="每行 name=value。"
                        >
                          <UTextarea
                            :model-value="policyMatchersText(selectedPolicy)"
                            :rows="3"
                            class="settings-control"
                            spellcheck="false"
                            @update:model-value="setPolicyMatchers(selectedPolicyIndex, String($event))"
                          />
                        </UFormField>
                      </div>
                    </article>
                  </div>
                </SettingsSectionPanel>
              </UForm>

              <UForm
                v-if="activeSectionKey === 'providers' && providersValue"
                id="settings-form-providers"
                :state="drafts.providers"
                :validate="() => sectionFormErrors('providers')"
                :validate-on="['blur', 'change']"
                @submit="validateSection('providers')"
                @error="focusFirstError($event.errors)"
              >
                <SettingsSectionPanel
                  external-actions
                  anchor="providers"
                  title="Provider 配置"
                  eyebrow="External connections"
                  description="编辑 endpoint、模型和查询边界。Provider test 是独立请求，不代表配置已 apply。"
                  form-id="settings-form-providers"
                  :section="drafts.providers"
                  :active-revision="activeRevision"
                  :validation="runtimes.providers.validation"
                  :validated-fingerprint="runtimes.providers.validatedFingerprint"
                  :validating="runtimes.providers.validating"
                  :applying="runtimes.providers.applying"
                  :error="runtimes.providers.error"
                  :outcome="runtimes.providers.outcome"
                  @update-summary="setSectionSummary('providers', $event)"
                  @reset="resetSection('providers')"
                  @rebase="rebaseSection('providers', $event)"
                  @apply="requestApply('providers')"
                  @refresh-outcome="refreshApplyOutcome('providers')"
                >
                  <UAlert
                    v-if="providersValue.length === 0"
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-plug"
                    title="当前 Revision 没有 Provider 配置"
                    description="页面只展示服务端返回的 Provider 连接事实。"
                  />
                  <div
                    v-else
                    class="settings-provider-workbench"
                  >
                    <div
                      class="provider-summary-list"
                      role="list"
                      aria-label="Provider 连接摘要"
                    >
                      <article
                        v-for="provider in providersValue"
                        :key="provider.provider"
                        class="provider-summary-row"
                        role="listitem"
                        tabindex="0"
                        @click="openProviderEditor(provider.provider)"
                        @keydown.enter.prevent="openProviderEditor(provider.provider)"
                      >
                        <div
                          class="provider-summary-icon"
                          aria-hidden="true"
                        >
                          <UIcon :name="providerIcons[provider.provider]" />
                        </div>
                        <div class="provider-summary-content">
                          <header>
                            <div class="provider-summary-name">
                              <span
                                class="provider-state-dot"
                                :data-state="providerState(provider.provider)"
                              /><strong>{{ providerLabels[provider.provider] }}</strong>
                            </div>
                            <UBadge
                              :color="stateColor(providerState(provider.provider))"
                              variant="soft"
                              :label="stateLabel(providerState(provider.provider))"
                            />
                          </header>
                          <p>{{ providerDescriptions[provider.provider] }}</p>
                          <div class="provider-summary-meta">
                            <span>{{ provider.endpoint || '服务端默认 Endpoint' }}</span>
                            <span>{{ providerSecretCount(provider.provider) }} 个 Secret 引用</span>
                            <span>{{ providerCheckedAt(provider.provider) }}</span>
                          </div>
                        </div>
                        <UButton
                          color="neutral"
                          variant="ghost"
                          icon="i-lucide-chevron-right"
                          square
                          :aria-label="`编辑 ${providerLabels[provider.provider]}`"
                          @click.stop="openProviderEditor(provider.provider)"
                        />
                      </article>
                    </div>
                  </div>
                </SettingsSectionPanel>
              </UForm>

              <UForm
                v-if="activeSectionKey === 'secret-references' && secretReferencesValue"
                id="settings-form-secret-references"
                :state="drafts['secret-references']"
                :validate="() => sectionFormErrors('secret-references')"
                :validate-on="['blur', 'change']"
                @submit="validateSection('secret-references')"
                @error="focusFirstError($event.errors)"
              >
                <SettingsSectionPanel
                  external-actions
                  anchor="secret-references"
                  title="Secret references"
                  eyebrow="Write-only values"
                  description="页面只持有 Secret version reference 与 fingerprint；Secret value 不进入草稿、状态或历史。"
                  form-id="settings-form-secret-references"
                  :section="drafts['secret-references']"
                  :active-revision="activeRevision"
                  :validation="runtimes['secret-references'].validation"
                  :validated-fingerprint="runtimes['secret-references'].validatedFingerprint"
                  :validating="runtimes['secret-references'].validating"
                  :applying="runtimes['secret-references'].applying"
                  :error="runtimes['secret-references'].error"
                  :outcome="runtimes['secret-references'].outcome"
                  @update-summary="setSectionSummary('secret-references', $event)"
                  @reset="resetSection('secret-references')"
                  @rebase="rebaseSection('secret-references', $event)"
                  @apply="requestApply('secret-references')"
                  @refresh-outcome="refreshApplyOutcome('secret-references')"
                >
                  <div class="settings-list-heading">
                    <span>{{ secretReferencesValue.length }} references</span>
                    <UButton
                      color="warning"
                      variant="outline"
                      icon="i-lucide-key-round"
                      label="创建 Secret version"
                      @click="openSecretModal"
                    />
                  </div>
                  <UAlert
                    v-if="secretReferencesValue.length === 0"
                    color="neutral"
                    variant="soft"
                    icon="i-lucide-key-square"
                    title="当前 Revision 没有 Secret reference"
                    description="Secret 值不会因为空状态而显示或回填。"
                  />
                  <ul
                    v-else
                    class="secret-reference-list"
                    data-field="secret-references"
                  >
                    <li
                      v-for="(reference, index) in secretReferencesValue"
                      :key="`${reference.provider}-${reference.purpose}`"
                    >
                      <div><strong>{{ providerLabels[reference.provider] }} / {{ reference.purpose }}</strong><span>{{ reference.state || 'configured' }}</span></div>
                      <code>{{ reference.fingerprint || reference.secret_version_id }}</code>
                      <UButton
                        color="error"
                        variant="ghost"
                        icon="i-lucide-trash-2"
                        square
                        :aria-label="`从本地草稿移除 ${reference.provider}/${reference.purpose} reference`"
                        @click="removeSecretReference(index)"
                      />
                    </li>
                  </ul>
                </SettingsSectionPanel>
              </UForm>

              <section
                v-if="activeSectionKey === 'revisions'"
                id="revision-history"
                class="settings-readonly-section"
                aria-labelledby="revision-history-heading"
                tabindex="-1"
              >
                <header>
                  <div>
                    <p class="settings-eyebrow">
                      Read-only history
                    </p><h2 id="revision-history-heading">
                      Configuration Revisions
                    </h2>
                  </div>
                  <UBadge
                    color="neutral"
                    variant="soft"
                    :label="`${revisionRows.length} revisions`"
                  />
                </header>
                <div class="settings-table-scroll">
                  <UTable
                    :data="revisionRows"
                    :columns="revisionColumns"
                    empty="没有 Revision 历史"
                    class="revision-table"
                  />
                </div>
              </section>

              <section
                v-if="activeSectionKey === 'revisions'"
                class="settings-readonly-section"
                aria-labelledby="storage-heading"
              >
                <header>
                  <div>
                    <p class="settings-eyebrow">
                      Durability
                    </p><h2 id="storage-heading">
                      存储与备份
                    </h2>
                  </div>
                </header>
                <dl
                  v-if="storage"
                  class="settings-facts-grid"
                >
                  <div><dt>数据库表</dt><dd>{{ storage.database_tables }}</dd></div>
                  <div><dt>配置 Revision</dt><dd>{{ storage.configuration_count }}</dd></div>
                  <div><dt>通知记录</dt><dd>{{ storage.notification_count }}</dd></div>
                  <div><dt>Secret versions</dt><dd>{{ storage.secret_version_count }}</dd></div>
                  <div><dt>Data capacity</dt><dd>{{ formatBytes(storage.data_capacity_bytes) }}</dd></div>
                  <div><dt>Data available</dt><dd>{{ formatBytes(storage.data_available_bytes) }}</dd></div>
                  <div><dt>最近备份</dt><dd>{{ storage.latest_backup_name || "无记录" }}</dd></div>
                  <div><dt>备份时间 (UTC)</dt><dd>{{ formatISO(storage.latest_backup_at) }}</dd></div>
                </dl>
              </section>

              <section
                v-if="activeSectionKey === 'revisions'"
                class="settings-readonly-section"
                aria-labelledby="bootstrap-heading"
              >
                <header>
                  <div>
                    <p class="settings-eyebrow">
                      Read-only diagnostics
                    </p><h2 id="bootstrap-heading">
                      Bootstrap diagnostics
                    </h2>
                  </div>
                </header>
                <dl class="settings-facts-grid settings-facts-grid--diagnostics">
                  <div><dt>Listen boundary</dt><dd>{{ settings.bootstrap.listen_boundary }}</dd></div>
                  <div><dt>MySQL database</dt><dd>{{ settings.bootstrap.mysql_database }}</dd></div>
                  <div><dt>Data directory</dt><dd>{{ settings.bootstrap.data_directory }}</dd></div>
                  <div><dt>Worker target</dt><dd>{{ settings.bootstrap.worker_management_target }}</dd></div>
                  <div><dt>Lifecycle</dt><dd>{{ settings.bootstrap.lifecycle }}</dd></div>
                </dl>
              </section>
            </div>
          </Transition>
        </template>
      </main>
    </div>

    <Transition name="settings-actions">
      <div
        v-if="activeSectionIsDirty && activeSectionDraft && activeSectionRuntime"
        class="settings-action-bar"
        aria-live="polite"
      >
        <div class="settings-action-status">
          <span
            :class="['settings-action-status__dot', { 'is-valid': activeSectionCanApply, 'is-invalid': activeSectionRuntime.validation && !activeSectionRuntime.validation.valid }]"
            aria-hidden="true"
          />
          <div>
            <strong>{{ activeSectionStatus }}</strong>
            <span>{{ settingsSectionLabel(activeSectionDraft.key) }} · base #{{ activeSectionDraft.baseRevisionNumber }}</span>
          </div>
        </div>
        <div class="settings-action-buttons">
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-rotate-ccw"
            label="放弃修改"
            :disabled="activeSectionRuntime.validating || activeSectionRuntime.applying"
            @click="resetSection(activeSectionDraft.key)"
          />
          <UButton
            type="submit"
            :form="`settings-form-${activeSectionDraft.key}`"
            color="neutral"
            variant="outline"
            icon="i-lucide-shield-check"
            :label="activeSectionCanApply ? '重新验证' : '验证配置'"
            :loading="activeSectionRuntime.validating"
            :disabled="!activeSectionCanValidate"
          />
          <UButton
            v-if="activeSectionCanApply"
            color="primary"
            icon="i-lucide-git-compare-arrows"
            label="查看变更并应用"
            :loading="activeSectionRuntime.applying"
            @click="requestApply(activeSectionDraft.key)"
          />
        </div>
      </div>
    </Transition>

    <UModal
      :open="applyConfirmationOpen"
      title="查看变更并应用"
      :description="pendingApplyDraft ? `${settingsSectionLabel(pendingApplyDraft.key)} · base Revision #${pendingApplyDraft.baseRevisionNumber}` : '配置变更'"
      :dismissible="true"
      @update:open="applyConfirmationOpen = $event"
    >
      <template #body>
        <div
          v-if="pendingApplyDraft"
          class="settings-diff-review"
        >
          <UAlert
            color="success"
            variant="soft"
            icon="i-lucide-shield-check"
            title="配置验证已通过"
            description="即将基于精确基线创建完整 Configuration Revision。Worker 与 Provider 结果仍会在应用后逐项观测。"
          />
          <dl class="settings-diff-identity">
            <div><dt>设置分区</dt><dd>{{ settingsSectionLabel(pendingApplyDraft.key) }}</dd></div>
            <div><dt>基线 Revision</dt><dd>#{{ pendingApplyDraft.baseRevisionNumber }}</dd></div>
            <div><dt>Exact hash</dt><dd><code>{{ pendingApplyDraft.baseRevisionHash }}</code></dd></div>
            <div><dt>变更说明</dt><dd>{{ pendingApplyDraft.summary }}</dd></div>
          </dl>
          <ol class="settings-diff-list">
            <li
              v-for="(change, index) in settingsSectionChanges(pendingApplyDraft)"
              :key="`${change}-${index}`"
            >
              <span>{{ String(index + 1).padStart(2, '0') }}</span>
              <p>{{ change }}</p>
            </li>
          </ol>
          <p class="settings-diff-note">
            恢复方式：创建后续 Configuration Revision；历史版本不会被原位改写。
          </p>
        </div>
      </template>
      <template #footer>
        <div class="settings-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="返回编辑"
            @click="applyConfirmationOpen = false"
          />
          <UButton
            color="primary"
            icon="i-lucide-upload-cloud"
            label="应用配置"
            @click="confirmApply"
          />
        </div>
      </template>
    </UModal>

    <USlideover
      :open="providerEditorOpen"
      side="right"
      :overlay="true"
      :modal="true"
      :transition="true"
      :ui="providerSlideoverUI"
      :close="true"
      :title="selectedProviderConfiguration ? `编辑 ${providerLabels[selectedProviderConfiguration.provider]}` : '编辑 Provider'"
      :description="selectedProviderConfiguration ? providerDescriptions[selectedProviderConfiguration.provider] : 'Provider 配置'"
      @update:open="closeProviderEditor"
    >
      <template #body>
        <div
          v-if="selectedProviderConfiguration && selectedProviderEditor"
          class="settings-provider-editor"
        >
          <div class="settings-provider-editor__status">
            <UBadge
              :color="stateColor(providerState(selectedProviderConfiguration.provider))"
              variant="soft"
              :label="stateLabel(providerState(selectedProviderConfiguration.provider))"
            />
            <span>{{ providerSecretCount(selectedProviderConfiguration.provider) }} 个 Secret 引用</span>
            <span>最近检查：{{ providerCheckedAt(selectedProviderConfiguration.provider) }}</span>
          </div>
          <Transition
            name="settings-panel"
            mode="out-in"
          >
            <Suspense :key="selectedProviderConfiguration.provider">
              <component
                :is="selectedProviderEditor"
                :model-value="selectedProviderConfiguration"
                @update:model-value="updateSelectedProviderConfiguration"
              />
              <template #fallback>
                <UAlert
                  color="neutral"
                  variant="soft"
                  icon="i-lucide-loader-circle"
                  title="正在加载 Provider 编辑器"
                  :description="`${providerLabels[selectedProviderConfiguration.provider]} 编辑区域正在按需加载。`"
                />
              </template>
            </Suspense>
          </Transition>
          <UAlert
            v-if="providerResults[selectedProviderConfiguration.provider]"
            :color="stateColor(providerResults[selectedProviderConfiguration.provider]!.state)"
            variant="soft"
            icon="i-lucide-flask-conical"
            :title="`最近一次本地测试：${stateLabel(providerResults[selectedProviderConfiguration.provider]!.state)}`"
            :description="providerResults[selectedProviderConfiguration.provider]!.detail"
          />
        </div>
      </template>
      <template #footer>
        <div
          v-if="selectedProviderConfiguration"
          class="settings-provider-editor__footer"
        >
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-flask-conical"
            label="测试连接"
            @click="openProviderTest(selectedProviderConfiguration.provider)"
          />
          <UButton
            color="primary"
            icon="i-lucide-check"
            label="完成编辑"
            @click="providerEditorOpen = false"
          />
        </div>
      </template>
    </USlideover>

    <UModal
      :open="providerTestOpen"
      title="执行 Provider test？"
      description="该请求会使用当前本地 Provider 配置、已引用的 Secret version 和默认 Scope 访问目标 Provider；它不会 apply Configuration Revision。"
      :dismissible="!providerTesting"
      :close="!providerTesting"
      :ui="providerTestModalUI"
      @update:open="closeProviderTest"
    >
      <template #body>
        <div
          v-if="providerTestConfiguration"
          class="settings-modal-body"
        >
          <UAlert
            color="warning"
            variant="soft"
            icon="i-lucide-flask-conical"
            title="独立 Provider 请求"
            description="成功只代表本次 test 返回可用，不代表草稿已发布，也不代表 Worker 已观察。"
          />
          <dl>
            <div><dt>Provider</dt><dd>{{ providerLabels[providerTestConfiguration.provider] }}</dd></div>
            <div><dt>Endpoint</dt><dd>{{ providerTestConfiguration.endpoint || "服务端默认" }}</dd></div>
            <div><dt>Timeout</dt><dd>{{ providerTestConfiguration.timeout_ms }} ms</dd></div>
            <div><dt>Secret references</dt><dd>{{ secretReferencesValue?.filter((item) => item.provider === providerTestConfiguration?.provider).length ?? 0 }}</dd></div>
          </dl>
          <UAlert
            v-if="providerTestError"
            color="error"
            variant="soft"
            icon="i-lucide-circle-x"
            title="Provider test 失败"
            :description="providerTestError"
          />
        </div>
      </template>
      <template #footer>
        <div class="settings-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="取消"
            :disabled="providerTesting"
            @click="closeProviderTest(false)"
          />
          <UButton
            color="warning"
            icon="i-lucide-flask-conical"
            label="执行一次 test"
            :loading="providerTesting"
            @click="confirmProviderTest"
          />
        </div>
      </template>
    </UModal>

    <UModal
      :open="secretModalOpen"
      title="创建新的 Secret version"
      description="Secret value 是 write-only；请求开始前会从页面响应式状态清除，失败也不会回填。"
      :dismissible="!secretSaving"
      :close="!secretSaving"
      @update:open="closeSecretModal"
    >
      <template #body>
        <div class="settings-modal-body">
          <UAlert
            color="warning"
            variant="soft"
            icon="i-lucide-key-round"
            title="独立 Secret 写入"
            description="创建成功后只把返回的 version reference 加入本地 section 草稿；仍需单独 validate 和 apply。"
          />
          <UFormField
            label="Provider"
            name="secret-provider"
          >
            <USelect
              v-model="secretState.provider"
              :items="providerOptions"
              value-key="value"
              class="settings-control"
            />
          </UFormField>
          <UFormField
            label="Purpose"
            name="secret-purpose"
          >
            <UInput
              v-model="secretState.purpose"
              class="settings-control"
              autocomplete="off"
              spellcheck="false"
            />
          </UFormField>
          <UFormField
            label="Secret value"
            name="secret-value"
          >
            <UInput
              v-model="secretState.value"
              type="password"
              class="settings-control"
              autocomplete="new-password"
              spellcheck="false"
            />
          </UFormField>
          <UAlert
            v-if="secretError"
            color="error"
            variant="soft"
            icon="i-lucide-circle-x"
            title="Secret version 未创建"
            :description="secretError"
          />
        </div>
      </template>
      <template #footer>
        <div class="settings-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-arrow-left"
            label="取消"
            :disabled="secretSaving"
            @click="closeSecretModal(false)"
          />
          <UButton
            color="warning"
            icon="i-lucide-key-round"
            label="创建 version"
            :loading="secretSaving"
            :disabled="!secretState.value"
            @click="saveSecret"
          />
        </div>
      </template>
    </UModal>

    <UModal
      :open="draftRecoveryOpen"
      :title="draftRecovery?.status === 'expired' ? 'Settings 草稿已超过 24 小时' : '发现未完成的 Settings 草稿'"
      :description="draftRecovery ? `${draftRecoveryDiffs.length} 个 section · 保存于 ${formatISO(new Date(draftRecovery.payload.savedAt).toISOString())}` : '本地草稿恢复'"
      :dismissible="false"
      :close="false"
    >
      <template #body>
        <div
          v-if="draftRecovery"
          class="settings-diff-review"
        >
          <UAlert
            v-if="draftRecovery.status === 'expired'"
            color="warning"
            variant="soft"
            icon="i-lucide-clock-alert"
            title="草稿已过期"
            description="超过 24 小时的草稿只能查看 Diff 或丢弃，不能恢复、验证或应用。"
          />
          <UAlert
            v-else-if="draftRecoveryConflicts"
            color="warning"
            variant="soft"
            icon="i-lucide-git-compare-arrows"
            title="活动 Revision 已变化"
            description="恢复会保留草稿的原始 base Revision/hash；页面不会自动合并、rebase、验证或 apply。恢复后需逐 section 明确处理冲突。"
          />
          <UAlert
            v-else
            color="info"
            variant="soft"
            icon="i-lucide-file-clock"
            title="仅保存非敏感 allowlist 字段"
            description="Secret value、Token、凭据、校验结果与自动 apply 状态均未进入持久草稿。"
          />
          <template v-if="draftRecoveryDiffVisible">
            <section
              v-for="section in draftRecoveryDiffs"
              :key="section.key"
              class="settings-diff-review"
            >
              <dl class="settings-diff-identity">
                <div><dt>设置分区</dt><dd>{{ section.label }}</dd></div>
                <div><dt>基线 Revision</dt><dd>#{{ section.baseRevisionNumber }}</dd></div>
                <div><dt>变更说明</dt><dd>{{ section.summary || '未填写' }}</dd></div>
              </dl>
              <ol class="settings-diff-list">
                <li
                  v-for="(change, index) in section.changes"
                  :key="`${section.key}-${index}`"
                >
                  <span>{{ String(index + 1).padStart(2, '0') }}</span>
                  <p>{{ change }}</p>
                </li>
              </ol>
            </section>
          </template>
        </div>
      </template>
      <template #footer>
        <div class="settings-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            :icon="draftRecoveryDiffVisible ? 'i-lucide-eye-off' : 'i-lucide-file-diff'"
            :label="draftRecoveryDiffVisible ? '收起 Diff' : '查看 Diff'"
            @click="draftRecoveryDiffVisible = !draftRecoveryDiffVisible"
          />
          <UButton
            color="error"
            variant="outline"
            icon="i-lucide-trash-2"
            label="丢弃草稿"
            @click="discardPersistedDraftRecovery"
          />
          <UButton
            v-if="draftRecovery?.status === 'fresh'"
            color="primary"
            :icon="draftRecoveryConflicts ? 'i-lucide-git-compare-arrows' : 'i-lucide-history'"
            :label="draftRecoveryConflicts ? '恢复并处理冲突' : '恢复草稿'"
            @click="restorePersistedDraftRecovery"
          />
        </div>
      </template>
    </UModal>

    <UModal
      :open="leaveModalOpen"
      title="如何处理本地 Settings 草稿？"
      description="这些 section 草稿尚未产生后端副作用；可以保留非敏感字段 24 小时后离开。"
      :dismissible="false"
      :close="false"
    >
      <template #body>
        <UAlert
          color="warning"
          variant="soft"
          icon="i-lucide-triangle-alert"
          title="存在未应用修改"
          :description="`${dirtySectionCount} 个 section 存在本地修改；选择保留或放弃都不会改写活动 Revision、Provider 和 Scope。`"
        />
      </template>
      <template #footer>
        <div class="settings-modal-actions">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-pencil"
            label="继续编辑"
            @click="stayOnSettings"
          />
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-save"
            label="保留 24 小时并离开"
            @click="preserveAndLeave"
          />
          <UButton
            color="error"
            icon="i-lucide-log-out"
            label="放弃并离开"
            @click="discardAndLeave"
          />
        </div>
      </template>
    </UModal>
  </article>
</template>

<style scoped>
.settings-view { display: grid; width: 100%; min-width: 0; gap: var(--co-space-5); margin-inline: auto; padding-bottom: var(--co-page-end-space); }
.settings-page-heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-5); }
.settings-heading-copy { min-width: 0; }
.settings-page-heading h1 { margin: 0; font-size: clamp(24px, 2.2vw, 32px); letter-spacing: 0; }
.settings-page-heading p:not(.settings-eyebrow) { max-width: 64ch; margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 13px; }
.settings-eyebrow { margin: 0 0 var(--co-space-1); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.settings-page-actions { display: flex; flex: 0 0 auto; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.settings-command-row { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-4); padding-block: var(--co-space-2); }
.settings-search-wrap { position: relative; min-width: min(100%, 420px); flex: 1 1 420px; }
.settings-search { width: 100%; }
.settings-search-results { position: absolute; top: calc(100% + 6px); right: 0; left: 0; z-index: calc(var(--co-z-sticky) + 2); display: grid; max-height: 320px; overflow: auto; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-floating); box-shadow: var(--co-shadow-floating); }
.settings-search-results button { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-3); padding: var(--co-space-3); border: 0; border-bottom: 1px solid var(--co-border-subtle); background: transparent; color: var(--co-text-primary); text-align: left; cursor: pointer; }
.settings-search-results button:hover, .settings-search-results button:focus-visible { background: var(--co-bg-hover); outline: none; }
.settings-search-results button > span { display: grid; min-width: 0; gap: 2px; }
.settings-search-results small, .settings-search-empty { color: var(--co-text-muted); font-size: 11px; }
.settings-search-empty { margin: var(--co-space-2) 0 0; }
.settings-workbench { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr); align-items: start; gap: clamp(var(--co-space-5), 2vw, var(--co-space-8)); }
.settings-navigation { display: grid; min-width: 0; gap: var(--co-space-3); }
.settings-navigation__heading { display: flex; min-width: 0; align-items: baseline; justify-content: space-between; gap: var(--co-space-3); padding: 0 var(--co-space-2); }
.settings-navigation__heading span { color: var(--co-text-muted); font-size: 10px; }
.settings-navigation__heading strong { font-size: 13px; }
.settings-section-nav { display: grid; min-width: 0; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--co-space-4); }
.settings-section-nav-group { display: grid; min-width: 0; gap: 4px; }
.settings-section-nav-group__label { padding: 0 var(--co-space-2) var(--co-space-1); color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; letter-spacing: .04em; text-transform: uppercase; }
.settings-section-link { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); min-height: 60px; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-control); background: var(--co-bg-surface); color: var(--co-text-secondary); text-align: left; cursor: pointer; transition: border-color var(--co-motion-fast) var(--co-ease-out), background var(--co-motion-fast) var(--co-ease-out), color var(--co-motion-fast) var(--co-ease-out); }
.settings-section-link:hover, .settings-section-link:focus-visible { background: var(--co-bg-hover); color: var(--co-text-primary); outline: none; }
.settings-section-link.is-active { border-color: var(--co-focus-ring); background: var(--co-bg-floating); color: var(--co-text-primary); box-shadow: inset 0 3px 0 var(--co-focus-ring); }
.settings-section-link > span { display: grid; min-width: 0; gap: 2px; }
.settings-section-link strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.settings-section-link small { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.settings-navigation__baseline { display: flex; min-width: 0; flex-wrap: wrap; align-items: baseline; gap: var(--co-space-2) var(--co-space-4); padding: var(--co-space-3); border-top: 1px solid var(--co-border-default); background: var(--co-bg-subtle); }
.settings-navigation__baseline span, .settings-navigation__baseline small { color: var(--co-text-muted); font-size: 10px; }
.settings-navigation__baseline strong { font-size: 12px; }
.settings-current { display: grid; min-width: 0; gap: var(--co-space-4); }
.settings-pending { position: static; display: grid; min-width: 0; gap: var(--co-space-4); padding: var(--co-space-5); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.settings-pending > header { display: grid; gap: 2px; padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-subtle); }
.settings-pending > header > span { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; font-weight: 800; text-transform: uppercase; }
.settings-pending h2 { margin: 0; font-size: 16px; }
.settings-pending header p, .settings-pending__empty, .settings-pending__truth { margin: 0; color: var(--co-text-muted); font-size: 10px; line-height: 1.5; }
.settings-pending__count { display: flex; align-items: baseline; gap: var(--co-space-2); }
.settings-pending__count strong { font-family: var(--co-font-mono); font-size: 28px; line-height: 1; font-variant-numeric: tabular-nums; }
.settings-pending__count span { color: var(--co-text-muted); font-size: 10px; }
.settings-pending__sections { display: grid; gap: 4px; }
.settings-pending__sections button { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-2); padding: var(--co-space-2) var(--co-space-3); border: 1px solid transparent; border-radius: var(--co-radius-control); background: var(--co-bg-canvas); color: var(--co-text-secondary); text-align: left; cursor: pointer; }
.settings-pending__sections button:hover, .settings-pending__sections button:focus-visible { border-color: var(--co-border-default); color: var(--co-text-primary); outline: none; }
.settings-pending__sections button.is-active { border-color: var(--co-border-default); background: var(--co-bg-floating); color: var(--co-text-primary); box-shadow: inset 3px 0 0 var(--co-focus-ring); }
.settings-pending__changes { display: grid; gap: var(--co-space-2); max-height: 220px; margin: 0; padding: 0; overflow-y: auto; list-style: none; }
.settings-pending__changes li { display: grid; gap: 2px; padding: var(--co-space-2); border-radius: var(--co-radius-control); background: var(--co-bg-canvas); color: var(--co-text-secondary); font-size: 10px; overflow-wrap: anywhere; }
.settings-pending__changes li small { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 9px; }
.settings-pending__facts { display: grid; margin: 0; }
.settings-pending__facts div { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-2); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-subtle); }
.settings-pending__facts dt { color: var(--co-text-muted); font-size: 10px; }
.settings-pending__facts dd { margin: 0; color: var(--co-text-primary); font-size: 10px; font-weight: 700; text-align: right; }
.settings-pending__actions { display: grid; gap: var(--co-space-2); }
.settings-pending__actions :deep(button) { width: 100%; justify-content: center; }
.active-revision-band { display: grid; min-width: 0; grid-template-columns: minmax(260px, .8fr) minmax(0, 1.2fr); align-items: center; gap: var(--co-space-6); padding: var(--co-space-5); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-panel); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.active-revision-band h2, .settings-readonly-section h2 { margin: 0; font-size: 24px; letter-spacing: 0; }
.active-revision-band > div > p:not(.settings-eyebrow) { margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 12px; }
.active-revision-summary { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 11px; }
.active-revision-band dl, .settings-modal-body dl { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; }
.active-revision-band dl div, .settings-modal-body dl div { min-width: 0; padding: var(--co-space-2) var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.active-revision-band dt, .settings-modal-body dt, .settings-facts-grid dt { color: var(--co-text-muted); font-size: 10px; }
.active-revision-band dd, .settings-modal-body dd, .settings-facts-grid dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-primary); overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
.settings-form-grid { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.settings-form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.settings-form-grid--policy { grid-template-columns: 1fr; gap: var(--co-space-5); }
.settings-form-grid--policy .settings-span-two { grid-column: auto; }
.settings-form-grid--provider { grid-template-columns: minmax(0, 1fr) minmax(220px, .55fr); }
.settings-span-two { grid-column: span 2; }
.settings-control { width: 100%; }
.settings-toggle-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); padding-block: var(--co-space-2); }
.settings-component-group { display: grid; min-width: 0; gap: var(--co-space-4); padding-block: var(--co-space-2) var(--co-space-5); border-bottom: 1px solid var(--co-border-default); }
.settings-component-group:last-child { padding-bottom: 0; border-bottom: 0; }
.settings-component-group__heading { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-4); }
.settings-component-group__heading h3 { margin: 0; font-size: 16px; }
.settings-component-group__heading p:not(.settings-eyebrow) { max-width: 68ch; margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 12px; }
.settings-setting-list { display: grid; min-width: 0; gap: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-canvas); }
.settings-setting-row { display: grid; min-width: 0; grid-template-columns: minmax(0, 1fr) minmax(180px, 300px); align-items: center; gap: var(--co-space-6); padding: var(--co-space-4); border-bottom: 1px solid var(--co-border-default); }
.settings-setting-row:last-child { border-bottom: 0; }
.settings-setting-copy { min-width: 0; }
.settings-setting-copy strong { display: block; font-size: 14px; }
.settings-setting-copy p { margin: 3px 0 0; color: var(--co-text-secondary); font-size: 12px; }
.settings-setting-copy small { display: block; margin-top: 5px; color: var(--co-text-muted); font-size: 10px; }
.settings-setting-field { min-width: 0; }
.settings-setting-field :deep(label) { color: var(--co-text-muted); font-size: 10px; }
.settings-setting-control { width: 100%; }
.settings-setting-row--toggle { grid-template-columns: minmax(0, 1fr) auto; min-height: 76px; }
.settings-setting-list--toggles { background: var(--co-bg-surface); }
.settings-item__identity { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.settings-item__index { color: var(--co-text-muted); font-family: var(--co-font-mono); font-size: 10px; font-weight: 750; text-transform: uppercase; }
.settings-item__header--scope, .settings-item__header--policy { min-height: 42px; }
.settings-repeated-list--scopes, .settings-repeated-list--policies { grid-template-columns: 1fr; }
.settings-list-heading { display: flex; min-width: 0; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.settings-list-heading > span { color: var(--co-text-muted); font-size: 11px; }
.settings-repeated-list { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-3); }
.settings-item { display: grid; min-width: 0; align-content: start; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.settings-item > header { display: flex; min-width: 0; min-height: 36px; align-items: center; gap: var(--co-space-2); padding-bottom: var(--co-space-2); border-bottom: 1px solid var(--co-border-default); }
.settings-item > header > :last-child { margin-left: auto; }
.settings-item > header > div { display: grid; min-width: 0; }
.settings-item > header code { color: var(--co-text-muted); font-size: 10px; }
.settings-checkbox-row { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-3); }
.provider-item-footer { display: flex; min-width: 0; align-items: flex-end; justify-content: space-between; gap: var(--co-space-3); margin-top: auto; padding-top: var(--co-space-2); border-top: 1px solid var(--co-border-default); }
.settings-provider-workbench { display: grid; min-width: 0; grid-template-columns: minmax(240px, .44fr) minmax(0, 1fr); gap: var(--co-space-3); }
.provider-summary-list { display: grid; min-width: 0; align-content: start; gap: 1px; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-border-default); }
.provider-summary-row { display: grid; min-width: 0; gap: 4px; padding: var(--co-space-3); border: 0; background: var(--co-bg-canvas); color: var(--co-text-secondary); text-align: left; cursor: pointer; }
.provider-summary-row:hover, .provider-summary-row:focus-visible, .provider-summary-row.is-selected { background: var(--co-bg-floating); color: var(--co-text-primary); outline: none; }
.provider-summary-row.is-selected { box-shadow: inset 3px 0 0 var(--co-focus-ring); }
.provider-summary-row header, .provider-summary-name { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.provider-summary-row header { justify-content: space-between; }
.provider-summary-name { overflow: hidden; }
.provider-summary-name strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.provider-summary-name code { color: var(--co-text-muted); font-size: 10px; }
.provider-state-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--co-text-muted); }
.provider-state-dot[data-state="available"] { background: var(--co-status-success-fg); }
.provider-state-dot[data-state="partial"] { background: var(--co-status-warning-fg); }
.provider-state-dot[data-state="unavailable"] { background: var(--co-status-critical-fg); }
.provider-summary-row p { margin: 0; overflow: hidden; color: inherit; font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }
.provider-summary-row small { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.provider-detail-panel { display: grid; min-width: 0; align-content: start; gap: var(--co-space-4); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); }
.provider-detail-header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.provider-detail-header h3 { margin: 0; font-size: 18px; }
.provider-detail-header p:not(.settings-eyebrow) { margin: 2px 0 0; color: var(--co-text-secondary); font-size: 11px; }
.provider-detail-status { display: flex; flex: 0 0 auto; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); }
.provider-result { display: flex; min-width: 0; align-items: center; gap: var(--co-space-2); }
.provider-result span, .provider-result-empty { color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.secret-reference-list { display: grid; min-width: 0; gap: 0; margin: 0; padding: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); list-style: none; }
.secret-reference-list li { display: grid; min-width: 0; grid-template-columns: minmax(180px, 1fr) minmax(220px, 1fr) auto; align-items: center; gap: var(--co-space-3); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.secret-reference-list li > div { display: grid; }
.secret-reference-list span { color: var(--co-status-success-fg); font-size: 10px; }
.secret-reference-list code { min-width: 0; color: var(--co-text-muted); overflow-wrap: anywhere; font-size: 10px; }
.settings-readonly-section { display: grid; min-width: 0; gap: var(--co-space-4); scroll-margin-top: 76px; padding-block: var(--co-space-5); border-top: 1px solid var(--co-border-default); outline: none; }
.settings-readonly-section:focus-visible { box-shadow: inset 3px 0 0 var(--co-focus-ring); }
.settings-readonly-section > header { display: flex; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); }
.settings-table-scroll { min-width: 0; overflow-x: auto; }
.revision-table { min-width: 900px; }
.settings-facts-grid { display: grid; min-width: 0; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); }
.settings-facts-grid--diagnostics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
.settings-facts-grid div { min-width: 0; padding: var(--co-space-3); border-right: 1px solid var(--co-border-default); border-bottom: 1px solid var(--co-border-default); }
.settings-modal-body { display: grid; min-width: 0; gap: var(--co-space-4); }
.settings-modal-actions { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
@media (max-width: 1500px) {
  .settings-setting-row { gap: var(--co-space-4); }
  .settings-repeated-list { grid-template-columns: 1fr; }
  .settings-provider-workbench { grid-template-columns: 1fr; }
}
@media (max-width: 1100px) {
  .settings-section-nav { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 820px) {
  .settings-page-heading, .provider-item-footer { align-items: stretch; flex-direction: column; }
  .settings-page-actions { justify-content: flex-start; }
  .settings-command-row { align-items: stretch; flex-direction: column; }
  .settings-search-wrap { min-width: 0; }
  .settings-section-nav { grid-template-columns: 1fr; }
  .settings-section-link.is-active { box-shadow: inset 0 3px 0 var(--co-focus-ring); }
  .active-revision-band { grid-template-columns: 1fr; }
  .active-revision-summary { justify-content: flex-start; }
  .settings-form-grid, .settings-form-grid--three, .settings-toggle-grid { grid-template-columns: 1fr; }
  .settings-setting-row { grid-template-columns: 1fr; gap: var(--co-space-3); }
  .settings-setting-row--toggle { grid-template-columns: minmax(0, 1fr) auto; }
  .settings-span-two { grid-column: auto; }
  .settings-facts-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .secret-reference-list li { grid-template-columns: 1fr auto; }
  .secret-reference-list code { grid-column: 1 / -1; }
}
@media (max-width: 520px) {
  .settings-page-heading h1 { font-size: 24px; }
  .settings-page-actions, .settings-page-actions :deep(button), .settings-modal-actions, .settings-modal-actions :deep(button) { width: 100%; }
  .settings-modal-actions { flex-direction: column; }
  .active-revision-band dl, .settings-modal-body dl, .settings-facts-grid, .settings-facts-grid--diagnostics { grid-template-columns: 1fr; }
}
</style>

<style scoped>
.settings-view {
  width: min(100%, 1320px);
  height: 100%;
  min-height: 0;
  grid-template-rows: auto minmax(0, 1fr);
  gap: var(--co-space-6);
  padding-bottom: 0;
  overflow: hidden;
}

.settings-page-heading {
  align-items: center;
  padding-bottom: var(--co-space-4);
}

.settings-page-heading h1 { font-size: 28px; }
.settings-page-heading p:not(.settings-eyebrow) { max-width: 720px; }

.settings-workbench {
  min-height: 0;
  grid-template-columns: 256px minmax(0, 1040px);
  align-items: stretch;
  justify-content: center;
  gap: var(--co-space-8);
  padding-top: var(--co-space-5);
  overflow: hidden;
  border-top: 1px solid var(--co-border-default);
}

.settings-navigation {
  position: static;
  height: 100%;
  min-height: 0;
  grid-template-rows: auto auto minmax(0, 1fr) auto;
  align-self: stretch;
  gap: var(--co-space-3);
  padding-right: var(--co-space-5);
  border-right: 1px solid var(--co-border-default);
}

.settings-navigation__heading { padding: 0 var(--co-space-2); }
.settings-search-wrap { min-width: 0; flex: none; }
.settings-search-results { right: auto; width: min(420px, calc(100vw - 48px)); }

.settings-section-nav {
  position: relative;
  display: grid;
  min-height: 0;
  grid-template-columns: 1fr;
  align-content: start;
  gap: var(--co-space-3);
  padding-right: var(--co-space-1);
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.settings-nav-indicator {
  position: absolute;
  top: 0;
  right: 0;
  left: 0;
  z-index: 0;
  border: 1px solid var(--co-border-default);
  border-radius: 9px;
  background: var(--co-bg-floating);
  box-shadow: var(--co-shadow-row);
  pointer-events: none;
  transition:
    height var(--co-motion-standard) var(--co-ease-out),
    opacity var(--co-motion-fast) var(--co-ease-out),
    transform var(--co-motion-standard) var(--co-ease-out);
}

.settings-section-nav-group { position: relative; z-index: 1; gap: 2px; }
.settings-section-nav-group__label { padding: var(--co-space-1) var(--co-space-2); }

.settings-section-link {
  position: relative;
  z-index: 1;
  min-height: 48px;
  padding: var(--co-space-2) var(--co-space-3);
  border-color: transparent;
  border-radius: 9px;
  background: transparent;
  transition: color var(--co-motion-fast) var(--co-ease-out);
}

.settings-section-link:hover,
.settings-section-link:focus-visible { border-color: transparent; background: color-mix(in srgb, var(--co-bg-hover) 72%, transparent); }
.settings-section-link.is-active { border-color: transparent; background: transparent; box-shadow: none; color: var(--co-text-primary); }
.settings-section-link.is-active :deep(svg) { color: var(--co-action-primary); }
.settings-section-link small { font-size: 9px; }

.settings-navigation__baseline {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: var(--co-space-2);
  margin-top: var(--co-space-2);
  padding: var(--co-space-3) var(--co-space-2);
  background: transparent;
}
.settings-navigation__baseline > div { display: grid; gap: 1px; }
.settings-baseline-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--co-status-success-fg); box-shadow: 0 0 0 3px color-mix(in srgb, var(--co-status-success-fg) 14%, transparent); }

.settings-current {
  width: 100%;
  height: 100%;
  min-height: 0;
  max-width: 1040px;
  align-content: start;
  gap: var(--co-space-4);
  padding-right: var(--co-space-2);
  padding-bottom: 112px;
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}

.settings-context-bar {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding-bottom: var(--co-space-3);
  border-bottom: 1px solid var(--co-border-subtle);
}
.settings-context-bar > div { display: grid; gap: 2px; }
.settings-context-bar strong { font-size: 13px; }
.settings-context-bar span { color: var(--co-text-muted); font-size: 10px; }
.settings-current :deep(.workspace-technical-details) { width: min(100%, 820px); }

.settings-panel { display: grid; width: 100%; min-width: 0; }
.settings-panel > form { width: min(100%, 820px); }
.settings-panel > .settings-readonly-section { width: 100%; max-width: 1040px; }
.settings-panel-enter-active,
.settings-panel-leave-active {
  transition:
    opacity var(--co-motion-fast) var(--co-ease-out),
    transform var(--co-motion-standard) var(--co-ease-out);
}
.settings-panel-enter-from { opacity: 0; transform: translateY(6px); }
.settings-panel-leave-to { opacity: 0; transform: translateY(-2px); }

.settings-component-group { padding-block: 0 var(--co-space-5); }
.settings-setting-list { border-radius: 10px; background: transparent; }
.settings-setting-row { grid-template-columns: minmax(0, 1fr) minmax(200px, 280px); gap: var(--co-space-5); padding: var(--co-space-3) var(--co-space-4); }
.settings-setting-row--toggle { min-height: 68px; }
.settings-setting-list--toggles { background: transparent; }

.settings-domain-editor {
  display: grid;
  min-width: 0;
  grid-template-columns: 220px minmax(0, 1fr);
  align-items: start;
  gap: var(--co-space-5);
}

.settings-object-list {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: 2px;
  padding-right: var(--co-space-4);
  border-right: 1px solid var(--co-border-default);
}
.settings-object-list > header { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-2); min-height: 40px; padding: 0 var(--co-space-2) var(--co-space-2); }
.settings-object-list > header > div { display: grid; gap: 1px; }
.settings-object-list > header strong { font-size: 12px; }
.settings-object-list > header span { color: var(--co-text-muted); font-size: 10px; }
.settings-object-button {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--co-space-2);
  padding: var(--co-space-2);
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--co-text-secondary);
  text-align: left;
  cursor: pointer;
  transition: background var(--co-motion-fast) var(--co-ease-out), color var(--co-motion-fast) var(--co-ease-out);
}
.settings-object-button:hover,
.settings-object-button:focus-visible { background: var(--co-bg-hover); color: var(--co-text-primary); outline: none; }
.settings-object-button.is-active { background: var(--co-bg-surface); color: var(--co-text-primary); }
.settings-object-button > span:nth-child(2) { display: grid; min-width: 0; gap: 1px; }
.settings-object-button strong,
.settings-object-button small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.settings-object-button small { color: var(--co-text-muted); font-size: 9px; }
.settings-object-button__icon { display: grid; width: 28px; height: 28px; place-items: center; border-radius: 7px; background: var(--co-bg-canvas); color: var(--co-text-muted); }
.settings-object-button__badges { display: flex; flex-direction: column; gap: 2px; }

.settings-object-editor { display: grid; min-width: 0; gap: var(--co-space-5); }
.settings-object-editor > header { display: flex; min-width: 0; align-items: flex-start; justify-content: space-between; gap: var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.settings-object-editor h3 { margin: 0; font-size: 17px; }
.settings-object-editor > header p:not(.settings-eyebrow) { margin: 2px 0 0; color: var(--co-text-secondary); font-size: 11px; }
.settings-object-editor__actions { display: flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-2); }

.scope-effective-preview { display: grid; gap: var(--co-space-3); padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: 10px; background: var(--co-bg-subtle); }
.scope-effective-preview > div:first-child { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.scope-effective-preview span { color: var(--co-text-muted); font-size: 10px; }
.scope-effective-preview strong { font-family: var(--co-font-mono); font-size: 10px; }
.scope-namespace-list { display: flex; flex-wrap: wrap; gap: var(--co-space-1); }

.policy-stage-preview { display: grid; min-width: 0; grid-template-columns: repeat(3, minmax(0, 1fr) auto) minmax(0, 1fr); align-items: center; gap: var(--co-space-2); }
.policy-stage-preview > div { display: grid; min-width: 0; gap: 2px; padding: var(--co-space-3); border: 1px solid var(--co-border-default); border-radius: 9px; background: var(--co-bg-subtle); }
.policy-stage-preview > div > span { color: var(--co-action-primary); font-family: var(--co-font-mono); font-size: 9px; }
.policy-stage-preview strong { font-size: 11px; }
.policy-stage-preview small { overflow: hidden; color: var(--co-text-muted); font-size: 9px; text-overflow: ellipsis; white-space: nowrap; }
.policy-stage-preview > :deep(svg) { color: var(--co-text-muted); }

.settings-provider-workbench { grid-template-columns: 1fr; }
.provider-summary-list { gap: 0; border-radius: 10px; background: transparent; }
.provider-summary-row {
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: var(--co-space-3);
  min-height: 76px;
  padding: var(--co-space-3) var(--co-space-4);
  border-bottom: 1px solid var(--co-border-default);
  background: transparent;
}
.provider-summary-row:last-child { border-bottom: 0; }
.provider-summary-row:hover,
.provider-summary-row:focus-visible { background: var(--co-bg-hover); }
.provider-summary-icon { display: grid; width: 38px; height: 38px; place-items: center; border: 1px solid var(--co-border-default); border-radius: 9px; background: var(--co-bg-canvas); color: var(--co-text-secondary); }
.provider-summary-content { display: grid; min-width: 0; gap: 3px; }
.provider-summary-content header { display: flex; align-items: center; justify-content: space-between; gap: var(--co-space-3); }
.provider-summary-content p { margin: 0; color: var(--co-text-secondary); font-size: 11px; }
.provider-summary-meta { display: flex; min-width: 0; flex-wrap: wrap; gap: var(--co-space-1) var(--co-space-3); color: var(--co-text-muted); font-size: 9px; }
.provider-summary-meta span:first-child { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

.settings-readonly-section { padding-block: var(--co-space-3) var(--co-space-5); border-top: 0; border-bottom: 1px solid var(--co-border-default); }
.settings-readonly-section h2 { font-size: 22px; }
.revision-table { min-width: 760px; }
.settings-facts-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); border-radius: 10px; }

.secret-reference-list { border-radius: 10px; }
.secret-reference-list li { min-height: 58px; }

.settings-action-bar {
  position: fixed;
  right: max(var(--co-page-gutter), calc((100vw - var(--co-sidebar-width) - 1320px) / 2));
  bottom: max(16px, env(safe-area-inset-bottom));
  z-index: calc(var(--co-z-sticky) + 3);
  display: flex;
  width: min(1040px, calc(100vw - var(--co-sidebar-width) - 256px - (3 * var(--co-page-gutter))));
  min-height: 64px;
  align-items: center;
  justify-content: space-between;
  gap: var(--co-space-4);
  padding: var(--co-space-2) var(--co-space-3);
  border: 1px solid var(--co-border-strong);
  border-radius: 12px;
  background: color-mix(in srgb, var(--co-bg-floating) 94%, transparent);
  box-shadow: var(--co-shadow-floating);
  backdrop-filter: blur(14px);
}
.settings-action-status { display: flex; min-width: 0; align-items: center; gap: var(--co-space-3); }
.settings-action-status > div { display: grid; min-width: 0; gap: 2px; }
.settings-action-status strong { font-size: 12px; }
.settings-action-status span:not(.settings-action-status__dot) { color: var(--co-text-muted); font-size: 9px; }
.settings-action-status__dot { width: 9px; height: 9px; flex: 0 0 auto; border-radius: 50%; background: var(--co-status-warning-fg); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-status-warning-fg) 12%, transparent); }
.settings-action-status__dot.is-valid { background: var(--co-status-success-fg); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-status-success-fg) 12%, transparent); }
.settings-action-status__dot.is-invalid { background: var(--co-status-critical-fg); box-shadow: 0 0 0 4px color-mix(in srgb, var(--co-status-critical-fg) 12%, transparent); }
.settings-action-buttons { display: flex; flex: 0 0 auto; align-items: center; gap: var(--co-space-2); }
.settings-actions-enter-active,
.settings-actions-leave-active { transition: opacity var(--co-motion-standard) var(--co-ease-out), transform var(--co-motion-standard) var(--co-ease-out); }
.settings-actions-enter-from,
.settings-actions-leave-to { opacity: 0; transform: translateY(8px); }

.settings-diff-review { display: grid; min-width: 0; gap: var(--co-space-4); }
.settings-diff-identity { display: grid; margin: 0; }
.settings-diff-identity > div { display: grid; min-width: 0; grid-template-columns: 120px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-2) 0; border-bottom: 1px solid var(--co-border-default); }
.settings-diff-identity dt { color: var(--co-text-muted); font-size: 10px; }
.settings-diff-identity dd { min-width: 0; margin: 0; font-size: 11px; overflow-wrap: anywhere; }
.settings-diff-list { display: grid; gap: 0; margin: 0; padding: 0; border: 1px solid var(--co-border-default); border-radius: 10px; list-style: none; }
.settings-diff-list li { display: grid; grid-template-columns: 32px minmax(0, 1fr); gap: var(--co-space-3); padding: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.settings-diff-list li:last-child { border-bottom: 0; }
.settings-diff-list span { color: var(--co-action-primary); font-family: var(--co-font-mono); font-size: 9px; }
.settings-diff-list p { margin: 0; color: var(--co-text-secondary); font-size: 11px; }
.settings-diff-note { margin: 0; color: var(--co-text-muted); font-size: 10px; }

.settings-view.is-provider-editor-open .settings-current,
.settings-view.is-provider-editor-open .settings-section-nav { overflow: hidden; }

:global(.settings-provider-overlay) {
  position: fixed !important;
  inset: 0 !important;
  z-index: calc(var(--co-z-overlay) + 10) !important;
  background: rgb(20 18 15 / 28%) !important;
  backdrop-filter: blur(1px);
  isolation: isolate;
}

:global(.settings-provider-slideover) {
  position: fixed !important;
  inset: 0 0 0 auto !important;
  z-index: calc(var(--co-z-overlay) + 11) !important;
  display: grid !important;
  width: min(660px, calc(100vw - var(--co-sidebar-rail-width))) !important;
  max-width: 660px !important;
  height: 100dvh !important;
  max-height: 100dvh !important;
  grid-template-rows: auto minmax(0, 1fr) auto;
  overflow: hidden !important;
  border: 0 !important;
  border-left: 1px solid var(--co-border-default) !important;
  border-radius: 12px 0 0 12px !important;
  background: var(--co-bg-overlay) !important;
  box-shadow: -20px 0 48px rgb(20 18 15 / 18%) !important;
  opacity: 1 !important;
  isolation: isolate;
  contain: paint;
}

:global(.settings-provider-test-overlay) {
  z-index: calc(var(--co-z-overlay) + 12) !important;
}

:global(.settings-provider-test-modal) {
  z-index: calc(var(--co-z-overlay) + 13) !important;
}

:global(.settings-provider-slideover__header),
:global(.settings-provider-slideover__footer) {
  position: relative;
  z-index: 2;
  flex: none !important;
  background: var(--co-bg-overlay) !important;
}

:global(.settings-provider-slideover__header) {
  min-height: var(--co-header-height);
  padding: var(--co-space-4) var(--co-space-5) !important;
  border-bottom: 1px solid var(--co-border-default);
}

:global(.settings-provider-slideover__body) {
  width: 100%;
  min-width: 0;
  min-height: 0;
  padding: var(--co-space-5) !important;
  overflow-x: hidden !important;
  overflow-y: auto !important;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
  background: var(--co-bg-overlay) !important;
}

:global(.settings-provider-slideover__footer) {
  min-height: 68px;
  justify-content: flex-end;
  padding: var(--co-space-3) var(--co-space-5) !important;
  border-top: 1px solid var(--co-border-default);
  box-shadow: 0 -8px 24px rgb(20 18 15 / 4%);
}

.settings-provider-editor { display: grid; min-width: 0; gap: var(--co-space-5); }
.settings-provider-editor__status { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; gap: var(--co-space-2) var(--co-space-3); padding-bottom: var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.settings-provider-editor__status span { color: var(--co-text-muted); font-size: 10px; }
.settings-provider-editor__footer { display: flex; width: 100%; justify-content: flex-end; gap: var(--co-space-2); }
:global(.provider-type-settings) { display: grid; min-width: 0; gap: var(--co-space-5); }

@media (max-width: 1180px) {
  .settings-workbench { grid-template-columns: 224px minmax(0, 1fr); gap: var(--co-space-5); }
  .settings-action-bar { right: var(--co-page-gutter); width: calc(100vw - var(--co-sidebar-width) - 224px - (3 * var(--co-page-gutter))); }
}

@media (max-width: 1024px) {
  .settings-workbench { grid-template-columns: 1fr; grid-template-rows: auto minmax(0, 1fr); }
  .settings-navigation { height: auto; grid-template-rows: auto auto auto; padding: 0 0 var(--co-space-4); overflow: visible; border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .settings-section-nav { display: flex; padding-right: 0; padding-bottom: var(--co-space-1); overflow-x: auto; overflow-y: hidden; scrollbar-gutter: auto; }
  .settings-nav-indicator { display: none; }
  .settings-section-nav-group { display: flex; flex: 0 0 auto; }
  .settings-section-nav-group__label { display: none; }
  .settings-section-link { min-width: 150px; border-color: var(--co-border-default); }
  .settings-section-link.is-active { border-color: var(--co-border-strong); background: var(--co-bg-floating); }
  .settings-navigation__baseline { display: none; }
  .settings-search-wrap { max-width: 420px; }
  .settings-action-bar { right: var(--co-page-gutter); left: var(--co-page-gutter); width: auto; }
}

@media (max-width: 760px) {
  .settings-page-heading,
  .settings-context-bar,
  .settings-action-bar,
  .settings-object-editor > header { align-items: stretch; flex-direction: column; }
  .settings-page-actions { justify-content: flex-start; }
  .settings-domain-editor { grid-template-columns: 1fr; }
  .settings-object-list { grid-template-columns: repeat(2, minmax(0, 1fr)); padding: 0 0 var(--co-space-3); border-right: 0; border-bottom: 1px solid var(--co-border-default); }
  .settings-object-list > header { grid-column: 1 / -1; }
  .settings-setting-row,
  .settings-setting-row--toggle { grid-template-columns: 1fr; gap: var(--co-space-3); }
  .policy-stage-preview { grid-template-columns: 1fr; }
  .policy-stage-preview > :deep(svg) { transform: rotate(90deg); justify-self: center; }
  .settings-action-buttons { width: 100%; flex-wrap: wrap; }
  .settings-action-buttons :deep(button) { flex: 1 1 auto; }
  .settings-facts-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  :global(.settings-provider-slideover) { width: 100vw !important; max-width: 100vw !important; border-radius: 0 !important; }
}

@media (prefers-reduced-motion: reduce) {
  .settings-nav-indicator,
  .settings-section-link,
  .settings-object-button,
  .settings-panel-enter-active,
  .settings-panel-leave-active,
  .settings-actions-enter-active,
  .settings-actions-leave-active { transition: none; }
  :global(.settings-provider-overlay),
  :global(.settings-provider-slideover) { animation: none !important; transition: none !important; }
  .settings-panel-enter-from,
  .settings-panel-leave-to,
  .settings-actions-enter-from,
  .settings-actions-leave-to { transform: none; }
}
</style>
