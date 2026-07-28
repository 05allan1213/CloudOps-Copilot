package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

const trustedResolutionFacts = `{"candidates":[{"status":"matched","category":"confirmed_match","commit_sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}],"image_resolution":{"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","source":"https://github.com/acme/app","version":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","status":"confirmed","valid":true,"truncated":false,"degraded":false,"registry_metadata":{"registry_id":"registry:abcdef123456","repository":"acme/app","manifest_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","config_digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","revision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","source":"https://github.com/acme/app","version":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","integrity_status":"verified","result_hash":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","valid":true,"truncated":false,"degraded":false,"redaction":{"auth_material_omitted":true,"responses_omitted":true,"policy":"registry_metadata_bounded"}}}}`

func TestChangeDiagnosisRequiresDeterministicallyDeployedEvidence(t *testing.T) {
	id := "evidence-change"
	diagnosis := Diagnosis{Summary: "release regression", Confidence: .9, Hypotheses: []Hypothesis{{Statement: "deployed commit caused the release regression", Confidence: .9, EvidenceIDs: []string{id}}}, ConfirmedFacts: []Claim{{Statement: "revision is deployed", Strong: true, EvidenceIDs: []string{id}}}}
	evidence := map[string]EvidenceRecord{id: {PublicID: id, ToolName: "github.get_commit", Facts: json.RawMessage(`{"sha":"deadbeef"}`), Valid: true}}
	problems := ValidateDiagnosis(diagnosis, evidence)
	if !containsProblem(problems, "deterministically correlated") {
		t.Fatalf("unconfirmed commit accepted: %v", problems)
	}
	evidence[id] = EvidenceRecord{PublicID: id, ToolName: "change.list_recent", Facts: json.RawMessage(trustedResolutionFacts), Valid: true}
	if problems := ValidateDiagnosis(diagnosis, evidence); len(problems) != 0 {
		t.Fatalf("confirmed candidate rejected: %v", problems)
	}
}

func TestChangeDiagnosisRejectsArgoOnlyForeignInvalidTruncatedAndDegradedResolution(t *testing.T) {
	id := "change"
	diagnosis := Diagnosis{Summary: "release", Confidence: .9, ConfirmedFacts: []Claim{{Statement: "deployed image caused the release", Strong: true, EvidenceIDs: []string{id}}}}
	base := trustedResolutionFacts
	variants := map[string]string{
		"argo only": `{"candidates":[{"status":"matched","category":"confirmed_match","argocd_deployed_revision":"deadbeef"}]}`,
		"foreign":   strings.Replace(base, `"status":"confirmed"`, `"status":"conflict"`, 1),
		"invalid":   strings.Replace(base, `"valid":true`, `"valid":false`, 1),
		"truncated": strings.Replace(base, `"truncated":false`, `"truncated":true`, 1),
		"degraded":  strings.Replace(base, `"degraded":false`, `"degraded":true`, 1),
	}
	for name, facts := range variants {
		t.Run(name, func(t *testing.T) {
			evidence := map[string]EvidenceRecord{id: {PublicID: id, ToolName: "change.list_recent", Facts: json.RawMessage(facts), Valid: true}}
			if problems := ValidateDiagnosis(diagnosis, evidence); !containsProblem(problems, "deterministically correlated") {
				t.Fatalf("unsafe evidence accepted: %v", problems)
			}
		})
	}
}

func TestChangeDiagnosisRejectsExcludedForeignSensitiveAndWriteInstructions(t *testing.T) {
	id := "excluded"
	evidence := map[string]EvidenceRecord{id: {PublicID: id, ToolName: "change.list_recent", Facts: json.RawMessage(`{"candidates":[{"status":"excluded","category":"excluded"}]}`), Valid: true}}
	diagnosis := Diagnosis{Summary: "candidate", Confidence: .9, ConfirmedFacts: []Claim{{Statement: "commit is root cause", Strong: true, EvidenceIDs: []string{id}}}, RecommendedNextActions: []string{"argocd sync checkout"}}
	problems := ValidateDiagnosis(diagnosis, evidence)
	if !containsProblem(problems, "deterministically correlated") || !containsProblem(problems, "prohibited") {
		t.Fatalf("unsafe diagnosis problems=%v", problems)
	}
	diagnosis.RecommendedNextActions = nil
	diagnosis.ConfirmedFacts[0].EvidenceIDs = []string{"foreign"}
	if problems := ValidateDiagnosis(diagnosis, evidence); !containsProblem(problems, "unknown evidence") {
		t.Fatalf("foreign evidence accepted: %v", problems)
	}
	evidence[id] = EvidenceRecord{PublicID: id, ToolName: "change.list_recent", Facts: json.RawMessage(`{"category":"confirmed_match"}`), Valid: false}
	diagnosis.ConfirmedFacts[0].EvidenceIDs = []string{id}
	if problems := ValidateDiagnosis(diagnosis, evidence); !containsProblem(problems, "lacks valid") {
		t.Fatalf("invalid sensitive evidence accepted: %v", problems)
	}
}

func containsProblem(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
