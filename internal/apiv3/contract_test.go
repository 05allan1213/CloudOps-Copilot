package apiv3

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const contractIncidentID = "123e4567-e89b-12d3-a456-426614174000"

func TestPhase2RoutesCompleteAndV2IsUntouched(t *testing.T) {
	engine := newContractEngine(NewHandler(Config{}))
	registered := make(map[string]bool)
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, expected := range Phase2Routes() {
		if !registered[expected.Method+" "+expected.Path] {
			t.Fatalf("missing V3 route %s %s", expected.Method, expected.Path)
		}
	}
	for route := range registered {
		if strings.Contains(route, "/api/v2/") {
			t.Fatalf("V3 registration changed V2 route surface: %s", route)
		}
	}
	if len(registered) != len(Phase2Routes()) {
		t.Fatalf("registered route count=%d want=%d", len(registered), len(Phase2Routes()))
	}
}

func TestQueryContractUsesProblemJSONAndPublicUUIDOnly(t *testing.T) {
	projection := NewMemoryQueryPort()
	engine := newContractEngine(NewHandler(Config{Queries: projection}))

	invalid := httptest.NewRecorder()
	engine.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/api/v3/incidents/42", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("numeric id status=%d want 400", invalid.Code)
	}
	invalidProblem := assertProblem(t, invalid, "INVALID_PUBLIC_ID")
	if strings.Contains(invalidProblem.Instance, "/42") {
		t.Fatalf("numeric id leaked in problem instance: %+v", invalidProblem)
	}

	missing := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v3/incidents/"+contractIncidentID, nil)
	request.Header.Set(RequestIDHeader, "contract-request-1")
	engine.ServeHTTP(missing, request)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing id status=%d want 404", missing.Code)
	}
	problem := assertProblem(t, missing, "RESOURCE_NOT_FOUND")
	if problem.RequestID != "contract-request-1" || missing.Header().Get(RequestIDHeader) != "contract-request-1" {
		t.Fatalf("request id contract not preserved: body=%+v header=%q", problem, missing.Header().Get(RequestIDHeader))
	}
	if missing.Header().Get(TraceIDHeader) == "" || problem.TraceID == "" {
		t.Fatal("trace id contract missing")
	}

	list := httptest.NewRecorder()
	engine.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil))
	if list.Code != http.StatusOK || list.Header().Get("Content-Type") != JSONMediaType {
		t.Fatalf("list response status/content type=%d/%q", list.Code, list.Header().Get("Content-Type"))
	}
	if strings.Contains(list.Body.String(), `"numeric_id"`) || strings.Contains(list.Body.String(), `"lease"`) {
		t.Fatalf("internal fields leaked in list response: %s", list.Body.String())
	}
}

func TestUnwiredQueryPortFailsClosedInsteadOfReturningFakeEmptyState(t *testing.T) {
	engine := newContractEngine(NewHandler(Config{}))
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil))
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("unwired query status=%d want 501", response.Code)
	}
	assertProblem(t, response, "NOT_IMPLEMENTED")
}

func TestQueryPortIsReadOnlyAndSSEIsIncidentScoped(t *testing.T) {
	projection := NewMemoryQueryPort()
	if err := projection.PutIncident(IncidentView{ID: contractIncidentID, Status: "detected", Severity: "warning"}); err != nil {
		t.Fatal(err)
	}
	childID := "223e4567-e89b-12d3-a456-426614174000"
	if err := projection.PutChildren(contractIncidentID, QueryEvidence, []ResourceView{{ID: childID, Kind: "evidence", Status: "valid"}}); err != nil {
		t.Fatal(err)
	}
	if err := projection.PutEvents(contractIncidentID, []RefreshEvent{{Cursor: "opaque-1", Resource: "timeline"}}); err != nil {
		t.Fatal(err)
	}
	engine := newContractEngine(NewHandler(Config{Queries: projection}))

	evidence := httptest.NewRecorder()
	engine.ServeHTTP(evidence, httptest.NewRequest(http.MethodGet, "/api/v3/incidents/"+contractIncidentID+"/evidence", nil))
	if evidence.Code != http.StatusOK || evidence.Header().Get("Content-Type") != JSONMediaType {
		t.Fatalf("evidence response=%d/%q", evidence.Code, evidence.Header().Get("Content-Type"))
	}
	if !strings.Contains(evidence.Body.String(), childID) {
		t.Fatalf("evidence public id missing: %s", evidence.Body.String())
	}

	events := httptest.NewRecorder()
	eventRequest := httptest.NewRequest(http.MethodGet, "/api/v3/incidents/"+contractIncidentID+"/events", nil)
	eventRequest.Header.Set("Last-Event-ID", "opaque-0")
	engine.ServeHTTP(events, eventRequest)
	if events.Code != http.StatusOK || events.Header().Get("Content-Type") != SSEMediaType {
		t.Fatalf("events response=%d/%q", events.Code, events.Header().Get("Content-Type"))
	}
	if !strings.Contains(events.Body.String(), "event: incident.refresh") || !strings.Contains(events.Body.String(), "incident_id") {
		t.Fatalf("invalid SSE refresh contract: %s", events.Body.String())
	}
	if strings.Contains(events.Body.String(), `"id":`) || strings.Contains(events.Body.String(), `"numeric`) {
		t.Fatalf("SSE exposed an internal id: %s", events.Body.String())
	}
}

