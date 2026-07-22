package apiv3

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	resolutionReportID      = "323e4567-e89b-12d3-a456-426614174000"
	resolutionSignalID      = "423e4567-e89b-12d3-a456-426614174000"
	resolutionEvidenceID    = "523e4567-e89b-12d3-a456-426614174000"
	resolutionAgentRunID    = "623e4567-e89b-12d3-a456-426614174000"
	resolutionPlanID        = "723e4567-e89b-12d3-a456-426614174000"
	resolutionDecisionID    = "823e4567-e89b-12d3-a456-426614174000"
	resolutionDeliveryID    = "923e4567-e89b-12d3-a456-426614174000"
	resolutionRunID         = "a23e4567-e89b-12d3-a456-426614174000"
	resolutionCheckID       = "b23e4567-e89b-12d3-a456-426614174000"
	resolutionSampleID      = "c23e4567-e89b-12d3-a456-426614174000"
	resolutionEventID       = "d23e4567-e89b-12d3-a456-426614174000"
	resolutionObservationID = "e23e4567-e89b-12d3-a456-426614174000"
)

func TestResolutionReportQueryUsesV3CurrentCycleProjection(t *testing.T) {
	query := strings.ToLower(resolutionReportQuery)
	for _, required := range []string{
		"from resolution_reports r",
		"join incidents i",
		"i.public_id = ?",
		"i.domain_schema_version = 3",
		"r.domain_schema_version = 3",
		"i.cycle_no = r.cycle_no",
		"r.cycle_no = i.cycle_no",
	} {
		if !strings.Contains(query, required) {
			t.Errorf("ResolutionReport query is missing %q", required)
		}
	}
	if strings.Contains(query, "postmortems") {
		t.Fatal("ResolutionReport query still reads the legacy postmortems table")
	}
	selectList := strings.SplitN(query, "from resolution_reports", 2)[0]
	for _, forbidden := range []string{"r.id,", "r.incident_id", "r.verification_run_id", "r.initial_signal_id", "r.trigger_signal_id", "r.remediation_plan_id", "r.remediation_decision_id", "r.change_request_id"} {
		if strings.Contains(selectList, forbidden) {
			t.Errorf("ResolutionReport SELECT exposes numeric database identity %q", forbidden)
		}
	}
}

func TestResolutionReportHandlerReturnsCompleteSanitizedPostDeliveryProjection(t *testing.T) {
	report := validPostDeliveryResolutionReport()
	queries := &resolutionReportCapturePort{report: report}
	engine := newContractEngine(NewHandler(Config{Queries: queries}))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v3/incidents/"+contractIncidentID+"/resolution-report", nil)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if queries.request.Kind != QueryResolutionReport || queries.request.IncidentID != contractIncidentID {
		t.Fatalf("query request=%+v", queries.request)
	}
	var body resolutionReportResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Resource
	if got.ID != resolutionReportID || got.Kind != string(QueryResolutionReport) || got.Status != "resolved" || got.Cycle != 3 {
		t.Fatalf("identity=%+v", got)
	}
	if got.TriggerType != "post_delivery" || got.ResolutionReason != "recovered_after_remediation" ||
		got.Revisions.BadGitOpsRevision == "" || got.Revisions.FixGitOpsRevision == "" ||
		len(got.Diagnosis) == 0 || len(got.RemediationPlan) == 0 || len(got.RemediationDecision) == 0 || len(got.Delivery) == 0 {
		t.Fatalf("incomplete post-delivery report=%+v", got)
	}
	if strings.Contains(response.Body.String(), `"check_id":42`) || strings.Contains(response.Body.String(), `"internal_id"`) {
		t.Fatalf("internal numeric identity leaked: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), resolutionSampleID) || !strings.Contains(response.Body.String(), resolutionCheckID) {
		t.Fatalf("public verification identities were lost: %s", response.Body.String())
	}
}

