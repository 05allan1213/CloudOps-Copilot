package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
)

func TestStructuredModelRepairsAtMostOnceAndAccumulatesUsage(t *testing.T) {
	server, requests := structuredModelServer(t, []string{`{"ok":`, `{"ok":true}`})
	defer server.Close()
	model := newStructuredTestModel(t, server.URL)
	schema := []byte(`{"type":"object","additionalProperties":false,"required":["ok"],"properties":{"ok":{"type":"boolean"}}}`)
	var accepted struct {
		OK bool `json:"ok"`
	}
	output, usage, err := model.Invoke(context.Background(), "Return a bounded test object.", []byte(`{"case":"repair"}`), schema, func(raw []byte) error {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		var value struct {
			OK bool `json:"ok"`
		}
		if err := decoder.Decode(&value); err != nil {
			return err
		}
		if !value.OK {
			return errors.New("ok must be true")
		}
		accepted = value
		return nil
	})
	if err != nil || !accepted.OK || string(output) != `{"ok":true}` {
		t.Fatalf("output=%s accepted=%v err=%v", output, accepted.OK, err)
	}
	if usage.InputTokens != 6 || usage.OutputTokens != 10 {
		t.Fatalf("usage=%+v, want summed 6/10", usage)
	}
	if got := requests(); len(got) != 2 || !strings.Contains(got[1], "Repair the previous response exactly once") {
		t.Fatalf("requests=%d repair=%q", len(got), strings.Join(got, "\n"))
	}
}

func TestStructuredModelRejectsSecondInvalidOutputWithoutThirdCall(t *testing.T) {
	server, requests := structuredModelServer(t, []string{`{"ok":false}`, `{"ok":false}`})
	defer server.Close()
	model := newStructuredTestModel(t, server.URL)
	_, _, err := model.Invoke(context.Background(), "Return a bounded test object.", []byte(`{"case":"invalid"}`), []byte(`{"type":"object"}`), func([]byte) error {
		return errors.New("forced invalid output")
	})
	if !errors.Is(err, ErrStructuredOutput) {
		t.Fatalf("error=%v, want ErrStructuredOutput", err)
	}
	if got := len(requests()); got != 2 {
		t.Fatalf("provider calls=%d, want exactly 2", got)
	}
}

func TestJSONSchemaForIsStrictAndBounded(t *testing.T) {
	schema, err := JSONSchemaFor[agent.DiagnosisCandidate]()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(schema) || !bytes.Contains(schema, []byte(`"additionalProperties":false`)) ||
		!bytes.Contains(schema, []byte(`"evidence_fact_ids"`)) {
		t.Fatalf("unexpected schema: %s", schema)
	}
	deltaSchema, err := JSONSchemaFor[agent.StateDelta]()
	if err != nil || !bytes.Contains(deltaSchema, []byte(`"bounded_parameters"`)) ||
		!bytes.Contains(deltaSchema, []byte(`"description":"Allowlisted JSON object; keys are validated by the Go reducer."`)) {
		t.Fatalf("bounded parameter schema is not an object: %s", deltaSchema)
	}
}

func newStructuredTestModel(t *testing.T, endpoint string) *StructuredModel {
	t.Helper()
	zeroRetries := 0
	client := llm.NewClient(llm.Options{
		APIKey: "test-key", APIURL: endpoint, Model: "test-model", Timeout: time.Second,
		MaxRetries: &zeroRetries, HTTPClient: &http.Client{Timeout: time.Second},
	})
	model, err := NewStructuredModel(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func structuredModelServer(t *testing.T, outputs []string) (*httptest.Server, func() []string) {
	t.Helper()
	var mutex sync.Mutex
	requests := make([]string, 0, len(outputs))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		index := len(requests)
		content := ""
		for _, message := range request.Messages {
			content += message.Content + "\n"
		}
		requests = append(requests, content)
		mutex.Unlock()
		if index >= len(outputs) {
			t.Errorf("unexpected provider call %d", index+1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": outputs[index]}}},
			"usage":   map[string]any{"prompt_tokens": 3, "completion_tokens": 5, "total_tokens": 8},
		})
	}))
	return server, func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), requests...)
	}
}
