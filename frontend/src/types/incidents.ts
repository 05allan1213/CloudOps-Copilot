export type IncidentSeverity = "critical" | "warning" | "info" | "unknown";

export type IncidentStatus =
  | "detected"
  | "investigating"
  | "awaiting_approval"
  | "delivering"
  | "verifying"
  | "resolved"
  | "closed";

export interface IncidentView {
  id: string;
  cycle: number;
  status: IncidentStatus;
  severity: IncidentSeverity;
  summary?: string;
  version: number;
  needs_attention: boolean;
  blocking_reason_code?: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
  operational_context: IncidentOperationalContextView;
  attention: IncidentAttentionView;
  related_alert_count: number;
  context_links: IncidentContextLinkView[];
  decision?: IncidentDecisionView;
  recovery: IncidentRecoveryView;
  first_seen_at?: string;
  last_seen_at?: string;
  resolved_at?: string;
  created_at?: string;
  updated_at?: string;
}

export interface ResourceView {
  id: string;
  kind: string;
  status?: string;
  version?: number;
  cycle?: number;
  summary?: string;
  hash?: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
  created_at?: string;
  updated_at?: string;
}

export type JSONValue =
  | null
  | boolean
  | number
  | string
  | JSONValue[]
  | { [key: string]: JSONValue };

export interface IncidentResourceRefView {
  id: string;
  kind: string;
  namespace: string;
  name: string;
}

export interface IncidentTimeRangeView {
  from: string;
  to: string;
}

export interface IncidentOperationalContextView {
  operational_scope_id?: string;
  cluster: string;
  environment: string;
  namespace: string;
  service: string;
  resource: IncidentResourceRefView;
  time_range: IncidentTimeRangeView;
}

export interface IncidentContextLinkView {
  workspace: "monitoring" | "logs" | "traces" | "agent" | "alerts" | "devops";
  path: string;
  query: Record<string, string>;
  operational_scope_id: string;
  external: false;
}

export interface IncidentAttentionView {
  required: boolean;
  reason_code?: string;
  stage: "detect" | "investigate" | "decide" | "act" | "verify" | "recovered" | "closed" | "unknown";
}

export interface IncidentRecoveryView {
  state: "not_started" | "awaiting_verification" | "verifying" | "investigate" | "recovered";
  verification_attempts: number;
  failed_verification_count: number;
  latest_verification_id?: string;
  latest_verification_status?: string;
  common_window_started_at?: string;
  common_window_completed_at?: string;
  resolution_report_id?: string;
  can_close: boolean;
}

export interface IncidentAlertRelationView {
  id: string;
  cycle: number;
  alert_id: string;
  status: "firing" | "resolved";
  severity: IncidentSeverity;
  summary: string;
  category: string;
  source: string;
  cluster: string;
  environment: string;
  namespace: string;
  service: string;
  target_kind: string;
  target_name: string;
  first_seen_at: string;
  last_seen_at: string;
  resolved_at?: string;
  provenance: "owner_created" | "owner_attached" | "escalation_policy" | "legacy_automatic_ingress";
  configuration_revision_id?: string;
  escalation_policy_id?: string;
  context_link: IncidentContextLinkView;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
  created_at: string;
}

