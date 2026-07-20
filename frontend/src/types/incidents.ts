export type IncidentSeverity = "critical" | "warning" | "info" | "unknown";

export type IncidentStatus =
  | "DETECTED"
  | "CORRELATING"
  | "DIAGNOSING"
  | "DIAGNOSIS_COMPLETED"
  | "PLANNING_REMEDIATION"
  | "AWAITING_APPROVAL"
  | "APPLYING_CHANGE"
  | "VERIFYING"
  | "RESOLVED"
  | "FAILED"
  | "CLOSED_NO_ACTION";

export interface IncidentSectionSummaryDTO {
  availability: "available" | "unavailable" | "forbidden";
  status: string;
  id?: string;
}

export interface IncidentSummaryDTO {
  investigation: IncidentSectionSummaryDTO;
  approval: IncidentSectionSummaryDTO;
  delivery: IncidentSectionSummaryDTO;
  verification: IncidentSectionSummaryDTO;
  postmortem: IncidentSectionSummaryDTO;
}

export interface IncidentDTO {
  id: string;
  title: string;
  severity: IncidentSeverity;
  status: IncidentStatus;
  cluster: string;
  service: string;
  environment: string;
  namespace: string;
  workload_kind: string;
  workload_name: string;
  triggering_summary: string;
  first_seen_at: string;
  last_seen_at: string;
  resolved_at?: string;
  created_at: string;
  updated_at: string;
  summary: IncidentSummaryDTO;
}

export interface BoundedPage<T> {
  items: T[];
  total: number;
  page: number;
  page_size: number;
  bounded?: boolean;
}

export interface IncidentSignalDTO {
  source: string;
  status: "firing" | "resolved";
  severity: IncidentSeverity;
  category: string;
  summary: string;
  occurred_at: string;
  received_at: string;
}

export interface IncidentTimelineDTO {
  key: string;
  event_type: string;
  actor_type: "system" | "source" | "user" | "agent";
  summary: string;
  occurred_at: string;
}

export interface IncidentEvidenceDTO {
  id: string;
  type: string;
  source: string;
  resource_ref?: string;
  summary: string;
  state: "available" | "partial" | "malformed" | "unavailable" | "no_data";
  data_freshness: string;
  related_claim: string;
  truncated: boolean;
  collected_at: string;
}

export interface DiagnosisClaimDTO {
  statement: string;
  evidence_ids: string[];
  strong: boolean;
}

export interface DiagnosisHypothesisDTO {
  statement: string;
  confidence: number;
  evidence_ids: string[];
}

export interface AgentDiagnosisDTO {
  summary: string;
  hypotheses: DiagnosisHypothesisDTO[];
  confirmed_facts: DiagnosisClaimDTO[];
  unknowns: string[];
  confidence: number;
  affected_resources: string[];
  recommended_next_actions: string[];
  degraded: boolean;
  budget_summary: string;
  coverage_summary: string;
}

