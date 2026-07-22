package alertmanageringress

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCanonicalV2IdentitiesAndMultiAlertCorrelation(t *testing.T) {
	targets := mustTargets(t)
	first := testAlert("firing", "abc123", "WorkloadNotReady", "critical")
	second := testAlert("firing", "def456", "HighErrorRate", "warning")
	batch, err := normalizeEnvelope(testEnvelope(first, second), targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Signals) != 2 || len(batch.Rejections) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	left, right := batch.Signals[0], batch.Signals[1]
	if left.SourceEventID != "v2:bb3466ad8f87503540c45fcc86c2f8e859e3f62acf31d1817481a1ff3bcae7ed" ||
		left.AlertInstanceKey != "725b3a18075a23308e7ef19160fcde920f1463257b4f3f842ac0bdfff79f1452" ||
		left.CorrelationKey != "v2:6cc0330d16aa0610192c1880497fca6c315222d55822271ea2f47d5b88a7c037" {
		t.Fatalf("canonical identity vector source=%q instance=%q correlation=%q", left.SourceEventID, left.AlertInstanceKey, left.CorrelationKey)
	}
	if left.CorrelationKey != right.CorrelationKey || !strings.HasPrefix(left.CorrelationKey, "v2:") || len(left.CorrelationKey) != 67 {
		t.Fatalf("correlation identities=%q/%q", left.CorrelationKey, right.CorrelationKey)
	}
	if left.SourceEventID == right.SourceEventID || left.AlertInstanceKey == right.AlertInstanceKey {
		t.Fatal("distinct alert instances shared an event or instance identity")
	}
	if left.Category == right.Category {
		t.Fatal("test setup did not vary the alert symptom category")
	}
}

func TestCanonicalSourceEventResolvedEndsAtAndFiringPredictionRules(t *testing.T) {
	targets := mustTargets(t)
	firing := testAlert("firing", "abc123", "WorkloadNotReady", "critical")
	predicted := firing
	predicted.EndsAt = time.Date(2026, 7, 18, 13, 0, 0, 0, time.UTC)
	first, err := normalizeEnvelope(testEnvelope(firing), targets)
	if err != nil {
		t.Fatal(err)
	}
	second, err := normalizeEnvelope(testEnvelope(predicted), targets)
	if err != nil {
		t.Fatal(err)
	}
	if first.Signals[0].SourceEventID != second.Signals[0].SourceEventID || first.Signals[0].EndsAt != nil || second.Signals[0].EndsAt != nil {
		t.Fatal("firing identity drifted with Alertmanager's predicted endsAt")
	}
	resolvedA := firing
	resolvedA.Status = "resolved"
	resolvedA.EndsAt = time.Date(2026, 7, 18, 12, 5, 0, 0, time.UTC)
	resolvedB := resolvedA
	resolvedB.EndsAt = resolvedA.EndsAt.Add(time.Second)
	a, err := normalizeEnvelope(testEnvelope(resolvedA), targets)
	if err != nil {
		t.Fatal(err)
	}
	b, err := normalizeEnvelope(testEnvelope(resolvedB), targets)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signals[0].SourceEventID == b.Signals[0].SourceEventID || a.Signals[0].AlertInstanceKey != b.Signals[0].AlertInstanceKey {
		t.Fatal("resolved source_event_id did not bind real endsAt independently from alert_instance_key")
	}
}

