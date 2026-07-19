package agent

import "testing"

func TestGoldenRequiredEnvSufficiencyReadyOnlyWithCompleteTruthTable(t *testing.T) {
	facts := goldenFacts()
	result, err := EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: facts, Policy: GoldenRequiredEnvClaimPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != SufficiencyReady || result.ConfidenceCap != 1 || len(result.MissingFacets) != 0 || len(result.SupportingIDs) != 9 {
		t.Fatalf("result=%+v", result)
	}

	withoutLog := append([]EvidenceFact(nil), facts[:7]...)
	withoutLog = append(withoutLog, facts[8:]...)
	result, err = EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: withoutLog, Policy: GoldenRequiredEnvClaimPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != SufficiencyContinue || !containsString(result.MissingFacets, "log-symptom") {
		t.Fatalf("missing log result=%+v", result)
	}
}

func TestSufficiencyContradictionAndUnavailableSourceFailClosed(t *testing.T) {
	facts := goldenFacts()
	blocking := goldenFact("blocking", "kubernetes.required_env_present", "kubernetes", "podspec", true)
	result, err := EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: append(facts, blocking), Policy: GoldenRequiredEnvClaimPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != SufficiencyInsufficient || !containsString(result.ReasonCodes, "blocking_contradiction") || len(result.BlockingIDs) != 1 {
		t.Fatalf("blocking result=%+v", result)
	}

	result, err = EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: facts[:6], Policy: GoldenRequiredEnvClaimPolicy(), RequiredSourcesUnavailable: []string{"elasticsearch"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != SufficiencyInsufficient || !containsString(result.ReasonCodes, "required_source_unavailable") {
		t.Fatalf("unavailable result=%+v", result)
	}
}

func TestSufficiencyRejectsStaleCrossCycleAndSameCollectorFacts(t *testing.T) {
	policy := ClaimPolicy{
		Version: "test/v1", ClaimType: "test", MinIndependentCollectors: 2, RequireDirectFact: true,
		Requirements: []FactRequirement{{Facet: "a", AnyOf: []string{"fact.a"}}, {Facet: "b", AnyOf: []string{"fact.b"}}},
	}
	first := goldenFact("a", "fact.a", "prometheus", "same-query", true)
	second := goldenFact("b", "fact.b", "prometheus", "same-query", false)
	result, err := EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: []EvidenceFact{first, second}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != SufficiencyContinue || !containsString(result.ReasonCodes, "insufficient_independent_collectors") {
		t.Fatalf("same collector result=%+v", result)
	}

	second.SourceSystem, second.CollectionPath = "elasticsearch", "structured-log"
	second.Freshness = "stale"
	result, err = EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: []EvidenceFact{first, second}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.MissingFacets, "b") {
		t.Fatalf("stale fact was counted: %+v", result)
	}

	second.Freshness, second.CycleNo = "fresh", 99
	result, err = EvaluateSufficiency(SufficiencyInput{IncidentID: "incident-1", CycleNo: 2, Facts: []EvidenceFact{first, second}, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(result.MissingFacets, "b") {
		t.Fatalf("cross-cycle fact was counted: %+v", result)
	}
}

func goldenFacts() []EvidenceFact {
	types := []struct {
		id, factType, source, path string
		direct                     bool
	}{
		{"subject", "workload.subject_confirmed", "kubernetes", "deployment", true},
		{"diff", "gitops.required_env_removed", "github", "exact-diff", true},
		{"argo", "argocd.bad_revision_deployed", "argocd", "application-history", true},
		{"pod", "kubernetes.required_env_absent", "kubernetes", "podspec", true},
		{"source", "source_revision.unchanged", "registry", "image-config", true},
		{"image", "image_digest.unchanged", "registry", "manifest", true},
		{"metric", "metric.readiness_or_5xx_failure", "prometheus", "readiness-v1", true},
		{"log", "log.required_env_missing", "elasticsearch", "env-missing-v1", true},
		{"trace", "trace.request_failure", "tempo", "request-failure-v1", true},
	}
	facts := make([]EvidenceFact, 0, len(types))
	for _, item := range types {
		facts = append(facts, goldenFact(item.id, item.factType, item.source, item.path, item.direct))
	}
	return facts
}

func goldenFact(id, factType, source, path string, direct bool) EvidenceFact {
	return EvidenceFact{
		ID: id, EvidenceID: "evidence-" + id, IncidentID: "incident-1", CycleNo: 2,
		Type: factType, SourceSystem: source, CollectionPath: path, CorroborationGroup: source + "/" + path,
		Authority: "runtime_observation", Integrity: "verified", Freshness: "fresh", Completeness: "complete",
		ClaimUse: "support", CollectionStatus: CollectionAvailable, Direct: direct,
	}
}
