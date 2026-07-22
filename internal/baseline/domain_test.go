package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testSnapshot(t *testing.T) Snapshot {
	t.Helper()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	content := []byte("apiVersion: apps/v1\nkind: Deployment\n")
	s := Snapshot{
		Target: Target{
			Cluster: "kind-cloudops-v3", Environment: "local-demo", Namespace: "demo",
			WorkloadKind: "Deployment", WorkloadName: "demo", ContainerName: "demo",
			Repository: "acme/cloudops-demo", BaseBranch: "main", TargetPath: "apps/demo/deployment.yaml",
		},
		SourceRevision: strings.Repeat("a", 40),
		ImageDigest:    "sha256:" + strings.Repeat("b", 64),
		GitOpsRevision: strings.Repeat("c", 40),
		ConfigHash:     testHash(content),
		VerifiedAt:     now,
	}
	payloads := map[ObservationType]any{
		ObservationArgoRevision:        map[string]any{"deployed_revision": s.GitOpsRevision, "sync_status": "Synced", "operation_phase": "Succeeded"},
		ObservationKubernetesReadiness: map[string]any{"desired": 2, "ready": 2, "available": 2},
		ObservationAlertState:          map[string]any{"firing": 0, "query_valid": true},
		ObservationMetric:              map[string]any{"error_rate": 0.001, "availability": 0.999, "sample_count": 100},
		ObservationLog:                 map[string]any{"required_env_missing": 0, "sample_count": 1},
		ObservationTrace:               map[string]any{"error_rate": 0.001, "sample_count": 20},
		ObservationConfigBlob:          map[string]any{"revision": s.GitOpsRevision, "path": s.Target.TargetPath, "bytes": len(content)},
	}
	for typ, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		hash := testHash(raw)
		if typ == ObservationConfigBlob {
			hash = s.ConfigHash
		}
		s.Observations = append(s.Observations, Observation{
			Type: typ, SourceIdentity: "test/" + string(typ), ObservedJSON: raw,
			ContentHash: hash, ObservedAt: now,
		})
	}
	return s
}

func testHash(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func TestSnapshotFinalizeBindsIdentityAndObservations(t *testing.T) {
	snapshot := testSnapshot(t)
	if err := snapshot.Finalize(); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if len(snapshot.TargetIdentityHash) != 64 || len(snapshot.VerificationHash) != 64 {
		t.Fatalf("hashes were not finalized: target=%q verification=%q", snapshot.TargetIdentityHash, snapshot.VerificationHash)
	}
	if snapshot.PublicID() == "" {
		t.Fatal("PublicID() is empty")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	snapshot.Observations[0].ObservedJSON = json.RawMessage("{\"tampered\":true}")
	if err := snapshot.Validate(); err == nil {
		t.Fatal("tampered observation was accepted")
	}
}

func TestSnapshotRejectsMissingOrUnsafeFacts(t *testing.T) {
	snapshot := testSnapshot(t)
	snapshot.Observations = snapshot.Observations[:len(snapshot.Observations)-1]
	if err := snapshot.Finalize(); err == nil {
		t.Fatal("snapshot without a required observation was accepted")
	}
	snapshot = testSnapshot(t)
	snapshot.Target.TargetPath = "config/secret.yaml"
	if err := snapshot.Finalize(); err == nil {
		t.Fatal("sensitive target path was accepted")
	}
}

func TestObservationRejectsNonObjectAndWrongConfigHash(t *testing.T) {
	snapshot := testSnapshot(t)
	if err := snapshot.Finalize(); err != nil {
		t.Fatal(err)
	}
	observation := snapshot.Observations[0]
	observation.ObservedJSON = json.RawMessage("[\"not\",\"an\",\"object\"]")
	if err := observation.Validate(snapshot.TargetIdentityHash, snapshot.ConfigHash); err == nil {
		t.Fatal("non-object observation was accepted")
	}
	for index := range snapshot.Observations {
		if snapshot.Observations[index].Type == ObservationConfigBlob {
			snapshot.Observations[index].ContentHash = strings.Repeat("f", 64)
			break
		}
	}
	if err := snapshot.Validate(); err == nil {
		t.Fatal("config observation with mismatched content hash was accepted")
	}
}
