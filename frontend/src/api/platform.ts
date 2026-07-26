import { getJSON, postJSON } from "./client";
import type { TopologySnapshot } from "./infrastructure";

export type ProviderIdentity =
  | "llm"
  | "kubernetes"
  | "prometheus"
  | "alertmanager"
  | "elasticsearch"
  | "tempo"
  | "github"
  | "argocd";

export interface GeneralConfiguration {
  query_max_lookback_seconds: number;
  query_max_results: number;
  telemetry_retention_days: number;
  browser_notifications_enabled: boolean;
  automatic_escalation_enabled: boolean;
}

export interface OperationalScope {
  id?: string;
  name: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  configuration_revision_id?: string;
  configuration_revision_hash?: string;
  active: boolean;
}

export interface ProviderConfiguration {
  provider: ProviderIdentity;
  enabled: boolean;
  endpoint: string;
  model: string;
  timeout_ms: number;
  max_results: number;
  context_link_base: string;
}

export interface SecretReference {
  provider: ProviderIdentity;
  purpose: string;
  secret_version_id: string;
  state?: "configured" | "invalid";
  fingerprint?: string;
}

export interface ConfigurationDraft {
  summary: string;
  general: GeneralConfiguration;
  scope: OperationalScope;
  scopes: OperationalScope[];
  providers: ProviderConfiguration[];
  secret_references: SecretReference[];
}

export interface ActivationStatus {
  task_id: string;
  revision_id: string;
  status: "ready" | "running" | "succeeded" | "failed";
  worker_id?: string;
  observed_hash?: string;
  observed_at?: string;
  last_error?: string;
}

export interface ConfigurationRevision extends ConfigurationDraft {
  id: string;
  number: number;
  hash: string;
  created_by: string;
  created_at: string;
  active: boolean;
  worker_boundary?: ActivationStatus;
}

export type ProviderState = "available" | "partial" | "unavailable" | "disabled" | "not_configured";

export interface ProviderResult {
  provider: ProviderIdentity | "mysql";
  state: ProviderState;
  detail: string;
  checked_at?: string;
}

export interface ProviderHealth extends ProviderResult {
  configuration_revision_id?: string;
  updated_at: string;
}

export interface BootstrapDiagnostics {
  listen_boundary: string;
  mysql_database: string;
  data_directory: string;
  worker_management_target: string;
  lifecycle: string;
}

export interface SettingsSnapshot {
  bootstrap: BootstrapDiagnostics;
  active_revision: ConfigurationRevision;
  history: ConfigurationRevision[];
  provider_health: ProviderHealth[];
}

export interface ScopePage {
  items: OperationalScope[];
}

export interface BootstrapSnapshot {
  product: "CloudOps";
  contract: "V1";
  active_revision: ConfigurationRevision;
  active_scope: OperationalScope;
  provider_health: ProviderHealth[];
  scenario_state: string;
  capabilities: string[];
  collected_at: string;
}

export interface OverviewSnapshot {
  bootstrap: BootstrapSnapshot;
  unread_notifications: number;
  atlas: TopologySnapshot;
}

export interface FieldError {
  field: string;
  code: string;
  message: string;
}

export interface ConfigurationValidation {
  id: string;
  draft_hash: string;
  valid: boolean;
  errors: FieldError[];
  provider_results: ProviderResult[];
  created_at: string;
  expires_at: string;
}

export interface SecretVersion {
  id: string;
  provider: ProviderIdentity;
  purpose: string;
  state: "configured" | "invalid";
  fingerprint: string;
  created_at: string;
  referenced_by: string[];
}

export interface StorageStatus {
  database_tables: number;
  configuration_count: number;
  notification_count: number;
  secret_version_count: number;
  data_capacity_bytes: number;
  data_available_bytes: number;
  latest_backup_name?: string;
  latest_backup_at?: string;
  telemetry_retention_days: number;
}

export function getBootstrap(signal?: AbortSignal): Promise<BootstrapSnapshot> {
  return getJSON("/api/v1/bootstrap", { signal });
}

export function getOverview(signal?: AbortSignal): Promise<OverviewSnapshot> {
  return getJSON("/api/v1/overview", { signal });
}

export function getSettings(signal?: AbortSignal): Promise<SettingsSnapshot> {
  return getJSON("/api/v1/settings", { signal });
}

export async function getScopes(signal?: AbortSignal): Promise<OperationalScope[]> {
  const page = await getJSON<ScopePage>("/api/v1/scopes", { signal });
  return page.items;
}

export function activateScope(id: string): Promise<OperationalScope> {
  return postJSON(`/api/v1/scopes/${encodeURIComponent(id)}/activate`);
}

export function getStorageStatus(signal?: AbortSignal): Promise<StorageStatus> {
  return getJSON("/api/v1/storage-status", { signal });
}

export function validateSettings(draft: ConfigurationDraft): Promise<ConfigurationValidation> {
  return postJSON("/api/v1/settings/validate", draft);
}

export function applyConfiguration(validationID: string, draft: ConfigurationDraft): Promise<ConfigurationRevision> {
  return postJSON("/api/v1/configuration-revisions", { validation_id: validationID, draft });
}

export function createSecret(input: { provider: ProviderIdentity; purpose: string; value: string }): Promise<SecretVersion> {
  return postJSON("/api/v1/secrets", input);
}

export function testProvider(configuration: ProviderConfiguration, secretReferences: SecretReference[], clusterID = ""): Promise<ProviderResult> {
  return postJSON(`/api/v1/providers/${encodeURIComponent(configuration.provider)}/tests`, {
    configuration,
    secret_references: secretReferences,
    cluster_id: clusterID,
  });
}

export function configurationDraft(revision: ConfigurationRevision): ConfigurationDraft {
  const scopes = revision.scopes?.length ? revision.scopes : [revision.scope];
  return {
    summary: revision.summary,
    general: structuredClone(revision.general),
    scope: structuredClone(revision.scope),
    scopes: structuredClone(scopes),
    providers: structuredClone(revision.providers),
    secret_references: structuredClone(revision.secret_references),
  };
}
