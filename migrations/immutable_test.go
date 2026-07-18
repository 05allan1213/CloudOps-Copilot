package migrations

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestImmutableMigrationHistory(t *testing.T) {
	expected := map[string]string{
		"00001_incident_foundation.sql":                   "8f7dd2e188fba00f6a7cce45f7c2f2319a4f80c7df7882c1a3e4c6471bce080d",
		"00002_agent_runtime.sql":                         "f515765f604391c933bc59b6fd3b7c7d3cfd17f3c429a403bcc014b82d370411",
		"00003_change_intelligence.sql":                   "584fda2c41ca7657228aba5f0b4b0e2d62bd2a4cfd54ec386198c22e163fd0f6",
		"00004_gitops_remediation.sql":                    "fc4773b65f89626bcfb678526d3c631bef89188007df00cda7a96813d7f95c84",
		"00005_delivery_verification.sql":                 "8168e75a60f18ba5b8818c82b6dda71131adc8b9cca1f52eb93abce111f4a61d",
		"00006_observability_verification_postmortem.sql": "8e1a0b1f0a1125ffb54f8049be17617c1130cebbf446ecc371a281b4a7301fdb",
	}
	for name, expectedHash := range expected {
		contents, err := FS.ReadFile(name)
		if err != nil {
			t.Fatalf("read immutable migration %s: %v", name, err)
		}
		actualHash := fmt.Sprintf("%x", sha256.Sum256(contents))
		if actualHash != expectedHash {
			t.Errorf("immutable migration %s sha256=%s, want %s", name, actualHash, expectedHash)
		}
	}
}
