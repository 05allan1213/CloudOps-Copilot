import {
  configurationDraft,
  type ConfigurationDraft,
  type ConfigurationRevision,
  type EscalationPolicy,
  type GeneralConfiguration,
  type ProviderConfiguration,
  type ProviderHealth,
  type SecretReference,
} from "../../api/platform";

export const settingsSectionKeys = ["system", "scopes", "policies", "providers", "secret-references"] as const;

export type SettingsSectionKey = typeof settingsSectionKeys[number];

export interface ScopeSectionValue {
  defaultIndex: number;
  scopes: ConfigurationDraft["scopes"];
}

export type SettingsSectionValue =
  | GeneralConfiguration
  | ScopeSectionValue
  | EscalationPolicy[]
  | ProviderConfiguration[]
  | SecretReference[];

export interface SettingsSectionDraft {
  key: SettingsSectionKey;
  baseRevisionID: string;
  baseRevisionNumber: number;
  baseRevisionHash: string;
  baseDraft: ConfigurationDraft;
  baseline: SettingsSectionValue;
  value: SettingsSectionValue;
  summary: string;
}

export type SettingsSectionDrafts = Record<SettingsSectionKey, SettingsSectionDraft>;

export interface SettingsApplyItemResult {
  id: string;
  label: string;
  state: "waiting" | "running" | "succeeded" | "partial" | "failed" | "unknown" | "disabled";
  detail: string;
  observedAt?: string;
}

export interface SettingsApplyOutcome {
  state: "accepted" | "running" | "succeeded" | "partial" | "failed" | "unknown";
  title: string;
  description: string;
  items: SettingsApplyItemResult[];
}

const sectionLabels: Record<SettingsSectionKey, string> = {
  system: "系统边界",
  scopes: "Operational Scope",
  policies: "Escalation Policy",
  providers: "Provider 配置",
  "secret-references": "Secret reference",
};

const generalLabels: Record<keyof GeneralConfiguration, string> = {
  query_max_lookback_seconds: "最大回看时间",
  query_max_results: "查询结果上限",
  telemetry_retention_days: "Telemetry 保留天数",
  browser_notifications_enabled: "浏览器提醒",
  automatic_escalation_enabled: "自动 escalation",
};

export function settingsSectionLabel(key: SettingsSectionKey): string {
  return sectionLabels[key];
}

export function createSettingsSectionDrafts(revision: ConfigurationRevision): SettingsSectionDrafts {
  return Object.fromEntries(settingsSectionKeys.map((key) => [key, createSettingsSectionDraft(revision, key)])) as SettingsSectionDrafts;
}

export function createSettingsSectionDraft(
  revision: ConfigurationRevision,
  key: SettingsSectionKey,
): SettingsSectionDraft {
  const baseDraft = configurationDraft(revision);
  const baseline = readSectionValue(baseDraft, key);
  return {
    key,
    baseRevisionID: revision.id,
    baseRevisionNumber: revision.number,
    baseRevisionHash: revision.hash,
    baseDraft,
    baseline,
    value: clone(baseline),
    summary: "",
  };
}

export function resetSettingsSection(section: SettingsSectionDraft): SettingsSectionDraft {
  return {
    ...section,
    value: clone(section.baseline),
    summary: "",
  };
}

export function rebaseSettingsSection(
  section: SettingsSectionDraft,
  revision: ConfigurationRevision,
  preserveLocalValue: boolean,
): SettingsSectionDraft {
  const next = createSettingsSectionDraft(revision, section.key);
  if (!preserveLocalValue) return next;
  return {
    ...next,
    value: clone(section.value),
    summary: section.summary,
  };
}

export function sectionChangedInRevision(
  section: SettingsSectionDraft,
  revision: ConfigurationRevision,
): boolean {
  const nextValue = readSectionValue(configurationDraft(revision), section.key);
  return serialize(nextValue) !== serialize(section.baseline);
}

export function isSettingsSectionDirty(section: SettingsSectionDraft): boolean {
  return section.summary.trim().length > 0 || hasSettingsSectionValueChanges(section);
}

export function hasSettingsSectionValueChanges(section: SettingsSectionDraft): boolean {
  return serialize(section.value) !== serialize(section.baseline);
}

export function settingsSectionFingerprint(section: SettingsSectionDraft): string {
  return serialize({ summary: section.summary.trim(), value: section.value });
}

