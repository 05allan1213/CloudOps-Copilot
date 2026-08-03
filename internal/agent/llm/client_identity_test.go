package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatHashesProviderRequestIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "provider-secret-request-id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"body-id","choices":[{"message":{"role":"assistant","content":"{}"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`))
	}))
	defer server.Close()

	client := NewClient(Options{APIKey: "fixture", APIURL: server.URL, Model: "model-v1"})
	_, usage, err := client.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "fixture"}})
	if err != nil {
		t.Fatal(err)
	}
	want := providerRequestIDHash(http.Header{"X-Request-Id": []string{"provider-secret-request-id"}}, "body-id")
	if usage == nil || usage.RequestIDHash != want || len(usage.RequestIDHash) != 64 || usage.RequestIDHash == "provider-secret-request-id" {
		t.Fatalf("request identity usage=%+v want_hash=%q", usage, want)
	}
}

func TestChatStreamRejectsReasoningOnlyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"body-id\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"hidden reasoning\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1871,\"completion_tokens\":800,\"total_tokens\":2671}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Options{APIKey: "fixture", APIURL: server.URL, Model: "model-v1"})
	var emitted strings.Builder
	content, usage, err := client.ChatStream(context.Background(), []ChatMessage{{Role: "user", Content: "fixture"}}, func(delta string) error {
		emitted.WriteString(delta)
		return nil
	})

	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("ChatStream() error=%v want ErrInvalidResponse", err)
	}
	if content != "" || emitted.Len() != 0 {
		t.Fatalf("reasoning-only response leaked as final content=%q emitted=%q", content, emitted.String())
	}
	if usage == nil || usage.PromptTokens != 1871 || usage.CompletionTokens != 800 {
		t.Fatalf("ChatStream() usage=%+v", usage)
	}
}

func TestChatStreamCanBoundReasoningEffort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ReasoningEffort != "low" {
			t.Fatalf("reasoning_effort=%q want low", request.ReasoningEffort)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"bounded answer\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(Options{APIKey: "fixture", APIURL: server.URL, Model: "model-v1", ReasoningEffort: "low"})
	content, _, err := client.ChatStream(context.Background(), []ChatMessage{{Role: "user", Content: "fixture"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if content != "bounded answer" {
		t.Fatalf("content=%q", content)
	}
}
