package incidentmysql

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodePostmortemRootCause(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		classification string
		summary        string
		evidence       []string
	}{
		{
			name:           "structured claims",
			raw:            `{"summary":"diagnosis","confirmed_facts":[{"statement":"Deployment had zero ready replicas","evidence_ids":["ev-1","ev-2"],"strong":true}]}`,
			classification: "fact",
			summary:        "Deployment had zero ready replicas",
			evidence:       []string{"ev-1", "ev-2"},
		},
		{
			name:           "historical strings",
			raw:            `{"confirmed_facts":["Deployment was unavailable","Alert was firing"]}`,
			classification: "fact",
			summary:        "Deployment was unavailable; Alert was firing",
			evidence:       []string{"fallback-1"},
		},
		{name: "empty array", raw: `{"confirmed_facts":[]}`, classification: "unknown", summary: "no confirmed facts", evidence: []string{"fallback-1"}},
		{name: "null", raw: `{"confirmed_facts":null}`, classification: "unknown", summary: "no confirmed facts", evidence: []string{"fallback-1"}},
		{name: "malformed confirmed facts", raw: `{"confirmed_facts":{"statement":"bad shape"}}`, classification: "unknown", summary: "could not be decoded", evidence: []string{"fallback-1"}},
		{name: "malformed diagnosis", raw: `{"confirmed_facts":`, classification: "unknown", summary: "JSON was malformed", evidence: []string{"fallback-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decodePostmortemRootCause(json.RawMessage(tt.raw), []string{"fallback-1", "fallback-1", ""})
			if got.Classification != tt.classification || !strings.Contains(got.Summary, tt.summary) {
				t.Fatalf("root cause=%+v", got)
			}
			if strings.Join(got.EvidenceIDs, ",") != strings.Join(tt.evidence, ",") {
				t.Fatalf("evidence=%v want=%v", got.EvidenceIDs, tt.evidence)
			}
		})
	}
}
