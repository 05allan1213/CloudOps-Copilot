package alertmanagergateway

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
)

type fakeProvider struct {
	createID  string
	createErr error
	expireErr error
}

func (p *fakeProvider) CreateSilence(context.Context, alertdomain.SilenceProviderRequest) (string, error) {
	return p.createID, p.createErr
}

func (p *fakeProvider) ExpireSilence(context.Context, string, string) error { return p.expireErr }

func TestClientMapsWorkerProviderErrors(t *testing.T) {
	tests := []struct {
		name        string
		providerErr error
		want        error
	}{
		{"disabled", alertdomain.ErrProviderDisabled, alertdomain.ErrProviderDisabled},
		{"invalid", alertdomain.ErrInvalid, alertdomain.ErrInvalid},
		{"unavailable", alertdomain.ErrProviderUnavailable, alertdomain.ErrProviderUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			serverHandler, err := NewServer(&fakeProvider{createErr: test.providerErr})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(serverHandler)
			defer server.Close()
			client, err := NewClient(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.CreateSilence(context.Background(), alertdomain.SilenceProviderRequest{})
			if !errors.Is(err, test.want) {
				t.Fatalf("CreateSilence() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestClientRoundTripsCreateAndExpire(t *testing.T) {
	provider := &fakeProvider{createID: "provider-id"}
	serverHandler, _ := NewServer(provider)
	server := httptest.NewServer(serverHandler)
	defer server.Close()
	client, _ := NewClient(server.URL, time.Second)

	providerID, err := client.CreateSilence(context.Background(), alertdomain.SilenceProviderRequest{})
	if err != nil || providerID != "provider-id" {
		t.Fatalf("CreateSilence() = %q, %v", providerID, err)
	}
	if err := client.ExpireSilence(context.Background(), "provider-id", "revision-id"); err != nil {
		t.Fatalf("ExpireSilence() = %v", err)
	}
}