func TestCommandHeadersExpectedVersionHashAndNotImplemented(t *testing.T) {
	engine := newContractEngine(NewHandler(Config{}))
	body := `{"expected_version":1}`

	missingKey := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	engine.ServeHTTP(missingKey, request)
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d want 400", missingKey.Code)
	}
	assertProblem(t, missingKey, "IDEMPOTENCY_KEY_REQUIRED")

	oversizedKey := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, strings.Repeat("k", 129))
	engine.ServeHTTP(oversizedKey, request)
	if oversizedKey.Code != http.StatusBadRequest {
		t.Fatalf("oversized idempotency key status=%d want 400", oversizedKey.Code)
	}
	assertProblem(t, oversizedKey, "IDEMPOTENCY_KEY_REQUIRED")

	notImplemented := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "contract-key-1")
	engine.ServeHTTP(notImplemented, request)
	if notImplemented.Code != http.StatusNotImplemented {
		t.Fatalf("skeleton status=%d want 501", notImplemented.Code)
	}
	assertProblem(t, notImplemented, "NOT_IMPLEMENTED")

	missingHash := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/remediation-plans/"+contractIncidentID+"/decisions", strings.NewReader(`{"decision":"approved","expected_version":1}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "contract-key-2")
	engine.ServeHTTP(missingHash, request)
	if missingHash.Code != http.StatusBadRequest {
		t.Fatalf("missing hash status=%d want 400", missingHash.Code)
	}
	assertProblem(t, missingHash, "EXPECTED_HASH_REQUIRED")

	invalidTransition := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/remediation-plans/"+contractIncidentID+"/decisions", strings.NewReader(`{"decision":"hold","expected_version":1,"expected_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "contract-key-3")
	engine.ServeHTTP(invalidTransition, request)
	if invalidTransition.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid decision status=%d want 422", invalidTransition.Code)
	}
	assertProblem(t, invalidTransition, "INVALID_TRANSITION")
}

