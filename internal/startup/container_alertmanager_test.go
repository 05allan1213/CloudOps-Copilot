package startup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/config"
)

func TestV3AlertmanagerIngressRequiredBearerFailsClosedAtStartup(t *testing.T) {
	cfg := config.Load()
	cfg.AlertmanagerWebhookRequireBearer = true
	cfg.AlertmanagerWebhookBearerTokenFile = ""
	if _, err := initAlertmanagerIngress(&cfg, new(sql.DB), runtimeReady); err == nil || !strings.Contains(err.Error(), "bearer is required") {
		t.Fatalf("required empty bearer startup err=%v", err)
	}

	path := filepath.Join(t.TempDir(), "alertmanager-token")
	if err := os.WriteFile(path, []byte("0123456789abcdef0123456789abcdef\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.AlertmanagerWebhookBearerTokenFile = path
	if handler, err := initAlertmanagerIngress(&cfg, new(sql.DB), runtimeReady); err != nil || handler == nil {
		t.Fatalf("required file-backed bearer handler=%v err=%v", handler, err)
	}
}

func TestV3AlertmanagerIngressInvalidTargetAllowlistFailsStartup(t *testing.T) {
	cfg := config.Load()
	cfg.SignalTargetAllowlistJSON = `[{"cluster_id":"unknown"}]`
	if _, err := initAlertmanagerIngress(&cfg, new(sql.DB), runtimeReady); err == nil || !strings.Contains(err.Error(), "target allowlist") {
		t.Fatalf("invalid target allowlist startup err=%v", err)
	}
}

func runtimeReady(context.Context) error { return nil }