func TestTargetAllowlistRejectsUnknownConflictAndAmbiguity(t *testing.T) {
	allowed := testAlert("firing", "abc123", "WorkloadNotReady", "critical")
	unknown := allowed
	unknown.Labels = cloneMap(allowed.Labels)
	unknown.Labels["deployment"] = "cloudops-api"
	batch, err := normalizeEnvelope(testEnvelope(allowed, unknown), mustTargets(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Signals) != 1 || len(batch.Rejections) != 1 || batch.Rejections[0].ReasonCode != "target_not_allowlisted" {
		t.Fatalf("unknown target batch=%+v", batch)
	}

	conflict := allowed
	conflict.Labels = cloneMap(allowed.Labels)
	conflict.Labels["target_name"] = "injected-workload"
	batch, err = normalizeEnvelope(testEnvelope(conflict), mustTargets(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Signals) != 0 || len(batch.Rejections) != 1 || batch.Rejections[0].ReasonCode != "target_label_conflict" {
		t.Fatalf("conflicting target batch=%+v", batch)
	}

	targets := mustTargets(t)
	overlap := targets[0]
	overlap.MatchLabels = map[string]string{"deployment": "demo"}
	batch, err = normalizeEnvelope(testEnvelope(allowed), append(targets, overlap))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Rejections) != 1 || batch.Rejections[0].ReasonCode != "target_selector_ambiguous" {
		t.Fatalf("ambiguous target batch=%+v", batch)
	}
}

func TestExternalTextIsAllowlistedControlCleanedAndSecretRedacted(t *testing.T) {
	item := testAlert("firing", "abc123", "WorkloadNotReady", "critical")
	secretCanary := "AbCdEfGhIjKlMnOpQrStUvWxYz012345"
	item.Annotations = map[string]string{
		"summary":     "failure\nBearer bearer-token-0123456789 token=token-value-0123456789 " + secretCanary,
		"description": "-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----",
		"token":       "must-never-be-selected",
	}
	item.Labels["api_key"] = "label-secret-must-never-be-selected"
	batch, err := normalizeEnvelope(testEnvelope(item), mustTargets(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Signals) != 1 {
		t.Fatalf("batch=%+v", batch)
	}
	signal := batch.Signals[0]
	var annotations map[string]string
	if err := json.Unmarshal(signal.Annotations, &annotations); err != nil {
		t.Fatal(err)
	}
	combined := signal.Summary + " " + string(signal.Annotations) + " " + string(signal.Labels)
	for _, forbidden := range []string{"bearer-token-0123456789", "token-value-0123456789", secretCanary, "private-material", "must-never-be-selected", "label-secret-must-never-be-selected", `"token"`, `"api_key"`, "\n"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("persisted signal retained %q: %s", forbidden, combined)
		}
	}
	if !strings.Contains(signal.Summary, "[REDACTED]") || !strings.Contains(annotations["description"], "[REDACTED_PRIVATE_KEY]") {
		t.Fatalf("redaction markers missing summary=%q annotations=%v", signal.Summary, annotations)
	}

	unknown := item
	unknown.Labels = cloneMap(item.Labels)
	unknown.Labels["deployment"] = "not-allowlisted"
	batch, err = normalizeEnvelope(testEnvelope(unknown), mustTargets(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Rejections) != 1 {
		t.Fatalf("rejection batch=%+v", batch)
	}
	rejectionJSON, err := json.Marshal(batch.Rejections[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{secretCanary, "private-material", "token-value"} {
		if strings.Contains(string(rejectionJSON), forbidden) {
			t.Fatalf("rejection retained secret %q: %s", forbidden, rejectionJSON)
		}
	}
}

func TestExternalTextRedactsLowercaseEntropyAndPreservesUTF8ByteBound(t *testing.T) {
	lowercaseSecret := "qjtzmavpwnxkrcyfbuegdhilsoqv"
	if redacted := redactExternalText(lowercaseSecret, 128); redacted != "[REDACTED_HIGH_ENTROPY]" {
		t.Fatalf("lowercase high-entropy token was not redacted: %q", redacted)
	}
	item := testAlert("firing", "abc123", strings.Repeat("界", 43), "critical")
	item.Annotations = map[string]string{}
	batch, err := normalizeEnvelope(testEnvelope(item), mustTargets(t))
	if err != nil {
		t.Fatal(err)
	}
	category := batch.Signals[0].Category
	if !utf8.ValidString(category) || len(category) > 128 || category == "" {
		t.Fatalf("category is not a valid bounded UTF-8 value: bytes=%d value=%q", len(category), category)
	}
	if !utf8.ValidString(batch.Signals[0].Summary) {
		t.Fatalf("fallback summary is invalid UTF-8: %q", batch.Signals[0].Summary)
	}
}

func mustTargets(t *testing.T) []Target {
	t.Helper()
	targets, err := ParseTargetAllowlist(testTargetJSON())
	if err != nil {
		t.Fatal(err)
	}
	return targets
}

func testEnvelope(alerts ...alert) envelope {
	status := "firing"
	if len(alerts) == 1 {
		status = alerts[0].Status
	}
	return envelope{
		Version: "4", GroupKey: "{}:{alertname=demo}", Status: status, Receiver: "cloudops-demo",
		GroupLabels: map[string]string{}, CommonLabels: map[string]string{},
		CommonAnnotations: map[string]string{}, ExternalURL: "http://alertmanager:9093", Alerts: alerts,
	}
}

func testAlert(status, fingerprint, alertName, severity string) alert {
	return alert{
		Status: status, Fingerprint: fingerprint,
		StartsAt: time.Date(2026, 7, 18, 12, 0, 0, 123456000, time.UTC),
		Labels: map[string]string{
			"alertname": alertName, "severity": severity, "cluster": "kind-cloudops-v3",
			"environment": "local-demo", "namespace": "demo",
			"service": "demo", "deployment": "demo",
		},
		Annotations: map[string]string{"summary": "demo regression"},
	}
}

func cloneMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
