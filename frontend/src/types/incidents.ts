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
  created_at?: string;
  updated_at?: string;
}

export interface CollectionResponse<T> {
  items: T[];
  next_cursor?: string;
}

export interface IncidentResponse {
  incident: IncidentView;
}

export interface ResourceResponse {
  resource: ResourceView;
}

export interface CommandResponse {
  id: string;
  command: string;
  status: string;
  version?: number;
  cycle?: number;
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