export interface IncidentTimelineEventView {
  id: string;
  cycle: number;
  type: string;
  source_status?: string;
  target_status?: string;
  reason_code?: string;
  actor_type: string;
  actor_id: string;
  summary: string;
  metadata: JSONValue;
  occurred_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface IncidentEvidenceView {
  id: string;
  cycle: number;
  type: string;
  source: string;
  producer_type?: string;
  producer_id?: string;
  producer_version?: string;
  tool_name?: string;
  resource_ref: string;
  time_range?: JSONValue;
  query_text?: string;
  summary: string;
  content_hash: string;
  provenance?: JSONValue;
  valid: boolean;
  truncated: boolean;
  collected_at: string;
  observed_at?: string;
  context_link: IncidentContextLinkView;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface IncidentInvestigationView {
  id: string;
  cycle: number;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  version: number;
  objective: string;
  outcome?: string;
  failure_code?: string;
  failure_summary?: string;
  model_provider?: string;
  actual_model?: string;
  prompt_version: string;
  used_steps: number;
  max_steps: number;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  context_link: IncidentContextLinkView;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface IncidentDecisionView {
  cycle: number;
  kind: "pending" | "no_change" | "action" | "recovery";
  status: string;
  summary: string;
  investigation_id?: string;
  remediation_plan_id?: string;
  decision_id?: string;
  decision?: string;
  reason?: string;
  actor?: string;
  delivery_id?: string;
  verification_id?: string;
  context_link?: IncidentContextLinkView;
  decided_at?: string;
}

export interface RemediationTargetResourceView {
  api_version: string;
  kind: string;
  namespace: string;
  name: string;
  container?: string;
}

export interface RemediationTargetView {
  repository: string;
  base_branch: string;
  base_revision: string;
  last_known_good_revision: string;
  base_blob_sha: string;
  file_mode: string;
  path: string;
  field_ref: string;
  resource: RemediationTargetResourceView;
}

export interface EvidenceBindingView {
  id: string;
  content_hash: string;
}

export interface RemediationDecisionView {
  id: string;
  decision_schema_version: number;
  plan_version: number;
  decision: "approved" | "rejected";
  actor: {
    provider: "local";
    login: string;
    role: "owner";
  };
  reason: string;
  request_id: string;
  request_authenticated_at: string;
  expires_at: string;
  approved_hash_schema_version: number;
  approved_plan_hash: string;
  approved_base_sha: string;
  approved_post_image_hash: string;
  approved_tree_hash: string;
  approved_patch_hash: string;
  approved_policy_hash: string;
  approved_verification_hash: string;
  approved_evidence_set_hash: string;
  created_at: string;
}

export interface RemediationPlanView {
  id: string;
  kind: "remediation_plan";
  cycle: number;
  status: string;
  version: number;
  plan_version: number;
  plan_content_schema_version: number;
  incident_version: number;
  created_by_agent_run_id: string;
  operation_type: "restore_required_env";
  risk_level: "low" | "medium" | "high";
  patch_summary: string;
  rollback_plan: string;
  validation_plan: string;
  target: RemediationTargetView;
  hash_schema_version: number;
  diagnosis_hash: string;
  canonical_plan_hash: string;
  expected_before_hash: string;
  expected_post_image_hash: string;
  expected_tree_hash: string;
  proposed_patch_hash: string;
  canonical_manifest: JSONValue;
  bounded_diff: string;
  policy_version: string;
  policy_hash: string;
  policy_snapshot: JSONValue;
  verification_plan: JSONValue;
  verification_plan_hash: string;
  evidence_bindings: EvidenceBindingView[];
  evidence_set_hash: string;
  expires_at: string;
  decision?: RemediationDecisionView;
  created_at: string;
  updated_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface DeliveryView {
  id: string;
  kind: "delivery";
  cycle: number;
  status: string;
  version: number;
  remediation_plan_id: string;
  repository: string;
  base_revision: string;
  head_branch: string;
  commit_sha?: string;
  pr_number?: number;
  pr_url?: string;
  pr_state?: string;
  ci_status: string;
  merged_commit_sha?: string;
  target_revision?: string;
  detected_revision?: string;
  argocd_application?: string;
  argocd_project?: string;
  argocd_sync_status?: string;
  argocd_operation_phase?: string;
  argocd_health_status?: string;
  resource_health?: JSONValue;
  cluster?: string;
  environment?: string;
  namespace?: string;
  workload_kind?: string;
  workload_name?: string;
  deployment_generation?: number;
  observed_generation?: number;
  rollout_revision?: string;
  desired_replicas: number;
  updated_replicas: number;
  available_replicas: number;
  unavailable_replicas: number;
  sync_started_at?: string;
  sync_completed_at?: string;
  delivery_started_at?: string;
  delivery_deadline_at?: string;
  delivery_completed_at?: string;
  last_observed_at?: string;
  failure_code?: string;
  failure_reason?: string;
  created_at: string;
  updated_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface VerificationSampleView {
  id: string;
  schema_version: number;
  sequence: number;
  status: string;
  observed: JSONValue;
  source_reference?: string;
  reason_code?: string;
  window_start_at?: string;
  window_end_at?: string;
  sampled_at: string;
  content_hash: string;
  created_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface VerificationCheckView {
  id: string;
  spec_schema_version: number;
  type: string;
  status: string;
  required: boolean;
  profile_id: string;
  template_id: string;
  template_version: string;
  subject: {
    repository?: string;
    pull_request?: number;
    revision: string;
    argocd_application?: string;
    argocd_project?: string;
    cluster?: string;
    environment?: string;
    namespace?: string;
    service?: string;
    workload_kind?: string;
    workload_name?: string;
    alert_fingerprint?: string;
  };
  expected: JSONValue;
  observed?: JSONValue;
  comparison?: string;
  threshold?: number;
  source_reference?: string;
  source_identity: string;
  lookback_ms: number;
  initial_delay_ms: number;
  stability_window_ms: number;
  timeout_ms: number;
  poll_interval_ms: number;
  min_samples: number;
  sample_unit: string;
  failure_mode: string;
  first_checked_at?: string;
  last_checked_at?: string;
  passed_at?: string;
  consecutive_success_since?: string;
  attempt_count: number;
  failure_reason?: string;
  samples: VerificationSampleView[];
  created_at: string;
  updated_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface VerificationRunView {
  id: string;
  kind: "verification";
  cycle: number;
  status: string;
  version: number;
  trigger_type: "post_delivery" | "no_change_signal" | "operational_recovery";
  remediation_plan_id?: string;
  change_request_id?: string;
  trigger_signal_id?: string;
  recovery_provenance?: RecoveryProvenanceView;
  attempt: number;
  profile: {
    id: string;
    version: number;
    hash: string;
    contract_version: number;
  };
  revisions: {
    target_revision?: string;
    source_revision?: string;
    image_digest?: string;
    gitops_revision?: string;
  };
  started_at?: string;
  deadline_at: string;
  completed_at?: string;
  common_window: {
    stability_window_ms: number;
    success_since?: string;
    completed_at?: string;
  };
  result_summary?: string;
  failure_reason?: string;
  checks: VerificationCheckView[];
  created_at: string;
  updated_at: string;
  migrated_legacy: boolean;
  migrated_legacy_context: boolean;
}

export interface ResolutionReportView {
  id: string;
  kind: "resolution_report";
  status: "resolved";
  cycle: number;
  trigger_type: "post_delivery" | "no_change_signal" | "operational_recovery";
  resolution_reason: string;
  service: string;
  workload: string;
  environment: string;
  impact_summary: string;
  summary: string;
  hash: string;
  cycle_started_at: string;
  resolved_at: string;
  measured_duration_ms: number;
  generated_at: string;
  revisions: {
    bad_gitops_revision?: string;
    fix_gitops_revision?: string;
    source_revision?: string;
    image_digest?: string;
    gitops_revision?: string;
  };
  recovery_provenance?: RecoveryProvenanceView;
  verification_profile: {
    id: string;
    hash: string;
  };
  stability: {
    common_window_started_at: string;
    common_window_completed_at: string;
  };
  trigger_signal: JSONValue;
  diagnosis: JSONValue | null;
  evidence: JSONValue;
  remediation_plan: JSONValue | null;
  remediation_decision: JSONValue | null;
  delivery: JSONValue | null;
  verification: JSONValue;
  timeline: JSONValue;
  agent_usage: JSONValue;
  migrated_legacy_context: boolean;
}

export interface CollectionResponse<T> {
  items: T[];
  next_cursor?: string;
}

export interface IncidentResponse {
  incident: IncidentView;
}

export interface ResourceResponse<T = ResourceView> {
  resource: T;
}

export interface CommandResponse {
  id: string;
  command: string;
  status: string;
  version?: number;
  cycle?: number;
}

export interface CommandOutcome {
  result: CommandResponse;
  httpStatus: number;
  requestID: string;
  traceID: string;
  idempotentReplay: boolean;
  idempotencyKey: string;
}

export interface VersionedCommand {
  expected_version: number;
  reason?: string;
}

export interface DecisionCommand extends VersionedCommand {
  decision: "approved" | "rejected";
  expected_hash: string;
  reason: string;
}

export interface RecoveryDecisionCommand extends VersionedCommand {
  decision: "verify_recovery";
  reason: string;
}

export interface RecoveryProvenanceView {
  configuration_revision_id: string;
  operational_scope_id: string;
  investigation_id: string;
  decision_id: string;
}

export interface IncidentRealtimeEvent {
  cursor: string;
  incident_id: string;
  resource:
    | "incident"
    | "signals"
    | "timeline"
    | "evidence"
    | "investigations"
    | "remediation_plans"
    | "delivery"
    | "verifications"
    | "resolution_report";
}

export interface IncidentListQuery {
  status?: IncidentStatus;
  severity?: IncidentSeverity;
  service?: string;
  attention?: boolean;
  resource?: string;
  alert?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
}

export type LoadState =
  | "loading"
  | "ready"
  | "empty"
  | "error"
  | "forbidden"
  | "not_found"
  | "unavailable";