export function buildSectionConfigurationDraft(section: SettingsSectionDraft): ConfigurationDraft {
  const result = clone(section.baseDraft);
  result.summary = section.summary.trim();
  switch (section.key) {
    case "system":
      result.general = clone(section.value as GeneralConfiguration);
      break;
    case "scopes": {
      const value = section.value as ScopeSectionValue;
      result.scopes = clone(value.scopes);
      const index = clampIndex(value.defaultIndex, result.scopes.length);
      result.scope = clone(result.scopes[index] ?? result.scope);
      break;
    }
    case "policies":
      result.escalation_policies = clone(section.value as EscalationPolicy[]);
      break;
    case "providers":
      result.providers = clone(section.value as ProviderConfiguration[]);
      break;
    case "secret-references":
      result.secret_references = sanitizeSecretReferences(section.value as SecretReference[]);
      break;
  }
  return result;
}

export function settingsSectionChanges(section: SettingsSectionDraft): string[] {
  if (!hasSettingsSectionValueChanges(section)) return [];
  switch (section.key) {
    case "system":
      return generalChanges(
        section.baseline as GeneralConfiguration,
        section.value as GeneralConfiguration,
      );
    case "scopes":
      return scopeChanges(
        section.baseline as ScopeSectionValue,
        section.value as ScopeSectionValue,
      );
    case "policies":
      return collectionChanges(
        section.baseline as EscalationPolicy[],
        section.value as EscalationPolicy[],
        policyIdentity,
        "Policy",
      );
    case "providers":
      return providerChanges(
        section.baseline as ProviderConfiguration[],
        section.value as ProviderConfiguration[],
      );
    case "secret-references":
      return secretReferenceChanges(
        section.baseline as SecretReference[],
        section.value as SecretReference[],
      );
  }
}

export function validateSettingsSectionLocally(section: SettingsSectionDraft): Array<{ name: string; message: string }> {
  const errors: Array<{ name: string; message: string }> = [];
  const summary = section.summary.trim();
  if (summary.length < 3 || summary.length > 255) {
    errors.push({ name: `${section.key}.summary`, message: "发布摘要需为 3 至 255 个字符。" });
  }

  switch (section.key) {
    case "system": {
      const value = section.value as GeneralConfiguration;
      boundedNumber(errors, "system.query_max_lookback_seconds", value.query_max_lookback_seconds, 60, 2592000, "最大回看时间");
      boundedNumber(errors, "system.query_max_results", value.query_max_results, 1, 10000, "查询结果上限");
      boundedNumber(errors, "system.telemetry_retention_days", value.telemetry_retention_days, 1, 365, "Telemetry 保留天数");
      break;
    }
    case "scopes": {
      const value = section.value as ScopeSectionValue;
      if (value.scopes.length < 1 || value.scopes.length > 10) {
        errors.push({ name: "scopes", message: "必须保留 1 至 10 个 Cluster Scope。" });
      }
      value.scopes.forEach((scope, index) => {
        if (scope.name.trim().length < 2) errors.push({ name: `scopes.${index}.name`, message: "Scope 名称至少需要 2 个字符。" });
        if (!scope.cluster_id.trim()) errors.push({ name: `scopes.${index}.cluster_id`, message: "Cluster identity 不能为空。" });
        if (!scope.environment.trim()) errors.push({ name: `scopes.${index}.environment`, message: "Environment 不能为空。" });
        if (!scope.namespaces.length) errors.push({ name: `scopes.${index}.namespaces`, message: "至少需要 1 个 Namespace。" });
      });
      break;
    }
    case "policies": {
      const policies = section.value as EscalationPolicy[];
      if (policies.length > 50) errors.push({ name: "policies", message: "最多允许 50 条 Policy。" });
      policies.forEach((policy, index) => {
        if (!policy.name.trim()) errors.push({ name: `policies.${index}.name`, message: "Policy 名称不能为空。" });
        if (!policy.severities.length) errors.push({ name: `policies.${index}.severities`, message: "至少选择 1 个 Severity。" });
        boundedNumber(errors, `policies.${index}.minimum_firing_seconds`, policy.minimum_firing_seconds, 0, 604800, "持续 firing 时间");
        boundedNumber(errors, `policies.${index}.minimum_recurrence_count`, policy.minimum_recurrence_count, 1, 100, "最小复发次数");
      });
      break;
    }
    case "providers":
      (section.value as ProviderConfiguration[]).forEach((provider) => {
        if (provider.enabled && provider.provider !== "kubernetes" && !provider.endpoint.trim()) {
          errors.push({ name: `providers.${provider.provider}.endpoint`, message: "启用 Provider 前必须填写 Endpoint。" });
        }
        if (provider.provider === "llm" && !provider.model.trim()) {
          errors.push({ name: "providers.llm.model", message: "LLM Model 不能为空。" });
        }
        boundedNumber(errors, `providers.${provider.provider}.timeout_ms`, provider.timeout_ms, 1000, 60000, "Provider timeout");
        boundedNumber(errors, `providers.${provider.provider}.max_results`, provider.max_results, 1, 10000, "Provider 结果上限");
      });
      break;
    case "secret-references": {
      const seen = new Set<string>();
      for (const reference of section.value as SecretReference[]) {
        const identity = `${reference.provider}\u0000${reference.purpose}`;
        if (seen.has(identity)) {
          errors.push({ name: "secret-references", message: "同一 Provider purpose 只能引用 1 个 Secret version。" });
        }
        seen.add(identity);
      }
      break;
    }
  }
  return errors;
}

