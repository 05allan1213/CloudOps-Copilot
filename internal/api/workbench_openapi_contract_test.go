package api

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestOpenAPITypedWorkbenchProjectionContracts(t *testing.T) {
	data, err := os.ReadFile("../../docs/api-v1-openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIContract
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}

	wantResponses := map[string]string{
		"/api/v1/incidents/{id}/remediation-plans": "#/components/responses/RemediationPlanPage",
		"/api/v1/incidents/{id}/delivery":          "#/components/responses/DeliveryProjection",
		"/api/v1/incidents/{id}/verifications":     "#/components/responses/VerificationRunPage",
		"/api/v1/incidents/{id}/resolution-report": "#/components/responses/ResolutionReportProjection",
	}
	for path, want := range wantResponses {
		operation := document.Paths[path].Get
		if operation == nil {
			t.Fatalf("typed Workbench operation missing: %s", path)
		}
		response, ok := operation.Responses["200"]
		if !ok {
			t.Fatalf("typed Workbench 200 response missing: %s", path)
		}
		if got, _ := response["$ref"].(string); got != want {
			t.Errorf("%s response ref=%q want=%q", path, got, want)
		}
	}

	for schemaName, value := range map[string]reflect.Type{
		"RemediationPlan":               reflect.TypeOf(RemediationPlanView{}),
		"RemediationTarget":             reflect.TypeOf(RemediationTargetView{}),
		"RemediationTargetResource":     reflect.TypeOf(RemediationTargetResourceView{}),
		"EvidenceBinding":               reflect.TypeOf(EvidenceBindingView{}),
		"RemediationDecision":           reflect.TypeOf(RemediationDecisionView{}),
		"RemediationDecisionActor":      reflect.TypeOf(RemediationDecisionActorView{}),
		"Delivery":                      reflect.TypeOf(DeliveryView{}),
		"VerificationRun":               reflect.TypeOf(VerificationRunView{}),
		"VerificationProfile":           reflect.TypeOf(VerificationProfileView{}),
		"VerificationRevisions":         reflect.TypeOf(VerificationRevisionsView{}),
		"VerificationCommonWindow":      reflect.TypeOf(VerificationCommonWindowView{}),
		"VerificationCheck":             reflect.TypeOf(VerificationCheckView{}),
		"VerificationSubject":           reflect.TypeOf(VerificationSubjectView{}),
		"VerificationSample":            reflect.TypeOf(VerificationSampleView{}),
		"ResolutionReport":              reflect.TypeOf(ResolutionReportView{}),
		"ResolutionRevisions":           reflect.TypeOf(ResolutionRevisionsView{}),
		"ResolutionVerificationProfile": reflect.TypeOf(ResolutionVerificationProfileView{}),
		"ResolutionStability":           reflect.TypeOf(ResolutionStabilityView{}),
	} {
		schema, ok := document.Components.Schemas[schemaName]
		if !ok {
			t.Errorf("OpenAPI schema missing: %s", schemaName)
			continue
		}
		assertSchemaMatchesJSONFields(t, schema, value)
	}

	for _, path := range []string{
		"/api/v1/incidents/{id}/remediation-plans",
		"/api/v1/incidents/{id}/delivery",
		"/api/v1/incidents/{id}/verifications",
		"/api/v1/incidents/{id}/resolution-report",
	} {
		response, _ := document.Paths[path].Get.Responses["200"]["$ref"].(string)
		if strings.Contains(response, "/Resource") {
			t.Errorf("typed Workbench route still references generic Resource: %s -> %s", path, response)
		}
	}
}
