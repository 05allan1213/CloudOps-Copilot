import { apiURL, deleteJSON, getJSON, patchJSON, postJSON } from "./client";
import type { OperationalScope } from "./platform";
import type { TelemetryResourceReference, TelemetryTimeRange } from "./telemetry";

export type AgentSubject = "alert" | "incident" | "consultation";
export type AgentOutcome = "diagnosed" | "insufficient" | "cancelled" | "failed";
export type AgentRunStatus = "pending" | "running" | "completed" | "failed" | "cancelled";

export interface AgentEvidenceCitation {
  id: string;
  evidence_id: string;
  use: string;
  source: string;
  summary: string;
  query_execution_id?: string;
  configuration_revision_id: string;
  resource_ref: string;
  time_range?: TelemetryTimeRange;
  observed_at?: string;
  collected_at: string;
  content_hash: string;
  facts?: unknown;
}

export interface AgentGuidanceCitation {
  id: string;
  type: "knowledge" | "runbook" | string;
  knowledge_item_id?: string;
  revision_id: string;
  revision: number;
  title: string;
  source: string;
  age_seconds: number;
  stale: boolean;
  created_at: string;
  review_at?: string;
  expires_at?: string;
}

export interface AgentStep {
  id: string;
  sequence: number;
  type: string;
  tool: string;
  target: string;
  scope: unknown;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  result_summary?: string;
  evidence_id?: string;
  duration_ms: number;
  error_code?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
}

export interface ActionAuthorization {
  id: string;
  subject_type: string;
  subject_id: string;
  authorized_content_hash: string;
  authorized_by: string;
  reason: string;
  expires_at: string;
  created_at: string;
}

export interface ActionCard {
  id: string;
  run_id: string;
  authority: "reversible";
  action_type: string;
  target: unknown;
  parameters: unknown;
  preconditions: unknown;
  risk: string;
  content_hash: string;
  status: string;
  expires_at: string;
  authorization?: ActionAuthorization;
  created_at: string;
}

export interface OperationPlan {
  id: string;
  run_id: string;
  configuration_revision_id: string;
  authority: "high_impact";
  operation_type: string;
  target: unknown;
  parameters: unknown;
  intended_state: unknown;
  preconditions: unknown;
  risk: string;
  verification_intent: unknown;
  content_hash: string;
  status: string;
  expires_at: string;
  authorization?: ActionAuthorization;
  created_at: string;
}

export interface AgentRun {
  id: string;
  subject_type: AgentSubject;
  alert_id?: string;
  incident_id?: string;
  consultation_id?: string;
  configuration_revision_id: string;
  context_snapshot_id: string;
  scenario_id?: string;
  status: AgentRunStatus;
  outcome?: AgentOutcome;
  uncertainty: string;
  objective: string;
  answer?: string;
  model_provider?: string;
  actual_model?: string;
  prompt_version: string;
  tool_schema_version?: string;
  failure_code?: string;
  failure_summary?: string;
  cancel_requested_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
  evidence_count: number;
  steps: AgentStep[];
  evidence_citations: AgentEvidenceCitation[];
  guidance_citations: AgentGuidanceCitation[];
  action_cards: ActionCard[];
  operation_plans: OperationPlan[];
}

export interface AgentContextSnapshot {
  id: string;
  consultation_id?: string;
  run_id?: string;
  subject_type: AgentSubject;
  configuration_revision_id: string;
  scope: OperationalScope;
  resource_refs: TelemetryResourceReference[];
  filters: Record<string, unknown>;
  time_range: TelemetryTimeRange;
  query_definition_refs: string[];
  query_execution_refs: string[];
  evidence_refs: string[];
  content_hash: string;
  created_at: string;
}

export interface ConsultationSummary {
  id: string;
  title: string;
  status: string;
  active_snapshot_id: string;
  active_run?: AgentRun;
  scope: OperationalScope;
  message_count: number;
  created_at: string;
  updated_at: string;
}

export interface ConsultationMessage {
  id: string;
  consultation_id: string;
  run_id?: string;
  context_snapshot_id: string;
  sequence: number;
  role: "owner" | "assistant";
  content: string;
  status: string;
  created_at: string;
  completed_at?: string;
  evidence_citations: AgentEvidenceCitation[];
  guidance_citations: AgentGuidanceCitation[];
}

export interface ConsultationDetail extends ConsultationSummary {
  snapshots: AgentContextSnapshot[];
  messages: ConsultationMessage[];
}