func TestCommandIdempotencyConcurrentAndPayloadConflict(t *testing.T) {
	commands := NewMemoryCommandPort()
	engine := newContractEngine(NewHandler(Config{Commands: commands}))
	body := `{"expected_version":1,"reason":"retry"}`
	const workers = 16
	responses := make([]*httptest.ResponseRecorder, workers)
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := 0; index < workers; index++ {
		go func(index int) {
			defer wait.Done()
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(body))
			request.Header.Set("Content-Type", JSONMediaType)
			request.Header.Set(IdempotencyHeader, "same-key")
			engine.ServeHTTP(response, request)
			responses[index] = response
		}(index)
	}
	wait.Wait()
	for index, response := range responses {
		if response.Code != http.StatusAccepted {
			t.Fatalf("concurrent response %d status=%d body=%s", index, response.Code, response.Body.String())
		}
	}
	if got := len(commands.Requests()); got != 1 {
		t.Fatalf("command executions=%d want 1", got)
	}

	conflict := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(`{"expected_version":2}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "same-key")
	engine.ServeHTTP(conflict, request)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("payload conflict status=%d want 409", conflict.Code)
	}
	assertProblem(t, conflict, "IDEMPOTENCY_KEY_REUSED")

	replay := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/investigations", strings.NewReader(body))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "same-key")
	engine.ServeHTTP(replay, request)
	if replay.Code != http.StatusAccepted || replay.Header().Get(ReplayHeader) != "true" {
		t.Fatalf("replay status/header=%d/%q", replay.Code, replay.Header().Get(ReplayHeader))
	}
}

func TestAuthRoleCSRFAndRequestIdentity(t *testing.T) {
	auth := &contractAuthenticator{identity: Identity{Subject: "provider|operator", Provider: "provider", Login: "operator", Role: "operator"}}
	engine := newContractEngine(NewHandler(Config{
		Authenticator: auth, RequireAuth: true, RequireCSRF: true,
		CSRFSecret: []byte("0123456789abcdef0123456789abcdef"), AllowedOrigins: []string{"https://console.example"},
	}))

	unauthorized := httptest.NewRecorder()
	engine.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v3/incidents", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d want 401", unauthorized.Code)
	}
	assertProblem(t, unauthorized, "AUTHENTICATION_REQUIRED")

	auth.identity.Role = "viewer"
	viewerCommand := httptest.NewRecorder()
	viewerRequest := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	viewerRequest.Header.Set("Authorization", "Bearer test")
	viewerRequest.Header.Set("Content-Type", JSONMediaType)
	viewerRequest.Header.Set(IdempotencyHeader, "viewer-command")
	viewerRequest.Header.Set("Origin", "https://console.example")
	engine.ServeHTTP(viewerCommand, viewerRequest)
	if viewerCommand.Code != http.StatusForbidden {
		t.Fatalf("viewer command status=%d want 403", viewerCommand.Code)
	}
	assertProblem(t, viewerCommand, "ROLE_FORBIDDEN")
	auth.identity.Role = "operator"

	csrfRecorder := httptest.NewRecorder()
	csrfRequest := httptest.NewRequest(http.MethodGet, "/api/v3/session/csrf", nil)
	csrfRequest.Header.Set("Authorization", "Bearer test")
	engine.ServeHTTP(csrfRecorder, csrfRequest)
	if csrfRecorder.Code != http.StatusOK {
		t.Fatalf("csrf status=%d body=%s", csrfRecorder.Code, csrfRecorder.Body.String())
	}
	var csrf csrfResponse
	if err := json.Unmarshal(csrfRecorder.Body.Bytes(), &csrf); err != nil || csrf.Token == "" {
		t.Fatalf("csrf body=%s err=%v", csrfRecorder.Body.String(), err)
	}

	missingCSRF := httptest.NewRecorder()
	commandRequest := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	commandRequest.Header.Set("Authorization", "Bearer test")
	commandRequest.Header.Set("Content-Type", JSONMediaType)
	commandRequest.Header.Set(IdempotencyHeader, "csrf-key")
	commandRequest.Header.Set("Origin", "https://console.example")
	engine.ServeHTTP(missingCSRF, commandRequest)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing csrf status=%d want 403", missingCSRF.Code)
	}
	assertProblem(t, missingCSRF, "CSRF_REQUIRED")

	valid := httptest.NewRecorder()
	commandRequest = httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	commandRequest.Header.Set("Authorization", "Bearer test")
	commandRequest.Header.Set("Content-Type", JSONMediaType)
	commandRequest.Header.Set(IdempotencyHeader, "csrf-key")
	commandRequest.Header.Set("Origin", "https://console.example")
	commandRequest.Header.Set(CSRFHeader, csrf.Token)
	engine.ServeHTTP(valid, commandRequest)
	if valid.Code != http.StatusNotImplemented {
		t.Fatalf("valid command status=%d want 501 skeleton", valid.Code)
	}
	assertProblem(t, valid, "NOT_IMPLEMENTED")
}

func TestCSRFTokenRejectsForgedCrossIdentityExpiredAndForeignOrigin(t *testing.T) {
	now := time.Date(2026, 7, 18, 9, 0, 0, 0, time.UTC)
	auth := &contractAuthenticator{identity: Identity{Subject: "provider|alice", Provider: "github", Login: "alice", Role: "operator"}}
	handler := NewHandler(Config{
		Authenticator: auth, RequireAuth: true, RequireCSRF: true,
		CSRFSecret: []byte("0123456789abcdef0123456789abcdef"), AllowedOrigins: []string{"https://console.example"},
		Now: func() time.Time { return now },
	})
	engine := newContractEngine(handler)
	token := issueContractCSRF(t, engine)

	for name, testCase := range map[string]struct {
		mutate   func(string) string
		origin   string
		wantCode string
	}{
		"forged":         {mutate: func(token string) string { return token + "x" }, origin: "https://console.example", wantCode: "CSRF_INVALID"},
		"missing origin": {mutate: func(token string) string { return token }, origin: "", wantCode: "ORIGIN_FORBIDDEN"},
		"foreign origin": {mutate: func(token string) string { return token }, origin: "https://attacker.example", wantCode: "ORIGIN_FORBIDDEN"},
	} {
		t.Run(name, func(t *testing.T) {
			response := performAuthenticatedClose(engine, testCase.mutate(token), testCase.origin, "csrf-negative-"+strings.ReplaceAll(name, " ", "-"))
			if response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			assertProblem(t, response, testCase.wantCode)
		})
	}

	auth.identity = Identity{Subject: "provider|bob", Provider: "github", Login: "bob", Role: "operator"}
	crossIdentity := performAuthenticatedClose(engine, token, "https://console.example", "csrf-cross-identity")
	if crossIdentity.Code != http.StatusForbidden {
		t.Fatalf("cross identity status=%d", crossIdentity.Code)
	}
	assertProblem(t, crossIdentity, "CSRF_INVALID")

	auth.identity = Identity{Subject: "provider|alice", Provider: "github", Login: "alice", Role: "operator"}
	now = now.Add(16 * time.Minute)
	expired := performAuthenticatedClose(engine, token, "https://console.example", "csrf-expired")
	if expired.Code != http.StatusForbidden {
		t.Fatalf("expired status=%d", expired.Code)
	}
	assertProblem(t, expired, "CSRF_INVALID")
}

func TestStaleAndInvalidTransitionErrorsMapToStableProblems(t *testing.T) {
	commands := &errorCommandPort{err: ErrStaleVersion}
	engine := newContractEngine(NewHandler(Config{Commands: commands}))
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "stale-key")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale status=%d want 409", response.Code)
	}
	assertProblem(t, response, "STALE_EXPECTATION")

	commands.err = ErrInvalidTransition
	request = httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":2}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "transition-key")
	response = httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("transition status=%d want 422", response.Code)
	}
	assertProblem(t, response, "INVALID_TRANSITION")
}

func TestDurableReplayedCommandErrorSetsReplayHeader(t *testing.T) {
	commands := &errorCommandPort{
		result: CommandResult{Replayed: true},
		err:    ErrInvalidTransition,
	}
	engine := newContractEngine(NewHandler(Config{Commands: commands}))
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, "durable-error-replay")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || response.Header().Get(ReplayHeader) != "true" {
		t.Fatalf("durable error replay status/header=%d/%q body=%s", response.Code, response.Header().Get(ReplayHeader), response.Body.String())
	}
	assertProblem(t, response, "INVALID_TRANSITION")
}

type contractAuthenticator struct {
	identity Identity
}

func (a *contractAuthenticator) AuthenticateBearer(_ context.Context, header string) (Identity, error) {
	if header != "Bearer test" {
		return Identity{}, errors.New("invalid bearer")
	}
	return a.identity, nil
}

func (*contractAuthenticator) Verify(context.Context, Identity) error { return nil }

type errorCommandPort struct {
	mu     sync.Mutex
	result CommandResult
	err    error
}

func issueContractCSRF(t *testing.T, engine *gin.Engine) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v3/session/csrf", nil)
	request.Header.Set("Authorization", "Bearer test")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("csrf status=%d body=%s", response.Code, response.Body.String())
	}
	var result csrfResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Token == "" {
		t.Fatalf("csrf body=%s err=%v", response.Body.String(), err)
	}
	return result.Token
}

func performAuthenticatedClose(engine *gin.Engine, token, origin, key string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v3/incidents/"+contractIncidentID+"/close", strings.NewReader(`{"expected_version":1}`))
	request.Header.Set("Authorization", "Bearer test")
	request.Header.Set("Content-Type", JSONMediaType)
	request.Header.Set(IdempotencyHeader, key)
	request.Header.Set("Origin", origin)
	request.Header.Set(CSRFHeader, token)
	engine.ServeHTTP(response, request)
	return response
}

func (p *errorCommandPort) Execute(context.Context, CommandRequest) (CommandResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.result, p.err
}

func newContractEngine(handler *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterRoutes(engine.Group("/api/v3"), handler)
	return engine
}

func assertProblem(t *testing.T, response *httptest.ResponseRecorder, code string) Problem {
	t.Helper()
	if response.Header().Get("Content-Type") != ProblemMediaType {
		t.Fatalf("problem content type=%q want %q", response.Header().Get("Content-Type"), ProblemMediaType)
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("problem JSON=%s err=%v", response.Body.String(), err)
	}
	if problem.Code != code || problem.Status != response.Code || problem.RequestID == "" || problem.TraceID == "" {
		t.Fatalf("problem=%+v want code=%s status=%d with ids", problem, code, response.Code)
	}
	return problem
}
