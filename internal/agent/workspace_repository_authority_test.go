package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOperationPlanContentHashCanonicalAndMaterial(t *testing.T) {
	expiresAt := time.Date(2026, 7, 28, 12, 0, 0, 123456789, time.UTC)
	base := OperationPlan{
		Authority: "high_impact", ConfigurationRevisionID: "revision-1",
		OperationType:      "kubernetes.deployment.scale",
		Target:             json.RawMessage(`{"cluster_id":"local","environment":"dev","namespace":"demo","workload_kind":"Deployment","workload_name":"api"}`),
		Parameters:         json.RawMessage(`{"replicas":2}`),
		IntendedState:      json.RawMessage(`{"replicas":2}`),
		Preconditions:      json.RawMessage(`[{"type":"deployment.replicas","expected_replicas":1}]`),
		Risk:               "Changes one bounded workload.",
		VerificationIntent: json.RawMessage(`{"type":"kubernetes.deployment.scale","expected_replicas":2}`),
		ExpiresAt:          expiresAt,
	}
	want, err := OperationPlanContentHash(base)
	if err != nil {
		t.Fatal(err)
	}
	canonicalEquivalent := base
	canonicalEquivalent.Target = json.RawMessage(`{ "workload_name":"api", "namespace":"demo", "environment":"dev", "cluster_id":"local", "workload_kind":"Deployment" }`)
	got, err := OperationPlanContentHash(canonicalEquivalent)
	if err != nil || got != want {
		t.Fatalf("canonical equivalent hash=%q want=%q error=%v", got, want, err)
	}
	databaseEquivalent := base
	databaseEquivalent.ExpiresAt = expiresAt.Truncate(time.Microsecond)
	got, err = OperationPlanContentHash(databaseEquivalent)
	if err != nil || got != want {
		t.Fatalf("database timestamp equivalent hash=%q want=%q error=%v", got, want, err)
	}

	mutations := map[string]func(*OperationPlan){
		"authority":              func(value *OperationPlan) { value.Authority = "reversible" },
		"configuration revision": func(value *OperationPlan) { value.ConfigurationRevisionID = "revision-2" },
		"operation type":         func(value *OperationPlan) { value.OperationType = "kubernetes.deployment.restart" },
		"target": func(value *OperationPlan) {
			value.Target = json.RawMessage(`{"cluster_id":"local","environment":"dev","namespace":"demo","workload_kind":"Deployment","workload_name":"worker"}`)
		},
		"parameters":     func(value *OperationPlan) { value.Parameters = json.RawMessage(`{"replicas":3}`) },
		"intended state": func(value *OperationPlan) { value.IntendedState = json.RawMessage(`{"replicas":3}`) },
		"preconditions": func(value *OperationPlan) {
			value.Preconditions = json.RawMessage(`[{"type":"deployment.replicas","expected_replicas":0}]`)
		},
		"risk": func(value *OperationPlan) { value.Risk = "Different risk." },
		"verification intent": func(value *OperationPlan) {
			value.VerificationIntent = json.RawMessage(`{"type":"kubernetes.deployment.scale","expected_replicas":3}`)
		},
		"expiry": func(value *OperationPlan) { value.ExpiresAt = expiresAt.Add(time.Second) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			hash, hashErr := OperationPlanContentHash(changed)
			if hashErr != nil {
				t.Fatal(hashErr)
			}
			if hash == want {
				t.Fatalf("material field %s did not change content hash", name)
			}
		})
	}
}

func TestAuthorityContentHashRejectsInvalidJSON(t *testing.T) {
	_, err := ActionCardContentHash(ActionCard{
		Authority: "reversible", ActionType: "local.change_freeze.set",
		Target: json.RawMessage(`{"cluster_id":`), Parameters: json.RawMessage(`{}`),
		Preconditions: json.RawMessage(`[]`), Risk: "bounded", ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err == nil {
		t.Fatal("invalid authority JSON unexpectedly produced a hash")
	}
}
