package startup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	"github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/incidentstore"
)

func TestV3AlertmanagerIngressRequiredBearerFailsClosedAtStartup(t *testing.T) {
	cfg := config.Load()
	cfg.AlertmanagerWebhookRequireBearer = true
	cfg.AlertmanagerWebhookBearerTokenFile = ""
	if _, err := initAlertmanagerIngress(&cfg, startupAlertStore{}, runtimeReady); err == nil || !strings.Contains(err.Error(), "bearer is required") {
		t.Fatalf("required empty bearer startup err=%v", err)
	}

	path := filepath.Join(t.TempDir(), "alertmanager-token")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.AlertmanagerWebhookBearerTokenFile = path
	if handler, err := initAlertmanagerIngress(&cfg, startupAlertStore{}, runtimeReady); err != nil || handler == nil {
		t.Fatalf("required file-backed bearer handler=%v err=%v", handler, err)
	}
}

func TestV3AlertmanagerIngressInvalidTargetAllowlistFailsStartup(t *testing.T) {
	cfg := config.Load()
	cfg.SignalTargetAllowlistJSON = `[{"cluster_id":"unknown"}]`
	if _, err := initAlertmanagerIngress(&cfg, startupAlertStore{}, runtimeReady); err == nil || !strings.Contains(err.Error(), "target allowlist") {
		t.Fatalf("invalid target allowlist startup err=%v", err)
	}
}

func runtimeReady(context.Context) error { return nil }

type startupAlertStore struct{}

func (startupAlertStore) Ready(context.Context) error { return nil }
func (startupAlertStore) IngestBatch(context.Context, []alertdomain.SignalInput) ([]alertdomain.IngestResult, error) {
	return nil, nil
}
func (startupAlertStore) RecordRejections(context.Context, []incidentstore.RejectionInput) error {
	return nil
}