func TestScanResolutionReportProjectsDurableFields(t *testing.T) {
	want := validPostDeliveryResolutionReport()
	item, err := scanResolutionReport(resolutionReportScannerFixture{values: []any{
		want.ID, uint64(1), want.Cycle, want.TriggerType, want.ResolutionReason,
		want.Service, want.Workload, want.Environment, want.ImpactSummary,
		want.CycleStartedAt, want.ResolvedAt, want.MeasuredDurationMS,
		want.Revisions.BadGitOpsRevision, want.Revisions.FixGitOpsRevision,
		want.Revisions.SourceRevision, want.Revisions.ImageDigest, want.Revisions.GitOpsRevision,
		want.VerificationProfile.ID, want.VerificationProfile.Hash,
		want.Stability.CommonWindowStartedAt, want.Stability.CommonWindowCompletedAt,
		want.TriggerSignal, want.Diagnosis, want.Evidence, want.RemediationPlan,
		want.RemediationDecision, want.Delivery, want.Verification, want.Timeline,
		want.AgentUsage, want.Summary, want.Hash, want.GeneratedAt, want.MigratedLegacyContext,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != want.ID || item.Cycle != want.Cycle || item.TriggerType != want.TriggerType ||
		item.ResolutionReason != want.ResolutionReason || item.Revisions != want.Revisions ||
		item.VerificationProfile != want.VerificationProfile || item.Stability != want.Stability {
		t.Fatalf("projected item=%+v", item)
	}
	if strings.Contains(string(item.Verification), `"check_id"`) {
		t.Fatalf("numeric verification check key leaked: %s", item.Verification)
	}
}

func TestResolutionReportNoChangeKeepsOptionalSectionsNull(t *testing.T) {
	report := validPostDeliveryResolutionReport()
	report.TriggerType = "no_change_signal"
	report.ResolutionReason = "recovered_without_change"
	report.VerificationProfile.ID = "no-change/v1"
	report.Revisions.BadGitOpsRevision = ""
	report.Revisions.FixGitOpsRevision = ""
	report.Diagnosis = nil
	report.RemediationPlan = nil
	report.RemediationDecision = nil
	report.Delivery = nil
	report.TriggerSignal = json.RawMessage(`{"public_id":"` + resolutionSignalID + `","status":"resolved"}`)
	if err := validateResolutionReportView(report); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(resolutionReportResponse{Resource: *report})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"diagnosis":null`, `"remediation_plan":null`, `"remediation_decision":null`, `"delivery":null`} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("no-change response is missing %s: %s", field, encoded)
		}
	}
	if strings.Contains(string(encoded), "bad_gitops_revision") || strings.Contains(string(encoded), "fix_gitops_revision") {
		t.Fatalf("no-change response fabricated bad/fix revisions: %s", encoded)
	}
}

func TestMemoryQueryPortKeepsTypedResolutionReportProjection(t *testing.T) {
	projection := NewMemoryQueryPort()
	if err := projection.PutIncident(IncidentView{
		ID: contractIncidentID, Cycle: 3, Status: "resolved", Severity: "critical", Version: 1,
	}); err != nil {
		t.Fatal(err)
	}
	report := validPostDeliveryResolutionReport()
	if err := projection.PutResolutionReport(contractIncidentID, *report); err != nil {
		t.Fatal(err)
	}
	result, err := projection.Query(context.Background(), QueryRequest{Kind: QueryResolutionReport, IncidentID: contractIncidentID})
	if err != nil || result.ResolutionReport == nil || result.Resource != nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	result.ResolutionReport.Verification[0] = '['
	again, err := projection.Query(context.Background(), QueryRequest{Kind: QueryResolutionReport, IncidentID: contractIncidentID})
	if err != nil || again.ResolutionReport == nil || again.ResolutionReport.Verification[0] != '{' {
		t.Fatalf("stored projection was mutated: result=%+v err=%v", again, err)
	}
}

func TestResolutionReportProjectionRejectsUnsafeOrInvalidJSON(t *testing.T) {
	oversized := json.RawMessage(`{"padding":"` + strings.Repeat("x", maxResolutionAgentUsageJSONBytes) + `"}`)
	for name, mutate := range map[string]func(*ResolutionReportView){
		"secret-like key": func(report *ResolutionReportView) {
			report.Evidence = json.RawMessage(`{"items":[{"secret":"provider-token"}]}`)
		},
		"numeric internal id": func(report *ResolutionReportView) {
			report.Evidence = json.RawMessage(`{"items":[{"internal_id":42}]}`)
		},
		"numeric generic id": func(report *ResolutionReportView) {
			report.Evidence = json.RawMessage(`{"items":[{"id":42}]}`)
		},
		"malformed JSON": func(report *ResolutionReportView) {
			report.Timeline = json.RawMessage(`{"events":[`)
		},
		"oversized JSON": func(report *ResolutionReportView) {
			report.AgentUsage = oversized
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := validPostDeliveryResolutionReport()
			mutate(report)
			if err := validateResolutionReportView(report); !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func validPostDeliveryResolutionReport() *ResolutionReportView {
	cycleStarted := time.Date(2026, 7, 21, 1, 0, 0, 0, time.UTC)
	resolved := cycleStarted.Add(5 * time.Minute)
	return &ResolutionReportView{
		ID:                 resolutionReportID,
		Kind:               string(QueryResolutionReport),
		Status:             "resolved",
		Cycle:              3,
		TriggerType:        "post_delivery",
		ResolutionReason:   "recovered_after_remediation",
		Service:            "checkout",
		Workload:           "checkout-api",
		Environment:        "production",
		ImpactSummary:      "checkout latency exceeded the SLO",
		Summary:            "Verification passed after the common stability window",
		Hash:               strings.Repeat("f", 64),
		CycleStartedAt:     cycleStarted,
		ResolvedAt:         resolved,
		MeasuredDurationMS: uint64(resolved.Sub(cycleStarted).Milliseconds()),
		GeneratedAt:        resolved,
		Revisions: ResolutionRevisionsView{
			BadGitOpsRevision: strings.Repeat("a", 40),
			FixGitOpsRevision: strings.Repeat("b", 40),
			SourceRevision:    strings.Repeat("c", 40),
			ImageDigest:       "sha256:" + strings.Repeat("d", 64),
			GitOpsRevision:    strings.Repeat("b", 40),
		},
		VerificationProfile: ResolutionVerificationProfileView{
			ID: "golden-required-env/v1", Hash: strings.Repeat("e", 64),
		},
		Stability: ResolutionStabilityView{
			CommonWindowStartedAt: resolved.Add(-70 * time.Second), CommonWindowCompletedAt: resolved,
		},
		TriggerSignal:       json.RawMessage(`{"public_id":"` + resolutionSignalID + `","status":"firing"}`),
		Diagnosis:           json.RawMessage(`{"candidate":{"summary":"required env missing"},"evidence_ids":["` + resolutionEvidenceID + `"],"diagnosis_hash":"` + strings.Repeat("1", 64) + `"}`),
		Evidence:            json.RawMessage(`{"evidence_count":1,"items":[{"id":"` + resolutionEvidenceID + `","content_hash":"` + strings.Repeat("2", 64) + `"}]}`),
		RemediationPlan:     json.RawMessage(`{"id":"` + resolutionPlanID + `","creator_agent_run_id":"` + resolutionAgentRunID + `","canonical_plan_hash":"` + strings.Repeat("3", 64) + `"}`),
		RemediationDecision: json.RawMessage(`{"id":"` + resolutionDecisionID + `","decision":"approved"}`),
		Delivery:            json.RawMessage(`{"id":"` + resolutionDeliveryID + `","observations":{"latest_by_kind":[{"id":"` + resolutionObservationID + `","kind":"ci"}]}}`),
		Verification:        json.RawMessage(`{"run_id":"` + resolutionRunID + `","status":"passed","checks":[{"id":"` + resolutionCheckID + `","status":"passed"}],"samples":{"latest_by_check":[{"id":"` + resolutionSampleID + `","check_id":42,"status":"passed"}]}}`),
		Timeline:            json.RawMessage(`{"event_count":1,"events":[{"id":"` + resolutionEventID + `","event_type":"incident_resolved"}]}`),
		AgentUsage:          json.RawMessage(`{"agent_runs":1,"steps":4,"tool_calls":3,"model_calls":2,"tokens":1200}`),
	}
}

type resolutionReportCapturePort struct {
	request QueryRequest
	report  *ResolutionReportView
}

func (p *resolutionReportCapturePort) Query(_ context.Context, request QueryRequest) (QueryResponse, error) {
	p.request = request
	return QueryResponse{ResolutionReport: p.report}, nil
}

type resolutionReportScannerFixture struct {
	values []any
}

func (s resolutionReportScannerFixture) Scan(destinations ...any) error {
	if len(destinations) != len(s.values) {
		return fmt.Errorf("scan destinations=%d values=%d", len(destinations), len(s.values))
	}
	for index, destination := range destinations {
		value := s.values[index]
		switch target := destination.(type) {
		case *string:
			converted, ok := value.(string)
			if !ok {
				return fmt.Errorf("scan value %d is %T, want string", index, value)
			}
			*target = converted
		case *uint64:
			converted, ok := value.(uint64)
			if !ok {
				return fmt.Errorf("scan value %d is %T, want uint64", index, value)
			}
			*target = converted
		case *bool:
			converted, ok := value.(bool)
			if !ok {
				return fmt.Errorf("scan value %d is %T, want bool", index, value)
			}
			*target = converted
		case *time.Time:
			converted, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("scan value %d is %T, want time.Time", index, value)
			}
			*target = converted
		case *sql.NullString:
			if value == nil {
				*target = sql.NullString{}
				continue
			}
			converted, ok := value.(string)
			if !ok {
				return fmt.Errorf("scan value %d is %T, want nullable string", index, value)
			}
			*target = sql.NullString{String: converted, Valid: true}
		case *[]byte:
			switch converted := value.(type) {
			case nil:
				*target = nil
			case []byte:
				*target = append([]byte(nil), converted...)
			case json.RawMessage:
				*target = append([]byte(nil), converted...)
			default:
				return fmt.Errorf("scan value %d is %T, want JSON bytes", index, value)
			}
		default:
			return fmt.Errorf("unsupported scan destination %d: %T", index, destination)
		}
	}
	return nil
}
