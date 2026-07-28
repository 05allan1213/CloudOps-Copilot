package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
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
