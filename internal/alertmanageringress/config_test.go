package alertmanageringress

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTargetAllowlistIsTypedStrictAndDeterministic(t *testing.T) {
	targets, err := ParseTargetAllowlist(testTargetJSON())
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ClusterID != "kind-cloudops-demo" || targets[0].ServiceName != "cloudops-demo-workload" {
		t.Fatalf("targets=%+v", targets)
	}
	for name, raw := range map[string]string{
		"unknown field":   `[{"cluster_id":"c","environment":"e","namespace":"n","workload_kind":"Deployment","workload_name":"w","match_labels":{"target":"w"},"extra":true}]`,
		"case alias":      `[{"CLUSTER_ID":"c","environment":"e","namespace":"n","workload_kind":"Deployment","workload_name":"w","match_labels":{"target":"w"}}]`,
		"empty":           `[]`,
		"duplicate":       `[{"cluster_id":"c","environment":"e","namespace":"n","workload_kind":"Deployment","workload_name":"w","match_labels":{"target":"w"}},{"cluster_id":"c2","environment":"e","namespace":"n","workload_kind":"Deployment","workload_name":"w2","match_labels":{"target":"w"}}]`,
		"normalized keys": `[{"cluster_id":"c","environment":"e","namespace":"n","workload_kind":"Deployment","workload_name":"w","match_labels":{"target":"w"," target ":"other"}}]`,
		"multiple values": testTargetJSON() + ` {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTargetAllowlist(raw); err == nil {
				t.Fatalf("invalid allowlist accepted: %s", raw)
			}
		})
	}
}

func TestReadBearerTokenUsesBoundedCredentialFile(t *testing.T) {
	if token, err := ReadBearerToken(""); err != nil || token != nil {
		t.Fatalf("Phase 2 compatibility token=%q err=%v", token, err)
	}
	path := filepath.Join(t.TempDir(), "alertmanager-token")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	token, err := ReadBearerToken(path)
	if err != nil {
		t.Fatal(err)
	}
	verifier := newBearerVerifier(token)
	if !verifier.verify("Bearer 0123456789abcdef0123456789abcdef") || verifier.verify("Bearer wrong-wrong-wrong") {
		t.Fatal("constant-time bearer verifier accepted the wrong credential boundary")
	}
	for _, token := range [][]byte{[]byte("short"), []byte("0123456789abcde 0123456789abcdef")} {
		if _, err := NewHandler(Config{Store: &fakeStore{}, Targets: mustTargets(t), MaxBodyBytes: 1024, BearerToken: token}); err == nil {
			t.Fatalf("invalid bearer token %q was accepted", token)
		}
	}
}

func testTargetJSON() string {
	return `[{"cluster_id":"kind-cloudops-demo","environment":"local-demo","namespace":"cloudops-demo","workload_kind":"Deployment","workload_name":"cloudops-demo-workload","service_name":"cloudops-demo-workload","match_labels":{"cluster":"kind-cloudops-demo","environment":"local-demo","namespace":"cloudops-demo","deployment":"cloudops-demo-workload"}}]`
}