export function classifySettingsApplyOutcome(
  revision: ConfigurationRevision,
  providerHealth: ProviderHealth[],
): SettingsApplyOutcome {
  const boundary = revision.worker_boundary;
  const observedHashMismatch = boundary?.status === "succeeded"
    && Boolean(boundary.observed_hash)
    && boundary.observed_hash !== revision.hash;
  const items: SettingsApplyItemResult[] = [{
    id: "worker",
    label: "Worker boundary",
    state: observedHashMismatch ? "partial" : workerItemState(boundary?.status),
    detail: observedHashMismatch
      ? `Worker observed hash ${boundary?.observed_hash} 与 Revision hash 不一致。`
      : boundary?.last_error || boundary?.observed_hash || "尚未观察到 Worker 应用结果。",
    observedAt: boundary?.observed_at,
  }];
  const healthByProvider = new Map(providerHealth
    .filter((item) => !item.configuration_revision_id || item.configuration_revision_id === revision.id)
    .map((item) => [item.provider, item]));
  for (const provider of revision.providers) {
    if (!provider.enabled) {
      items.push({ id: provider.provider, label: provider.provider, state: "disabled", detail: "当前 Revision 明确停用。" });
      continue;
    }
    const health = healthByProvider.get(provider.provider);
    items.push({
      id: provider.provider,
      label: provider.provider,
      state: providerItemState(health?.state),
      detail: health?.detail || "尚未取得当前 Revision 的 Provider 观测。",
      observedAt: health?.checked_at || health?.updated_at,
    });
  }

  if (!boundary) {
    return outcome("accepted", "Revision 已接收", "尚未观察到 Worker boundary，不能声明配置已生效。", items);
  }
  if (boundary.status === "ready") {
    return outcome("accepted", "Revision 已接收", "Activation task 正在等待 Worker 领取。", items);
  }
  if (boundary.status === "running") {
    return outcome("running", "Worker 正在应用", "配置已发布，但尚未完成全部观测。", items);
  }
  if (boundary.status === "failed") {
    return outcome("failed", "Worker 应用失败", boundary.last_error || "Worker 未能应用当前 Revision。", items);
  }
  if (boundary.status !== "succeeded") {
    return outcome("unknown", "Worker 状态未知", `未识别的 Worker 状态：${boundary.status}`, items);
  }
  if (observedHashMismatch) {
    return outcome("partial", "Worker 观测与 Revision 不一致", "Revision 已被 Worker 报告为 succeeded，但 observed hash 不匹配，不能声明配置已生效。", items);
  }

  const enabledProviderItems = items.filter((item) => item.id !== "worker" && item.state !== "disabled");
  if (enabledProviderItems.some((item) => item.state !== "succeeded")) {
    return outcome("partial", "Revision 仅部分生效", "Worker 已完成，但至少一个启用的 Provider 尚未确认可用。", items);
  }
  return outcome("succeeded", "Revision 已在 Worker 生效", "Worker 与全部启用 Provider 的当前观测均可用。", items);
}

export function sanitizeSecretReferences(references: SecretReference[]): SecretReference[] {
  return references.map((reference) => ({
    provider: reference.provider,
    purpose: reference.purpose.trim(),
    secret_version_id: reference.secret_version_id.trim(),
  }));
}

function readSectionValue(draft: ConfigurationDraft, key: SettingsSectionKey): SettingsSectionValue {
  switch (key) {
    case "system":
      return clone(draft.general);
    case "scopes": {
      const scopes = draft.scopes.length ? clone(draft.scopes) : [clone(draft.scope)];
      const defaultIndex = Math.max(0, scopes.findIndex((scope) => sameScope(scope, draft.scope)));
      return { defaultIndex, scopes };
    }
    case "policies":
      return clone(draft.escalation_policies);
    case "providers":
      return clone(draft.providers);
    case "secret-references":
      return clone(draft.secret_references);
  }
}

function sameScope(left: ConfigurationDraft["scope"], right: ConfigurationDraft["scope"]): boolean {
  if (left.id && right.id) return left.id === right.id;
  return left.cluster_id === right.cluster_id
    && left.name === right.name
    && left.environment === right.environment
    && serialize(left.namespaces) === serialize(right.namespaces);
}