export interface KnowledgeRevision {
  id: string;
  revision: number;
  content: string;
  content_hash: string;
  source_type: string;
  source_consultation_id?: string;
  source_message_id?: string;
  scope: OperationalScope;
  resource_refs: TelemetryResourceReference[];
  review_at?: string;
  expires_at?: string;
  confirmed_by: string;
  created_at: string;
}

export interface KnowledgeItem {
  id: string;
  title: string;
  status: "active" | "disabled" | "deleted";
  current_revision: KnowledgeRevision;
  revisions?: KnowledgeRevision[];
  created_at: string;
  updated_at: string;
}

export interface RunbookGuidance {
  id: string;
  title: string;
  path: string;
  revision: string;
  content?: string;
  modified_at: string;
}

export interface AgentStreamEvent {
  id: string;
  run_id: string;
  consultation_id?: string;
  sequence: number;
  type: string;
  payload: Record<string, unknown>;
  created_at: string;
}

export interface AgentContextInput {
  title: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  resource_refs: TelemetryResourceReference[];
  filters?: Record<string, unknown>;
  from: string;
  to: string;
  query_definition_refs: string[];
  query_execution_refs: string[];
  evidence_refs: string[];
}

export interface CreatedAgentConsultation {
  id: string;
  title: string;
  status: string;
  context_snapshot: {
    id: string;
    consultation_id: string;
    configuration_revision_id: string;
    scope: OperationalScope;
    resource_refs: TelemetryResourceReference[];
    filters?: Record<string, unknown>;
    time_range: TelemetryTimeRange;
    query_definition_refs: string[];
    query_execution_refs: string[];
    evidence_refs: string[];
    content_hash: string;
    created_at: string;
  };
  created_at: string;
}

export interface SaveKnowledgeInput {
  title: string;
  content: string;
  source_consultation_id?: string;
  source_message_id?: string;
  cluster_id: string;
  environment: string;
  namespaces: string[];
  resource_refs: TelemetryResourceReference[];
  review_at?: string;
  expires_at?: string;
}

export interface ActionCardProposalInput {
  run_id: string;
  action_type: "local.change_freeze.set";
  target: {
    cluster_id: string;
    environment: string;
    namespace: string;
    workload_kind: "Deployment";
    workload_name: string;
    scenario_id: string;
  };
  parameters: { enabled: boolean; reason: string };
  preconditions: Array<{
    type: "local.change_freeze";
    expected_enabled: boolean;
    expected_version: number;
  }>;
  risk: string;
  expires_at: string;
}

export interface OperationPlanProposalInput {
  run_id: string;
  action_type: "kubernetes.deployment.scale";
  target: {
    cluster_id: string;
    environment: string;
    namespace: string;
    workload_kind: "Deployment";
    workload_name: string;
    scenario_id: string;
  };
  parameters: { replicas: number };
  intended_state: { replicas: number };
  preconditions: Array<
    | { type: "deployment.replicas"; expected_replicas: number }
    | { type: "deployment.resource_version"; expected_resource_version: string }
    | { type: "local.change_freeze"; expected_enabled: false; expected_version: number }
  >;
  risk: string;
  verification_intent: { type: "kubernetes.deployment.scale"; expected_replicas: number };
  expires_at: string;
}

export async function getAgentInvestigations(signal?: AbortSignal): Promise<AgentRun[]> {
  return (await getJSON<{ items: AgentRun[] }>("/api/v1/agent/investigations?limit=100", { signal })).items;
}

export function getAgentInvestigation(id: string, signal?: AbortSignal): Promise<AgentRun> {
  return getJSON(`/api/v1/agent/investigations/${encodeURIComponent(id)}`, { signal });
}

export function cancelAgentInvestigation(id: string, signal?: AbortSignal): Promise<AgentRun> {
  return postJSON(`/api/v1/agent/investigations/${encodeURIComponent(id)}/cancel`, undefined, { signal });
}

export async function getAgentConsultations(signal?: AbortSignal): Promise<ConsultationSummary[]> {
  return (await getJSON<{ items: ConsultationSummary[] }>("/api/v1/agent/consultations?limit=100", { signal })).items;
}

export function getAgentConsultation(id: string, signal?: AbortSignal): Promise<ConsultationDetail> {
  return getJSON(`/api/v1/agent/consultations/${encodeURIComponent(id)}`, { signal });
}

