import type { ActionCard, OperationPlan } from "./agent";
import { getJSON, postJSON } from "./client";

export interface OperationTarget {
  cluster_id: string;
  environment: string;
  namespace: string;
  workload_kind: "Deployment" | string;
  workload_name: string;
  scenario_id?: string;
}

export interface OperationContextLink {
  kind: "agent" | "incident" | "verification" | string;
  label: string;
  href: string;
  status?: string;
}

export interface OperationAuditEvent {
  id: string;
  sequence: number;
  type: string;
  payload: Record<string, unknown>;
  content_hash: string;
  occurred_at: string;
}

export interface OperationVerificationObservation {
  id: string;
  source: "local" | "kubernetes";
  status: "passed" | "failed";
  provider_identity: Record<string, unknown>;
  evidence: Record<string, unknown>;
  content_hash: string;
  summary: string;
  observed_at: string;
}

export type OperationExecutionStatus =
  | "ready"
  | "running"
  | "succeeded"
  | "failed"
  | "precondition_failed"
  | "verification_failed"
  | "cancelled";

export interface OperationExecution {
  id: string;
  subject_type: "action_card" | "operation_plan";
  subject_id: string;
  run_id: string;
  incident_id?: string;
  configuration_revision_id: string;
  operation_type: string;
  expected_content_hash: string;
  status: OperationExecutionStatus;
  attempt: number;
  external_effect_started_at?: string;
  result?: Record<string, unknown>;
  failure_code?: string;
  failure_summary?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  events: OperationAuditEvent[];
  verification?: OperationVerificationObservation;
  links: OperationContextLink[];
}

export interface ChangeFreezeState {
  target: OperationTarget;
  enabled: boolean;
  reason: string;
  row_version: number;
  updated_at?: string;
}

export interface ChangeCandidate {
  id: string;
  incident_id: string;
  run_id: string;
  cycle: number;
  change_ref: string;
  source_type: string;
  repository: string;
  commit_sha: string;
  gitops_revision: string;
  image_digest: string;
  target_path: string;
  category: string;
  supporting_evidence: Record<string, unknown>;
  content_hash: string;
  change_time: string;
  created_at: string;
}

export interface DeploymentBaseline {
  id: string;
  target_identity_hash: string;
  cluster: string;
  environment: string;
  namespace: string;
  workload_kind: string;
  workload_name: string;
  container_name: string;
  repository: string;
  base_branch: string;
  target_path: string;
  source_revision: string;
  image_digest: string;
  gitops_revision: string;
  config_hash: string;
  verification_policy_version: string;
  verification_hash: string;
  status: string;
  row_version: number;
  verified_at: string;
  superseded_at?: string;
}

export interface DeliveryProjection {
  id: string;
  incident_id: string;
  repository: string;
  base_revision: string;
  head_branch: string;
  commit_sha: string;
  pull_request_number: number;
  pull_request_url: string;
  pull_request_state: string;
  ci_status: string;
  merged_commit_sha: string;
  target_revision: string;
  argo_application: string;
  argo_sync_status: string;
  argo_operation_phase: string;
  argo_health_status: string;
  rollout_revision: string;
  desired_replicas: number;
  updated_replicas: number;
  available_replicas: number;
  unavailable_replicas: number;
  status: string;
  last_observed_at?: string;
}

export interface ProviderBranch {
  provider: "kubernetes" | "github" | "argocd" | string;
  role: "core" | "optional";
  enabled: boolean;
  state: string;
  detail: string;
  configuration_revision_id: string;
  checked_at?: string;
}

export interface DevOpsWorkspace {
  operation_plans: OperationPlan[];
  action_cards: ActionCard[];
  executions: OperationExecution[];
  change_freezes: ChangeFreezeState[];
  change_candidates: ChangeCandidate[];
  deployment_baselines: DeploymentBaseline[];
  deliveries: DeliveryProjection[];
  providers: ProviderBranch[];
  collected_at: string;
}

export function getDevOpsWorkspace(signal?: AbortSignal): Promise<DevOpsWorkspace> {
  return getJSON("/api/v1/devops?limit=100", { signal });
}

export function executeActionCard(id: string, expectedHash: string): Promise<OperationExecution> {
  return postJSON(`/api/v1/agent/action-cards/${encodeURIComponent(id)}/executions`, { expected_hash: expectedHash });
}

export function executeOperationPlan(id: string, expectedHash: string): Promise<OperationExecution> {
  return postJSON(`/api/v1/operation-plans/${encodeURIComponent(id)}/executions`, { expected_hash: expectedHash });
}
