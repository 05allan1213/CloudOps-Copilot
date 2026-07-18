package incidentv3mysql

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"

	domain "github.com/05allan1213/CloudOps-Copilot/internal/incident"
)

func TestRetryableTransactionErrorIsNarrow(t *testing.T) {
	for _, code := range []uint16{1205, 1213} {
		if !retryableTransactionError(&drivermysql.MySQLError{Number: code, Message: "retry"}) {
			t.Fatalf("mysql error %d was not retryable", code)
		}
	}
	if retryableTransactionError(&drivermysql.MySQLError{Number: 1062, Message: "duplicate"}) || retryableTransactionError(errors.New("network")) {
		t.Fatal("non-deadlock error was retryable")
	}
}

func TestValidateSignal(t *testing.T) {
	start := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)
	valid := SignalInput{
		Source: "alertmanager", SourceEventID: "sha256:event", AlertInstanceKey: strings.Repeat("a", 64),
		CorrelationKey: "v2:target", Fingerprint: "fp", Status: domain.SignalStatusFiring,
		Severity: domain.SeverityWarning, Cluster: "kind", Environment: "demo", Namespace: "demo",
		ServiceName: "checkout", TargetKind: "Deployment", TargetName: "checkout", Category: "readiness",
		StartsAt: start, OccurredAt: start, Summary: "not ready", Labels: json.RawMessage(`{"safe":"value"}`),
	}
	if err := validateSignal(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.Namespace = "unknown"
	if err := validateSignal(invalid); err == nil {
		t.Fatal("unknown target accepted")
	}
	invalid = valid
	invalid.Severity = domain.Severity("fatal")
	if err := validateSignal(invalid); err == nil {
		t.Fatal("out-of-enum severity accepted")
	}
	resolved := valid
	resolved.Status = domain.SignalStatusResolved
	if err := validateSignal(resolved); err == nil {
		t.Fatal("resolved signal without ends_at accepted")
	}
	end := start.Add(time.Minute)
	resolved.EndsAt = &end
	if err := validateSignal(resolved); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalHashIsLengthPrefixed(t *testing.T) {
	if hashCanonical("ab", "c") == hashCanonical("a", "bc") {
		t.Fatal("ambiguous concatenation produced the same hash")
	}
	if len(hashCanonical("x")) != 64 {
		t.Fatal("hash is not SHA-256 hex")
	}
}
