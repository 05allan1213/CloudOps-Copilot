import type { AgentContextInput } from "../api/agent";

export const AGENT_CONTEXT_EVENT = "cloudops:agent-context";
export const AGENT_OPEN_EVENT = "cloudops:agent-open";

export interface AgentPageContext {
  route: string;
  input: AgentContextInput;
}

export interface AgentOpenRequest {
  consultationId?: string;
  context?: AgentPageContext;
}

export function publishAgentContext(context: AgentPageContext | null) {
  window.dispatchEvent(new CustomEvent<AgentPageContext | null>(AGENT_CONTEXT_EVENT, { detail: context }));
}

export function openAgentPanel(request: AgentOpenRequest = {}) {
  window.dispatchEvent(new CustomEvent<AgentOpenRequest>(AGENT_OPEN_EVENT, { detail: request }));
}
