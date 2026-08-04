package api

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestHandwrittenOpenAPIMatchesRoutes(t *testing.T) {
	data, err := os.ReadFile("../../docs/api-v1-openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		OpenAPI    string                    `yaml:"openapi"`
		Paths      map[string]map[string]any `yaml:"paths"`
		Components map[string]map[string]any `yaml:"components"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.OpenAPI != "3.1.0" {
		t.Fatalf("openapi=%q", document.OpenAPI)
	}
	actual := make(map[string]bool)
	for path, operations := range document.Paths {
		for method := range operations {
			switch strings.ToUpper(method) {
			case "GET", "POST", "PUT", "PATCH", "DELETE":
				actual[strings.ToUpper(method)+" "+path] = true
			}
		}
	}
	for _, route := range Routes() {
		path := documentedPath(route.Path)
		key := route.Method + " " + path
		if !actual[key] {
			t.Errorf("OpenAPI is missing %s", key)
		}
		delete(actual, key)
	}
	if len(actual) != 0 {
		t.Fatalf("OpenAPI has undocumented runtime routes: %v", actual)
	}
	responses := document.Components["responses"]
	if responses["Problem"] == nil || responses["CommandAccepted"] == nil {
		t.Fatal("OpenAPI is missing problem or command response contracts")
	}
}

func TestOpenAPICommandAndSafetyContractsMatchRuntime(t *testing.T) {
	data, err := os.ReadFile("../../docs/api-v1-openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document openAPIContract
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Security) != 0 {
		t.Fatal("Local Owner OpenAPI must not declare browser authentication")
	}

	commands := map[string]string{
		"/api/v1/incidents/{id}/investigations":    "#/components/schemas/VersionedCommand",
		"/api/v1/incidents/{id}/decision":          "#/components/schemas/RecoveryDecisionCommand",
		"/api/v1/incidents/{id}/close":             "#/components/schemas/VersionedCommand",
		"/api/v1/remediation-plans/{id}/decisions": "#/components/schemas/DecisionCommand",
	}
	for path, bodySchema := range commands {
		operation := document.Paths[path].Post
		if operation == nil {
			t.Fatalf("command operation missing: %s", path)
		}
		if !strings.Contains(strings.ToLower(operation.Description), "owner") {
			t.Errorf("command does not document Owner authority: %s", path)
		}
		parameterRefs := make(map[string]bool)
		for _, parameter := range operation.Parameters {
			parameterRefs[parameter.Ref] = true
		}
		for _, required := range []string{"#/components/parameters/IdempotencyKey", "#/components/parameters/Origin"} {
			if !parameterRefs[required] {
				t.Errorf("%s missing %s", path, required)
			}
		}
		if got := operation.RequestBody.Content[JSONMediaType].Schema.Ref; got != bodySchema {
			t.Errorf("%s body schema=%q want=%q", path, got, bodySchema)
		}
		for _, status := range []string{"202", "400", "403", "409", "413", "422", "501", "default"} {
			if operation.Responses[status] == nil {
				t.Errorf("%s missing response %s", path, status)
			}
		}
	}

	for _, route := range Routes() {
		if route.Method != "GET" {
			continue
		}
		path := documentedPath(route.Path)
		operation := document.Paths[path].Get
		if operation == nil || operation.Responses["501"] == nil {
			t.Errorf("unwired Query response is not documented for %s", path)
		}
	}

	for name, want := range map[string]string{
		"IdempotencyKey": IdempotencyHeader,
		"Origin":         "Origin",
	} {
		parameter := document.Components.Parameters[name]
		if parameter.Name != want || parameter.In != "header" || !parameter.Required {
			t.Errorf("parameter %s=%+v", name, parameter)
		}
	}
	problem := document.Components.Responses["Problem"]
	if problem.Content[ProblemMediaType] == nil || problem.Headers[RequestIDHeader] == nil || problem.Headers[TraceIDHeader] == nil || problem.Headers[ReplayHeader] == nil {
		t.Fatal("problem response is missing media type or request/trace/replay headers")
	}

	for schemaName, schema := range document.Components.Schemas {
		for _, forbidden := range []string{"numeric_id", "internal_id", "lease_owner", "lease_expires_at", "checkpoint", "prompt", "raw_result", "secret"} {
			if schema.Properties[forbidden] != nil {
				t.Errorf("schema %s exposes forbidden property %s", schemaName, forbidden)
			}
		}
	}
	proposal := document.Paths["/api/v1/operation-plans"].Post
	if proposal == nil {
		t.Fatal("Scenario Operation Plan proposal endpoint is missing")
	}
	if got := proposal.RequestBody.Content[JSONMediaType].Schema.Ref; got != "#/components/schemas/ScenarioScaleOperationPlanProposal" {
		t.Fatalf("Operation Plan proposal schema=%q", got)
	}
	for schemaName, fields := range map[string][]string{
		"AgentRun":           {"scenario_id"},
		"OperationTarget":    {"scenario_id"},
		"KubernetesResource": {"resource_version", "generation", "workload"},
	} {
		schema := document.Components.Schemas[schemaName]
		for _, field := range fields {
			if schema.Properties[field] == nil {
				t.Errorf("schema %s is missing Scenario contract field %s", schemaName, field)
			}
		}
	}
	assertSchemaMatchesJSONFields(t, document.Components.Schemas["Incident"], reflect.TypeOf(IncidentView{}))
	assertSchemaMatchesJSONFields(t, document.Components.Schemas["Resource"], reflect.TypeOf(ResourceView{}))
}

func documentedPath(path string) string {
	parts := strings.Split(path, "/")
	for index, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[index] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}

type openAPIContract struct {
	Security   []map[string][]string  `yaml:"security"`
	Paths      map[string]openAPIPath `yaml:"paths"`
	Components openAPIComponents      `yaml:"components"`
}

type openAPIPath struct {
	Parameters []openAPIReference `yaml:"parameters"`
	Get        *openAPIOperation  `yaml:"get"`
	Post       *openAPIOperation  `yaml:"post"`
}

type openAPIOperation struct {
	Description string                    `yaml:"description"`
	Parameters  []openAPIReference        `yaml:"parameters"`
	RequestBody openAPIRequestBody        `yaml:"requestBody"`
	Responses   map[string]map[string]any `yaml:"responses"`
}

type openAPIRequestBody struct {
	Content map[string]openAPIMedia `yaml:"content"`
}

type openAPIMedia struct {
	Schema openAPIReference `yaml:"schema"`
}

type openAPIReference struct {
	Ref string `yaml:"$ref"`
}

type openAPIComponents struct {
	Parameters map[string]openAPIParameter `yaml:"parameters"`
	Schemas    map[string]openAPISchema    `yaml:"schemas"`
	Responses  map[string]openAPIResponse  `yaml:"responses"`
}

type openAPIParameter struct {
	Name     string `yaml:"name"`
	In       string `yaml:"in"`
	Required bool   `yaml:"required"`
}

type openAPISchema struct {
	Properties map[string]any `yaml:"properties"`
}

type openAPIResponse struct {
	Headers map[string]any `yaml:"headers"`
	Content map[string]any `yaml:"content"`
}

func assertSchemaMatchesJSONFields(t *testing.T, schema openAPISchema, value reflect.Type) {
	t.Helper()
	for index := 0; index < value.NumField(); index++ {
		name := strings.Split(value.Field(index).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		if schema.Properties[name] == nil {
			t.Errorf("schema missing runtime JSON field %s.%s", value.Name(), name)
		}
	}
}
