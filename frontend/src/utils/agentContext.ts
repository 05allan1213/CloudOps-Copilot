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

export interface AgentRouteSelection {
  consultationID: string;
  investigationID: string;
}

export function queryString(value: unknown): string {
  const candidate = Array.isArray(value) ? value[0] : value;
  return typeof candidate === "string" ? candidate : "";
}

export function readAgentRouteSelection(query: Record<string, unknown>): AgentRouteSelection {
  return {
    consultationID: queryString(query.consultation),
    investigationID: queryString(query.investigation || query.run),
  };
}

export function agentContextHasEvidence(input: AgentContextInput | null | undefined): boolean {
  return Boolean(input && (input.query_execution_refs.length > 0 || input.evidence_refs.length > 0));
}

export function freeQueryContext(context: AgentPageContext): AgentPageContext {
  return {
    ...context,
    input: {
      ...context.input,
      title: `${context.input.title} · 自由查询`,
      filters: { ...context.input.filters, agent_entry: "free_query", unassociated_event: true },
    },
  };
}

export function publishAgentContext(context: AgentPageContext | null) {
  window.dispatchEvent(new CustomEvent<AgentPageContext | null>(AGENT_CONTEXT_EVENT, { detail: context }));
}

export function openAgentPanel(request: AgentOpenRequest = {}) {
  window.dispatchEvent(new CustomEvent<AgentOpenRequest>(AGENT_OPEN_EVENT, { detail: request }));
}

export function shouldStopGlobalAgent(open: boolean, routePath: string): boolean {
  return !open && routePath !== "/agent";
}

export function shouldClearAgentContextOnUnmount(routePath: string): boolean {
  return routePath !== "/agent";
}
