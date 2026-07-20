export type SessionRole = "viewer" | "operator";

export interface SessionActor {
  provider: string;
  login: string;
  role: SessionRole;
}

export interface SessionResponse {
  token: string;
  expires_at: string;
  actor: SessionActor;
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  detail: string;
  instance: string;
  code: string;
  request_id: string;
  trace_id: string;
}
