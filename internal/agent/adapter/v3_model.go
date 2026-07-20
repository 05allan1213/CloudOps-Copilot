package adapter

import (
	"context"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

var _ agent.InvestigationModel = (*LLMModel)(nil)

// ProposeDelta exposes the existing bounded LLM client through the V3
// one-durable-step model port. Provider output remains untrusted and is
// reduced by taskhandler.InvestigationStep before any state is persisted.
func (m *LLMModel) ProposeDelta(ctx context.Context, view agent.ModelView) (agent.StateDelta, agent.ModelUsage, error) {
	return callJSON[agent.StateDelta](ctx, m.client,
		"Propose exactly one bounded incident-investigation StateDelta. Return only JSON matching the supplied schema. Use only the current scope_ref, evidence fact IDs, fixed tool names and template IDs present in the input. Never emit shell commands, URLs, provider query languages, credentials, or write actions.",
		view,
	)
}

// SynthesizeDiagnosis asks the provider only for the bounded candidate. The
// deterministic sufficiency result and durable Evidence set remain project
// facts and are validated again before a DiagnosisRecord can be committed.
func (m *LLMModel) SynthesizeDiagnosis(ctx context.Context, view agent.DiagnosisView) (agent.DiagnosisCandidate, agent.ModelUsage, error) {
	return callJSON[agent.DiagnosisCandidate](ctx, m.client,
		"Synthesize one evidence-bound diagnosis candidate. Return only JSON matching the supplied schema. Cite only fact IDs present in the input, preserve unknowns, and do not claim confirmation beyond deterministic sufficiency. Remediation is advisory and limited to the allowed remediation_hint enum.",
		view,
	)
}