export interface AgentRunDTO {
  id: string;
  attempt: number;
  status: "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED";
  used_steps: number;
  max_steps: number;
  used_tool_calls: number;
  max_tool_calls: number;
  used_evidence_items: number;
  max_evidence_items: number;
  termination_reason?: string;
  failure_summary?: string;
  diagnosis?: AgentDiagnosisDTO;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface AgentStepDTO {
  id: string;
  sequence: number;
  type: string;
  status: "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED";
  summary: string;
  typed_tool?: string;
  evidence_id?: string;
  retry_count: number;
  duration_ms: number;
  failure_reason?: string;
  started_at?: string;
  finished_at?: string;
}

export interface AgentEvidenceDTO {
  id: string;
  typed_tool: string;
  resource_scope?: string;
  summary: string;
  state: IncidentEvidenceDTO["state"];
  truncated: boolean;
  collected_at: string;
}

export interface InvestigationDTO {
  runs: AgentRunDTO[];
  steps: AgentStepDTO[];
  evidence: AgentEvidenceDTO[];
}

export interface IncidentK8sPodDTO {
  namespace: string;
  name: string;
  phase: string;
  ready_containers: number;
  total_containers: number;
  restart_count: number;
  node_name?: string;
  pod_ip?: string;
  owner_kind?: string;
  owner_name?: string;
  collected_at?: string;
}

export interface IncidentK8sDeploymentDTO {
  namespace: string;
  name: string;
  replicas: number;
  ready_replicas: number;
  updated_replicas: number;
  available_replicas: number;
  strategy?: string;
  collected_at?: string;
}

export interface IncidentK8sServiceDTO {
  namespace: string;
  name: string;
  type: string;
  cluster_ip?: string;
  ports?: Array<{ name?: string; protocol: string; port: number; target_port?: string }>;
  collected_at?: string;
}

export interface IncidentK8sEventDTO {
  namespace?: string;
  name: string;
  type?: string;
  reason?: string;
  message?: string;
  involved_kind?: string;
  involved_name?: string;
  count?: number;
  last_seen?: string;
  collected_at?: string;
}

export interface IncidentResourcesDTO {
  cluster: string;
  namespace: string;
  deployments: IncidentK8sDeploymentDTO[];
  pods: IncidentK8sPodDTO[];
  services: IncidentK8sServiceDTO[];
  events: IncidentK8sEventDTO[];
}

export interface RemediationDTO {
  id: string;
  status: string;
  operation_type: string;
  target: {
    api_version: string;
    kind: string;
    namespace: string;
    name: string;
    container?: string;
  };
  proposed_value: { image_digest?: string; replicas?: number };
  risk_level: string;
  patch_summary: string;
  rollback_plan: string;
  validation_plan: string;
  approval_actor: string;
  approval_decided_at?: string;
  created_at: string;
  updated_at: string;
}

export interface DeliveryDTO {
  id: string;
  status: string;
  ci_status: string;
  pr_state: string;
  pull_request: number;
  pull_request_url: string;
  head_commit_sha: string;
  merged_commit_sha: string;
  target_revision: string;
  argocd_application: string;
  detected_revision: string;
  argocd_sync_status: string;
  argocd_operation_phase: string;
  argocd_health_status: string;
  image_digest?: string;
  provenance_status: "verified" | "unverified" | "conflict";
  attempts: number;
  deployment_generation: number;
  observed_generation: number;
  desired_replicas: number;
  updated_replicas: number;
  available_replicas: number;
  unavailable_replicas: number;
  started_at?: string;
  deadline_at?: string;
  completed_at?: string;
  failure_reason?: string;
  updated_at: string;
}

export interface VerificationRunDTO {
  id: string;
  incident_id: string;
  status: "pending" | "running" | "passed" | "failed" | "timed_out" | "cancelled";
  target_revision: string;
  attempt: number;
  started_at?: string;
  deadline_at: string;
  completed_at?: string;
  result_summary?: string;
  failure_reason?: string;
  created_at: string;
  updated_at: string;
}

export interface VerificationCheckDTO {
  id: string;
  type: string;
  template_id?: string;
  required: boolean;
  status: string;
  comparison?: "lt" | "lte" | "gt" | "gte" | "absent";
  threshold?: number;
  observed?: Record<string, string | number | boolean | null>;
  stability_window_seconds: number;
  stability_progress_seconds: number;
  timeout_seconds: number;
  attempt_count: number;
  failure_reason?: string;
  first_checked_at?: string;
  last_checked_at?: string;
  passed_at?: string;
}

export interface VerificationDetailDTO {
  verification: VerificationRunDTO;
  checks: VerificationCheckDTO[];
}

export interface ClassifiedFactDTO {
  classification: "fact" | "inference" | "unknown" | string;
  summary: string;
  evidence_ids?: string[];
}

export interface PostmortemDTO {
  id: string;
  incident_id: string;
  verification_id: string;
  title: string;
  impact_summary: string;
  detected_at: string;
  mitigated_at?: string;
  resolved_at: string;
  duration_seconds: number;
  service: string;
  workload: string;
  environment: string;
  triggering_signal: ClassifiedFactDTO;
  change_correlation: ClassifiedFactDTO;
  root_cause: ClassifiedFactDTO;
  remediation_summary: ClassifiedFactDTO;
  approval_summary: ClassifiedFactDTO;
  delivery_revision: string;
  verification_summary: string;
  checks: Array<{ check_id: string; type: string; status: string; required: boolean; template_id?: string; reason?: string }>;
  timeline: Array<{ event_type: string; summary: string; occurred_at: string }>;
  follow_up_actions: string[];
  generated_at: string;
  generation_version: number;
}

export interface IncidentRealtimeEvent {
  incident_id: string;
  sequence: number;
  kind: "refresh";
}

export interface IncidentListQuery {
  status?: string;
  severity?: string;
  service?: string;
  environment?: string;
  namespace?: string;
  workload?: string;
  created_from?: string;
  created_to?: string;
  q?: string;
  page?: number;
  page_size?: number;
}

export type LoadState = "loading" | "ready" | "empty" | "error" | "forbidden" | "not_found" | "not_generated" | "unavailable";
