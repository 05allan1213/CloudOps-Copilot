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
import USwitch from "@nuxt/ui/components/Switch.vue";
import UTable from "@nuxt/ui/components/Table.vue";
import UTextarea from "@nuxt/ui/components/Textarea.vue";
import UTabs from "@nuxt/ui/components/Tabs.vue";
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
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
import RiskConfirmation from "../../components/workspace/RiskConfirmation.vue";
import WorkspaceTechnicalDetails from "../../components/workspace/WorkspaceTechnicalDetails.vue";
import type { RiskConfirmationFacts } from "../../components/workspace/workspacePresentation";
import { OPERATIONAL_SCOPE_CHANGED_EVENT } from "../../utils/operationalScope";
import SettingsSectionPanel from "./SettingsSectionPanel.vue";
import {
  buildSectionConfigurationDraft,
  classifySettingsApplyOutcome,
  createSettingsSectionDraft,
  createSettingsSectionDrafts,
  isSettingsSectionDirty,
  rebaseSettingsSection,
  resetSettingsSection,
  settingsSectionFingerprint,
  settingsSectionKeys,
  settingsSectionLabel,
  validateSettingsSectionLocally,
  type ScopeSectionValue,
  type SettingsApplyOutcome,
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

type SettingsMode = "simple" | "full";
interface SettingsSectionLink {
  key: SettingsViewSection;
  label: string;
  hash: string;
  icon: string;
  description: string;
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
const settingsMode = ref<SettingsMode>("simple");
const settingsSearch = ref("");
const selectedProvider = ref<ProviderIdentity | null>(null);
let mounted = true;

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

const modeTabs = [
  { label: "简洁", value: "simple", icon: "i-lucide-layout-list" },
  { label: "完整", value: "full", icon: "i-lucide-panels-top-left" },
];

const activeRevision = computed(() => settings.value?.active_revision ?? null);
const systemValue = computed(() => drafts.value?.system.value as GeneralConfiguration | undefined);
const scopesValue = computed(() => drafts.value?.scopes.value as ScopeSectionValue | undefined);
const policiesValue = computed(() => drafts.value?.policies.value as EscalationPolicy[] | undefined);
const providersValue = computed(() => drafts.value?.providers.value as ProviderConfiguration[] | undefined);
const secretReferencesValue = computed(() => drafts.value?.["secret-references"].value as SecretReference[] | undefined);
const hasUnsavedChanges = computed(() => Boolean(
  drafts.value && settingsSectionKeys.some((key) => isSettingsSectionDirty(drafts.value![key])),
));
const dirtySectionCount = computed(() => (
  drafts.value ? settingsSectionKeys.filter((key) => isSettingsSectionDirty(drafts.value![key])).length : 0
));
const activeSectionKey = computed<SettingsViewSection>(() => (
  resolveSettingsViewSection(route.hash)
));
const providerHealthByID = computed(() => new Map(
  (settings.value?.provider_health ?? []).map((item) => [item.provider, item]),
));
const selectedProviderConfiguration = computed(() => (
  providersValue.value?.find((item) => item.provider === selectedProvider.value) ?? providersValue.value?.[0]
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
    { key: "system", label: "Telemetry 保留", field: "system.telemetry_retention_days", text: "数据保留天数" },
    { key: "scopes", label: "Cluster identity", field: "scopes.0.cluster_id", text: "运行范围与集群" },
    { key: "policies", label: "Severity", field: "policies.0.severities", text: "告警升级级别" },
    { key: "providers", label: "Endpoint", field: "providers.llm.endpoint", text: "Provider 连接地址" },
    { key: "secret-references", label: "Secret reference", field: "secret-references", text: "只读 Secret 元数据" },
  ];
  return results.concat(filterSettingsSearchEntries(query, fields));
});
const providerTestConfiguration = computed(() => (
  providersValue.value?.find((item) => item.provider === providerTestTarget.value) ?? null
));
const pendingApplyDraft = computed(() => (
  pendingApplySection.value && drafts.value ? drafts.value[pendingApplySection.value] : null
));
const applyFacts = computed<RiskConfirmationFacts>(() => {
  const section = pendingApplyDraft.value;
  if (!section) return { target: "", effect: "", recovery: "" };
  return {
    target: `${settingsSectionLabel(section.key)}，base Revision #${section.baseRevisionNumber}`,
    effect: "后端将基于该精确基线创建完整 Configuration Revision；Worker 与 Provider 逐项观测不构成原子成功。",
    exactHash: section.baseRevisionHash,
    recovery: "通过后续 Configuration Revision 恢复；历史 Revision 不会被原位改写。",
  };
});

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
  const link = sectionLinks.find((item) => item.key === key);
  if (link) void router.push({ path: "/settings", hash: link.hash });
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

function providerStateDetail(provider: ProviderIdentity): string {
  return providerHealthByID.value.get(provider)?.detail ?? "尚未收到服务端健康投影";
}

function secondsToHours(value: number): number {
  return Math.max(1, Math.round(value / 3600));
}

function hoursToSeconds(value: unknown): number {
  return Math.max(60, Number(value || 1) * 3600);
}

function millisecondsToSeconds(value: number): number {
  return Math.max(1, Math.round(value / 1000));
}

function secondsToMilliseconds(value: unknown): number {
  return Math.max(1000, Number(value || 1) * 1000);
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
  for (const key of settingsSectionKeys) clearSectionRuntime(key);
  providerResults.value = {};
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

async function loadSettings(initial = false) {
  loading.value = true;
  loadError.value = "";
  try {
    const [snapshot, storageStatus] = await Promise.all([getSettings(), getStorageStatus()]);
    if (!mounted) return;
    settings.value = snapshot;
    storage.value = storageStatus;
    if (initial || !drafts.value) initializeDrafts(snapshot.active_revision);
    else reconcileCleanDrafts(snapshot.active_revision);
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

    const applied = await applyConfiguration(validation.id, buildSectionConfigurationDraft(section));
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
  void nextTick(() => focusField(`scopes.${scopesValue.value!.scopes.length - 1}.name`));
}

function removeScope(index: number) {
  if (!scopesValue.value || scopesValue.value.scopes.length <= 1) return;
  scopesValue.value.scopes.splice(index, 1);
  if (scopesValue.value.defaultIndex > index) scopesValue.value.defaultIndex -= 1;
  else if (scopesValue.value.defaultIndex === index) {
    scopesValue.value.defaultIndex = Math.min(index, scopesValue.value.scopes.length - 1);
  }
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
  void nextTick(() => focusField(`policies.${policiesValue.value!.length - 1}.name`));
}

function removePolicy(index: number) {
  policiesValue.value?.splice(index, 1);
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
  event.preventDefault();
  event.returnValue = "";
}

function discardAllDrafts() {
  if (!drafts.value) return;
  for (const key of settingsSectionKeys) {
    drafts.value[key] = resetSettingsSection(drafts.value[key]);
    clearSectionRuntime(key, true);
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

function handleOperationalScopeChanged() {
  void loadSettings(false);
}

onBeforeRouteLeave((to) => {
  if (!shouldBlockSettingsLeave(hasUnsavedChanges.value)) return true;
  pendingRoute.value = to.fullPath;
  leaveModalOpen.value = true;
  return false;
});

watch(() => route.hash, (hash) => void focusRouteAnchor(hash));
watch(() => secretState.provider, (provider) => {
  secretState.purpose = provider === "llm" ? "api_key" : provider === "kubernetes" ? "credential" : "token";
  secretState.value = "";
});

onMounted(() => {
  window.addEventListener("beforeunload", handleBeforeUnload);
  window.addEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
  void loadSettings(true);
});

onBeforeUnmount(() => {
  mounted = false;
  secretState.value = "";
  window.removeEventListener("beforeunload", handleBeforeUnload);
  window.removeEventListener(OPERATIONAL_SCOPE_CHANGED_EVENT, handleOperationalScopeChanged);
});
</script>

<template>
  <article
    class="settings-view"
    aria-labelledby="settings-title"
  >
    <header class="settings-page-heading">
      <div class="settings-heading-copy">
        <p class="settings-eyebrow">
          Configuration control
        </p>
        <h1 id="settings-title">
          设置中心
        </h1>
        <p>一次查看一个配置分区；本地草稿、校验和 Revision 结果始终保留。</p>
      </div>
      <div class="settings-page-actions">
        <UBadge
          :color="hasUnsavedChanges ? 'warning' : 'success'"
          variant="soft"
          :icon="hasUnsavedChanges ? 'i-lucide-pencil-line' : 'i-lucide-circle-check'"
          :label="hasUnsavedChanges ? `${dirtySectionCount} 个 section 有本地修改` : '所有 section 与各自基线一致'"
        />
        <UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          label="刷新只读状态"
          :loading="loading"
          @click="loadSettings(false)"
        />
      </div>
    </header>

    <div class="settings-command-row">
      <div class="settings-search-wrap">
        <UInput
          v-model="settingsSearch"
          icon="i-lucide-search"
          size="lg"
          placeholder="搜索设置并跳转..."
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
      <UTabs
        v-model="settingsMode"
        :items="modeTabs"
        :content="false"
        color="neutral"
        variant="pill"
        size="sm"
        aria-label="设置显示模式"
      />
    </div>

    <nav
      class="settings-section-nav"
      aria-label="Settings 分区"
    >
      <button
        v-for="item in sectionLinks"
        :key="item.key"
        type="button"
        :class="['settings-section-link', { 'is-active': activeSectionKey === item.key }]"
        :aria-current="activeSectionKey === item.key ? 'page' : undefined"
        @click="selectSection(item.key)"
      >
        <UIcon
          :name="item.icon"
          aria-hidden="true"
        />
        <span><strong>{{ item.label }}</strong><small>{{ item.description }}</small></span>
        <UBadge
          v-if="sectionErrorCounts[item.key]"
          color="error"
          variant="soft"
          :label="String(sectionErrorCounts[item.key])"
        />
      </button>
    </nav>

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
          @click="loadSettings(false)"
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

    <template v-if="settings && drafts && activeRevision">
      <section
        class="active-revision-band"
        aria-labelledby="active-revision-heading"
      >
        <div>
          <p class="settings-eyebrow">
            Active configuration
          </p>
          <h2 id="active-revision-heading">
            Revision #{{ activeRevision.number }}
          </h2>
          <p>{{ activeRevision.summary }}</p>
        </div>
        <div class="active-revision-summary">
          <UBadge
            color="success"
            variant="soft"
            icon="i-lucide-check-circle-2"
            label="当前生效"
          />
          <span>{{ formatISO(activeRevision.created_at) }}</span>
          <span>{{ activationLabel(activeRevision.worker_boundary?.status) }}</span>
          <UButton
            color="neutral"
            variant="ghost"
            icon="i-lucide-braces"
            label="技术详情"
            @click="settingsMode = 'full'"
          />
        </div>
        <WorkspaceTechnicalDetails
          v-if="settingsMode === 'full'"
          :fields="[
            { label: 'Revision ID', value: activeRevision.id, code: true, copyValue: activeRevision.id },
            { label: 'Exact hash', value: activeRevision.hash, code: true, copyValue: activeRevision.hash },
            { label: 'Created at (UTC)', value: formatISO(activeRevision.created_at), code: true },
            { label: 'Worker boundary', value: activationLabel(activeRevision.worker_boundary?.status) },
          ]"
        />
      </section>

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
          <div class="settings-form-grid settings-form-grid--three">
            <UFormField
              label="最大回看时间（小时）"
              name="system.query_max_lookback_seconds"
              :data-field="'system.query_max_lookback_seconds'"
            >
              <UInputNumber
                :model-value="secondsToHours(systemValue.query_max_lookback_seconds)"
                :min="1"
                :max="720"
                :step="1"
                class="settings-control"
                @update:model-value="systemValue.query_max_lookback_seconds = hoursToSeconds($event)"
              />
            </UFormField>
            <UFormField
              label="查询结果上限"
              name="system.query_max_results"
              :data-field="'system.query_max_results'"
            >
              <UInputNumber
                v-model="systemValue.query_max_results"
                :min="1"
                :max="10000"
                :step="10"
                class="settings-control"
              />
            </UFormField>
            <UFormField
              label="Telemetry 保留天数"
              name="system.telemetry_retention_days"
              :data-field="'system.telemetry_retention_days'"
            >
              <UInputNumber
                v-model="systemValue.telemetry_retention_days"
                :min="1"
                :max="365"
                class="settings-control"
              />
            </UFormField>
          </div>
          <div class="settings-toggle-grid">
            <USwitch
              v-model="systemValue.browser_notifications_enabled"
              label="浏览器提醒"
              description="启用时由浏览器单独请求 Notification 权限。"
              @change="handleBrowserNotificationToggle"
            />
            <USwitch
              v-model="systemValue.automatic_escalation_enabled"
              label="自动 escalation"
              description="仍受活动 Escalation Policy 和服务端边界约束。"
            />
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
          <div class="settings-list-heading">
            <span>{{ scopesValue.scopes.length }} / 10 scopes</span>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-plus"
              label="添加 Scope"
              :disabled="scopesValue.scopes.length >= 10"
              @click="addScope"
            />
          </div>
          <div class="settings-repeated-list">
            <article
              v-for="(scope, index) in scopesValue.scopes.slice(0, settingsMode === 'simple' ? 1 : undefined)"
              :key="scope.id || `scope-${index}`"
              class="settings-item"
            >
              <header>
                <UCheckbox
                  :model-value="scopesValue.defaultIndex === index"
                  label="Revision 默认 Scope"
                  @update:model-value="setDefaultScope(index, Boolean($event))"
                />
                <UBadge
                  v-if="scope.active"
                  color="success"
                  variant="soft"
                  label="当前活动"
                />
                <UButton
                  color="error"
                  variant="ghost"
                  icon="i-lucide-trash-2"
                  square
                  :aria-label="`从本地草稿移除 Scope ${scope.name || index + 1}`"
                  :disabled="scopesValue.scopes.length <= 1"
                  @click="removeScope(index)"
                />
              </header>
              <div class="settings-form-grid">
                <UFormField
                  label="名称"
                  :name="`scopes.${index}.name`"
                  :data-field="`scopes.${index}.name`"
                >
                  <UInput
                    v-model="scope.name"
                    class="settings-control"
                    autocomplete="off"
                  />
                </UFormField>
                <UFormField
                  label="Cluster identity"
                  :name="`scopes.${index}.cluster_id`"
                  :data-field="`scopes.${index}.cluster_id`"
                >
                  <UInput
                    v-model="scope.cluster_id"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                  />
                </UFormField>
                <UFormField
                  label="Environment"
                  :name="`scopes.${index}.environment`"
                  :data-field="`scopes.${index}.environment`"
                >
                  <UInput
                    v-model="scope.environment"
                    class="settings-control"
                    autocomplete="off"
                  />
                </UFormField>
                <UFormField
                  label="Namespaces"
                  :name="`scopes.${index}.namespaces`"
                  :data-field="`scopes.${index}.namespaces`"
                  help="逗号分隔。"
                >
                  <UInput
                    :model-value="scope.namespaces.join(', ')"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                    @update:model-value="setScopeNamespaces(index, String($event))"
                  />
                </UFormField>
              </div>
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
            class="settings-repeated-list"
          >
            <article
              v-for="(policy, index) in policiesValue.slice(0, settingsMode === 'simple' ? 3 : undefined)"
              :key="policy.id || `policy-${index}`"
              class="settings-item"
            >
              <header>
                <USwitch
                  v-model="policy.enabled"
                  :label="policy.enabled ? '已启用' : '已停用'"
                />
                <UBadge
                  color="info"
                  variant="soft"
                  label="创建 Incident"
                />
                <UButton
                  color="error"
                  variant="ghost"
                  icon="i-lucide-trash-2"
                  square
                  :aria-label="`从本地草稿移除 Policy ${policy.name || index + 1}`"
                  @click="removePolicy(index)"
                />
              </header>
              <div class="settings-form-grid">
                <UFormField
                  class="settings-span-two"
                  label="Policy 名称"
                  :name="`policies.${index}.name`"
                  :data-field="`policies.${index}.name`"
                >
                  <UInput
                    v-model="policy.name"
                    class="settings-control"
                    autocomplete="off"
                  />
                </UFormField>
                <UFormField
                  class="settings-span-two"
                  label="Severity"
                  :name="`policies.${index}.severities`"
                  :data-field="`policies.${index}.severities`"
                >
                  <div class="settings-checkbox-row">
                    <UCheckbox
                      v-for="severity in severityOptions"
                      :key="severity"
                      :model-value="policy.severities.includes(severity)"
                      :label="severity"
                      @update:model-value="togglePolicySeverity(index, severity, Boolean($event))"
                    />
                  </div>
                </UFormField>
                <UFormField
                  label="持续 firing（秒）"
                  :name="`policies.${index}.minimum_firing_seconds`"
                  :data-field="`policies.${index}.minimum_firing_seconds`"
                >
                  <UInputNumber
                    v-model="policy.minimum_firing_seconds"
                    :min="0"
                    :max="604800"
                    class="settings-control"
                  />
                </UFormField>
                <UFormField
                  label="最小复发次数"
                  :name="`policies.${index}.minimum_recurrence_count`"
                  :data-field="`policies.${index}.minimum_recurrence_count`"
                >
                  <UInputNumber
                    v-model="policy.minimum_recurrence_count"
                    :min="1"
                    :max="100"
                    class="settings-control"
                  />
                </UFormField>
                <UFormField
                  class="settings-span-two"
                  label="Namespaces"
                  :name="`policies.${index}.namespaces`"
                  :data-field="`policies.${index}.namespaces`"
                  help="逗号分隔；留空表示不限。"
                >
                  <UInput
                    :model-value="policy.namespaces.join(', ')"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                    @update:model-value="setPolicyNamespaces(index, String($event))"
                  />
                </UFormField>
                <UFormField
                  class="settings-span-two"
                  label="Exact label matchers"
                  :name="`policies.${index}.label_matchers`"
                  :data-field="`policies.${index}.label_matchers`"
                  help="每行 name=value。"
                >
                  <UTextarea
                    :model-value="policyMatchersText(policy)"
                    :rows="3"
                    class="settings-control"
                    spellcheck="false"
                    @update:model-value="setPolicyMatchers(index, String($event))"
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
                :class="['provider-summary-row', { 'is-selected': selectedProviderConfiguration?.provider === provider.provider }]"
                role="listitem"
                tabindex="0"
                @click="selectedProvider = provider.provider"
                @keydown.enter.prevent="selectedProvider = provider.provider"
              >
                <header>
                  <div class="provider-summary-name">
                    <span
                      class="provider-state-dot"
                      :data-state="providerState(provider.provider)"
                    /><strong>{{ providerLabels[provider.provider] }}</strong><code>{{ provider.provider }}</code>
                  </div>
                  <UBadge
                    :color="stateColor(providerState(provider.provider))"
                    variant="soft"
                    :label="stateLabel(providerState(provider.provider))"
                  />
                </header>
                <p>{{ providerStateDetail(provider.provider) }}</p>
                <small>{{ provider.enabled ? '配置已启用' : '配置已停用' }} · {{ provider.endpoint || '服务端默认 Endpoint' }}</small>
              </article>
            </div>
            <article
              v-if="selectedProviderConfiguration"
              class="provider-detail-panel"
            >
              <header class="provider-detail-header">
                <div>
                  <p class="settings-eyebrow">
                    Provider detail
                  </p>
                  <h3>{{ providerLabels[selectedProviderConfiguration.provider] }}</h3>
                  <p>{{ providerStateDetail(selectedProviderConfiguration.provider) }}</p>
                </div>
                <USwitch
                  v-model="selectedProviderConfiguration.enabled"
                  :label="selectedProviderConfiguration.enabled ? '已启用' : '已停用'"
                />
              </header>
              <div class="settings-form-grid">
                <UFormField
                  class="settings-span-two"
                  label="Endpoint"
                  :name="`providers.${selectedProviderConfiguration.provider}.endpoint`"
                  :data-field="`providers.${selectedProviderConfiguration.provider}.endpoint`"
                >
                  <UInput
                    v-model="selectedProviderConfiguration.endpoint"
                    type="url"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                  />
                </UFormField>
                <UFormField
                  v-if="selectedProviderConfiguration.provider === 'llm'"
                  class="settings-span-two"
                  label="Model"
                  name="providers.llm.model"
                  data-field="providers.llm.model"
                >
                  <UInput
                    v-model="selectedProviderConfiguration.model"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                  />
                </UFormField>
                <UFormField
                  label="Timeout（秒）"
                  :name="`providers.${selectedProviderConfiguration.provider}.timeout_ms`"
                  :data-field="`providers.${selectedProviderConfiguration.provider}.timeout_ms`"
                >
                  <UInputNumber
                    :model-value="millisecondsToSeconds(selectedProviderConfiguration.timeout_ms)"
                    :min="1"
                    :max="60"
                    :step="1"
                    class="settings-control"
                    @update:model-value="selectedProviderConfiguration.timeout_ms = secondsToMilliseconds($event)"
                  />
                </UFormField>
                <UFormField
                  label="结果上限"
                  :name="`providers.${selectedProviderConfiguration.provider}.max_results`"
                  :data-field="`providers.${selectedProviderConfiguration.provider}.max_results`"
                >
                  <UInputNumber
                    v-model="selectedProviderConfiguration.max_results"
                    :min="1"
                    :max="10000"
                    :step="10"
                    class="settings-control"
                  />
                </UFormField>
                <UFormField
                  class="settings-span-two"
                  label="Context Link base"
                  :name="`providers.${selectedProviderConfiguration.provider}.context_link_base`"
                  :data-field="`providers.${selectedProviderConfiguration.provider}.context_link_base`"
                >
                  <UInput
                    v-model="selectedProviderConfiguration.context_link_base"
                    type="url"
                    class="settings-control"
                    autocomplete="off"
                    spellcheck="false"
                  />
                </UFormField>
              </div>
              <footer class="provider-item-footer">
                <div
                  v-if="providerResults[selectedProviderConfiguration.provider]"
                  class="provider-result"
                >
                  <UBadge
                    :color="stateColor(providerResults[selectedProviderConfiguration.provider]!.state)"
                    variant="soft"
                    :label="stateLabel(providerResults[selectedProviderConfiguration.provider]!.state)"
                  />
                  <span>{{ providerResults[selectedProviderConfiguration.provider]!.detail }}</span>
                </div>
                <span
                  v-else
                  class="provider-result-empty"
                >尚未测试当前本地 Provider 草稿</span>
                <UButton
                  color="neutral"
                  variant="outline"
                  icon="i-lucide-flask-conical"
                  label="审阅 Provider test"
                  @click="openProviderTest(selectedProviderConfiguration.provider)"
                />
              </footer>
            </article>
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
    </template>

    <RiskConfirmation
      :open="applyConfirmationOpen"
      kind="configuration"
      :facts="applyFacts"
      @update:open="applyConfirmationOpen = $event"
      @confirm="confirmApply"
    />

    <UModal
      :open="providerTestOpen"
      title="执行 Provider test？"
      description="该请求会使用当前本地 Provider 配置、已引用的 Secret version 和默认 Scope 访问目标 Provider；它不会 apply Configuration Revision。"
      :dismissible="!providerTesting"
      :close="!providerTesting"
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
      :open="leaveModalOpen"
      title="离开并放弃本地 Settings 草稿？"
      description="这些 section 草稿只存在于当前前端页面，尚未产生后端副作用。"
      :dismissible="false"
      :close="false"
    >
      <template #body>
        <UAlert
          color="warning"
          variant="soft"
          icon="i-lucide-triangle-alert"
          title="存在未应用修改"
          :description="`${dirtySectionCount} 个 section 的本地修改会被放弃；活动 Revision、Provider 和 Scope 不会被改写。`"
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
.settings-view { display: grid; width: min(100%, 1240px); min-width: 0; gap: var(--co-space-5); margin-inline: auto; padding-bottom: var(--co-page-end-space); }
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
.settings-section-nav { position: sticky; top: var(--co-header-height); z-index: var(--co-z-sticky); display: grid; min-width: 0; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 1px; padding: 1px; overflow: hidden; border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-border-default); box-shadow: var(--co-shadow-chrome); }
.settings-section-link { display: grid; min-width: 0; grid-template-columns: auto minmax(0, 1fr) auto; align-items: center; gap: var(--co-space-2); min-height: 58px; padding: var(--co-space-2) var(--co-space-3); border: 0; background: var(--co-bg-canvas); color: var(--co-text-secondary); text-align: left; cursor: pointer; transition: background var(--co-motion-fast) ease, color var(--co-motion-fast) ease; }
.settings-section-link:hover, .settings-section-link:focus-visible { background: var(--co-bg-hover); color: var(--co-text-primary); outline: none; }
.settings-section-link.is-active { background: var(--co-bg-floating); color: var(--co-text-primary); box-shadow: inset 0 -2px 0 var(--co-focus-ring); }
.settings-section-link > span { display: grid; min-width: 0; gap: 2px; }
.settings-section-link strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.settings-section-link small { overflow: hidden; color: var(--co-text-muted); font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.active-revision-band { display: grid; min-width: 0; grid-template-columns: minmax(220px, .8fr) minmax(0, 1.2fr); align-items: center; gap: var(--co-space-5); padding: var(--co-space-4); border: 1px solid var(--co-border-default); border-radius: var(--co-radius-frame); background: var(--co-bg-surface); box-shadow: var(--co-shadow-row); }
.active-revision-band h2, .settings-readonly-section h2 { margin: 0; font-size: 18px; letter-spacing: 0; }
.active-revision-band > div > p:not(.settings-eyebrow) { margin: var(--co-space-1) 0 0; color: var(--co-text-secondary); font-size: 12px; }
.active-revision-summary { display: flex; min-width: 0; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--co-space-2); color: var(--co-text-muted); font-size: 11px; }
.active-revision-band dl, .settings-modal-body dl { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); margin: 0; }
.active-revision-band dl div, .settings-modal-body dl div { min-width: 0; padding: var(--co-space-2) var(--co-space-3); border-bottom: 1px solid var(--co-border-default); }
.active-revision-band dt, .settings-modal-body dt, .settings-facts-grid dt { color: var(--co-text-muted); font-size: 10px; }
.active-revision-band dd, .settings-modal-body dd, .settings-facts-grid dd { min-width: 0; margin: 3px 0 0; color: var(--co-text-primary); overflow-wrap: anywhere; font-family: var(--co-font-mono); font-size: 10px; }
.settings-form-grid { display: grid; min-width: 0; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); }
.settings-form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.settings-span-two { grid-column: span 2; }
.settings-control { width: 100%; }
.settings-toggle-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--co-space-4); padding-block: var(--co-space-2); }
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
@media (max-width: 1100px) {
  .settings-section-nav { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .settings-repeated-list { grid-template-columns: 1fr; }
  .settings-provider-workbench { grid-template-columns: 1fr; }
}
@media (max-width: 820px) {
  .settings-page-heading, .provider-item-footer { align-items: stretch; flex-direction: column; }
  .settings-page-actions { justify-content: flex-start; }
  .settings-command-row { align-items: stretch; flex-direction: column; }
  .settings-search-wrap { min-width: 0; }
  .settings-section-nav { position: static; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .active-revision-band { grid-template-columns: 1fr; }
  .active-revision-summary { justify-content: flex-start; }
  .settings-form-grid, .settings-form-grid--three, .settings-toggle-grid { grid-template-columns: 1fr; }
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