export function createAgentConsultation(input: AgentContextInput, signal?: AbortSignal): Promise<CreatedAgentConsultation> {
  return postJSON("/api/v1/agent/consultations", input, { signal });
}

export function attachAgentSnapshot(id: string, input: AgentContextInput, signal?: AbortSignal): Promise<AgentContextSnapshot> {
  return postJSON(`/api/v1/agent/consultations/${encodeURIComponent(id)}/snapshots`, input, { signal });
}

export function sendAgentMessage(id: string, content: string, idempotencyKey: string, signal?: AbortSignal): Promise<{ message: ConsultationMessage; run: AgentRun }> {
  return postJSON(`/api/v1/agent/consultations/${encodeURIComponent(id)}/messages`, { content }, {
    headers: { "Idempotency-Key": idempotencyKey },
    signal,
  });
}

export function cancelAgentConsultation(id: string, signal?: AbortSignal): Promise<AgentRun> {
  return postJSON(`/api/v1/agent/consultations/${encodeURIComponent(id)}/cancel`, undefined, { signal });
}

export async function getKnowledgeItems(signal?: AbortSignal): Promise<KnowledgeItem[]> {
  return (await getJSON<{ items: KnowledgeItem[] }>("/api/v1/knowledge-items?limit=100", { signal })).items;
}

export function getKnowledgeItem(id: string, signal?: AbortSignal): Promise<KnowledgeItem> {
  return getJSON(`/api/v1/knowledge-items/${encodeURIComponent(id)}`, { signal });
}

export function createKnowledgeItem(input: SaveKnowledgeInput, signal?: AbortSignal): Promise<KnowledgeItem> {
  return postJSON("/api/v1/knowledge-items", input, { signal });
}

export function updateKnowledgeItem(id: string, input: Partial<SaveKnowledgeInput> & { status?: "active" | "disabled" }, signal?: AbortSignal): Promise<KnowledgeItem> {
  return patchJSON(`/api/v1/knowledge-items/${encodeURIComponent(id)}`, input, { signal });
}

export function deleteKnowledgeItem(id: string, signal?: AbortSignal): Promise<void> {
  return deleteJSON(`/api/v1/knowledge-items/${encodeURIComponent(id)}`, { signal });
}

export async function getRunbookGuidance(signal?: AbortSignal): Promise<RunbookGuidance[]> {
  return (await getJSON<{ items: RunbookGuidance[] }>("/api/v1/runbook-guidance", { signal })).items;
}

export async function getOperationPlans(signal?: AbortSignal): Promise<OperationPlan[]> {
  return (await getJSON<{ items: OperationPlan[] }>("/api/v1/operation-plans?limit=100", { signal })).items;
}

export function proposeActionCard(input: ActionCardProposalInput): Promise<ActionCard> {
  return postJSON("/api/v1/agent/action-cards", input);
}

export function proposeOperationPlan(input: OperationPlanProposalInput): Promise<OperationPlan> {
  return postJSON("/api/v1/operation-plans", input);
}

export function authorizeActionCard(id: string, expectedHash: string, reason: string, signal?: AbortSignal): Promise<ActionCard> {
  return postJSON(`/api/v1/agent/action-cards/${encodeURIComponent(id)}/authorizations`, { expected_hash: expectedHash, reason }, { signal });
}

export function authorizeOperationPlan(id: string, expectedHash: string, reason: string, signal?: AbortSignal): Promise<OperationPlan> {
  return postJSON(`/api/v1/operation-plans/${encodeURIComponent(id)}/authorizations`, { expected_hash: expectedHash, reason }, { signal });
}

const streamEventTypes = [
  "run.created",
  "run.started",
  "tool.started",
  "tool.completed",
  "tool.failed",
  "answer.delta",
  "answer.completed",
  "run.completed",
  "run.failed",
  "run.cancelled",
] as const;

export function openAgentEventStream(
  consultationID: string,
  onEvent: (event: AgentStreamEvent) => void,
  onError?: (event?: Event) => void,
  onOpen?: () => void,
): () => void {
  const source = new EventSource(apiURL(`/api/v1/agent/consultations/${encodeURIComponent(consultationID)}/events`));
  const receive = (message: Event) => {
    try {
      onEvent(JSON.parse((message as MessageEvent<string>).data) as AgentStreamEvent);
    } catch {
      onError?.();
    }
  };
  for (const type of streamEventTypes) source.addEventListener(type, receive);
  source.onopen = () => onOpen?.();
  source.onerror = () => onError?.();
  return () => source.close();
}