function generalChanges(baseline: GeneralConfiguration, value: GeneralConfiguration): string[] {
  return (Object.keys(generalLabels) as Array<keyof GeneralConfiguration>)
    .filter((key) => baseline[key] !== value[key])
    .map((key) => `${generalLabels[key]}：${displayValue(baseline[key])} -> ${displayValue(value[key])}`);
}

function scopeChanges(baseline: ScopeSectionValue, value: ScopeSectionValue): string[] {
  const changes = collectionChanges(baseline.scopes, value.scopes, scopeIdentity, "Scope");
  const before = baseline.scopes[clampIndex(baseline.defaultIndex, baseline.scopes.length)]?.cluster_id || "无";
  const after = value.scopes[clampIndex(value.defaultIndex, value.scopes.length)]?.cluster_id || "无";
  if (before !== after) changes.unshift(`Revision 默认 Scope：${before} -> ${after}`);
  return changes;
}

function providerChanges(baseline: ProviderConfiguration[], value: ProviderConfiguration[]): string[] {
  const before = new Map(baseline.map((item) => [item.provider, item]));
  const result: string[] = [];
  for (const provider of value) {
    const previous = before.get(provider.provider);
    if (!previous) {
      result.push(`新增 Provider：${provider.provider}`);
      continue;
    }
    const changedFields = (Object.keys(provider) as Array<keyof ProviderConfiguration>)
      .filter((key) => key !== "provider" && previous[key] !== provider[key]);
    if (changedFields.length) result.push(`${provider.provider}：修改 ${changedFields.join("、")}`);
    before.delete(provider.provider);
  }
  for (const provider of before.keys()) result.push(`移除 Provider：${provider}`);
  return result;
}

function secretReferenceChanges(baseline: SecretReference[], value: SecretReference[]): string[] {
  const before = new Map(baseline.map((item) => [secretIdentity(item), item]));
  const result: string[] = [];
  for (const reference of value) {
    const previous = before.get(secretIdentity(reference));
    if (!previous) result.push(`新增 reference：${reference.provider}/${reference.purpose}`);
    else if (previous.secret_version_id !== reference.secret_version_id) {
      result.push(`替换 reference：${reference.provider}/${reference.purpose}`);
    }
    before.delete(secretIdentity(reference));
  }
  for (const reference of before.values()) result.push(`移除 reference：${reference.provider}/${reference.purpose}`);
  return result;
}

function collectionChanges<T>(
  baseline: T[],
  value: T[],
  identity: (item: T, index: number) => string,
  label: string,
): string[] {
  const before = new Map(baseline.map((item, index) => [identity(item, index), item]));
  const result: string[] = [];
  value.forEach((item, index) => {
    const id = identity(item, index);
    const previous = before.get(id);
    if (!previous) result.push(`新增 ${label}：${id}`);
    else if (serialize(previous) !== serialize(item)) result.push(`修改 ${label}：${id}`);
    before.delete(id);
  });
  for (const id of before.keys()) result.push(`移除 ${label}：${id}`);
  return result;
}

function scopeIdentity(scope: ConfigurationDraft["scope"], index: number): string {
  return scope.id || scope.cluster_id || `Scope ${index + 1}`;
}

function policyIdentity(policy: EscalationPolicy, index: number): string {
  return policy.id || policy.name || `Policy ${index + 1}`;
}

function secretIdentity(reference: SecretReference): string {
  return `${reference.provider}\u0000${reference.purpose}`;
}

function displayValue(value: string | number | boolean): string {
  if (typeof value === "boolean") return value ? "启用" : "停用";
  return String(value);
}

function boundedNumber(
  errors: Array<{ name: string; message: string }>,
  name: string,
  value: number,
  min: number,
  max: number,
  label: string,
) {
  if (!Number.isFinite(value) || value < min || value > max) {
    errors.push({ name, message: `${label}必须在 ${min} 至 ${max} 之间。` });
  }
}

function workerItemState(status?: string): SettingsApplyItemResult["state"] {
  if (!status || status === "ready") return "waiting";
  if (status === "running") return "running";
  if (status === "succeeded") return "succeeded";
  if (status === "failed") return "failed";
  return "unknown";
}

function providerItemState(state?: ProviderHealth["state"]): SettingsApplyItemResult["state"] {
  if (state === "available") return "succeeded";
  if (state === "partial") return "partial";
  if (state === "unavailable" || state === "not_configured") return "failed";
  if (state === "disabled") return "disabled";
  return "unknown";
}

function outcome(
  state: SettingsApplyOutcome["state"],
  title: string,
  description: string,
  items: SettingsApplyItemResult[],
): SettingsApplyOutcome {
  return { state, title, description, items };
}

function clampIndex(index: number, length: number): number {
  return Math.min(Math.max(index, 0), Math.max(length - 1, 0));
}

function serialize(value: unknown): string {
  return JSON.stringify(value);
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T;
}
