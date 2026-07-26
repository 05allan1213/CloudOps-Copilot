package bootstrap

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	apibootstrap "github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/api"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
)

func TestRuntimeStartsWithDMLOnlyMySQLAndDoesNotMutateSchema(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_RUNTIME_MYSQL_DSN is not set; requires a migrated disposable MySQL database and DML-only user")
	}
	verifier, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = verifier.Close() }()
	before := runtimeTableCount(t, verifier)
	exerciseRuntime(t, runtimeApplication(t, dsn), verifier, http.StatusOK)
	after := runtimeTableCount(t, verifier)
	if before != after {
		t.Fatalf("runtime startup changed table count: before=%d after=%d", before, after)
	}
}

func TestRuntimeDoesNotAutoMigrateUnmigratedDatabase(t *testing.T) {
	dsn := os.Getenv("CLOUDOPS_TEST_UNMIGRATED_MYSQL_DSN")
	if dsn == "" {
		t.Skip("CLOUDOPS_TEST_UNMIGRATED_MYSQL_DSN is not set; requires an empty disposable MySQL database and DML-only user")
	}
	verifier, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = verifier.Close() }()
	before := runtimeTableCount(t, verifier)
	exerciseRuntime(t, runtimeApplication(t, dsn), verifier, http.StatusServiceUnavailable)
	after := runtimeTableCount(t, verifier)
	if before != after {
		t.Fatalf("unmigrated runtime startup changed table count: before=%d after=%d", before, after)
	}
}

func runtimeApplication(t *testing.T, dsn string) appconfig.Config {
	t.Helper()
	driverConfig, err := drivermysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	mysqlHost, mysqlPort, err := net.SplitHostPort(driverConfig.Addr)
	if err != nil {
		t.Fatalf("parse MySQL address %q: %v", driverConfig.Addr, err)
	}

	application := appconfig.Load()
	application.MySQLHost = mysqlHost
	application.MySQLPort = mysqlPort
	application.MySQLUser = driverConfig.User
	application.MySQLPassword = driverConfig.Passwd
	application.MySQLDatabase = driverConfig.DBName
	application.ListenAddr = "127.0.0.1:18080"
	application.StaticDir = ""
	application.RedisAddr = ""
	application.KafkaBrokers = nil
	application.K8SEnabled = false
	application.IncidentAgentEnabled = false
	application.ChangeIntelligenceEnabled = false
	application.RemediationEnabled = false
	application.DeliveryTrackingEnabled = false
	application.VerificationEnabled = false
	application.FastDemoEnabled = false
	return application
}

func exerciseRuntime(t *testing.T, application appconfig.Config, verifier *sql.DB, expectedReadiness int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	api, err := apibootstrap.NewAPI(ctx, apibootstrap.APIConfig{Application: application})
	if err != nil {
		t.Fatalf("start API with DML-only user: %v", err)
	}
	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	internalListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiDone := make(chan error, 1)
	go func() { apiDone <- api.Serve(ctx, apiListener, internalListener) }()
	waitForHTTPStatus(t, "http://"+apiListener.Addr().String()+"/readyz", expectedReadiness)
	waitForHTTPStatus(t, "http://"+internalListener.Addr().String()+"/readyz", expectedReadiness)
	if status := runtimeHTTPStatus(t, http.MethodGet, "http://"+internalListener.Addr().String()+"/api/v1/incidents", nil, ""); status != http.StatusNotFound {
		t.Fatalf("INTERNAL listener exposed user API: status=%d", status)
	}
	if status := runtimeHTTPStatus(t, http.MethodPost, "http://"+apiListener.Addr().String()+"/webhooks/alertmanager", bytes.NewBufferString(`{}`), "application/json"); status != http.StatusNotFound {
		t.Fatalf("user listener exposed internal webhook: status=%d", status)
	}
	if expectedReadiness == http.StatusOK {
		fingerprint := fmt.Sprintf("a%015x", time.Now().UnixNano())
		body := fmt.Sprintf(`{"version":"4","groupKey":"{}:{alertname=\"CloudOpsDemoDeploymentUnavailable\"}","truncatedAlerts":0,"status":"firing","receiver":"cloudops-demo","groupLabels":{},"commonLabels":{},"commonAnnotations":{},"externalURL":"http://alertmanager:9093","alerts":[{"status":"firing","labels":{"alertname":"CloudOpsDemoDeploymentUnavailable","severity":"critical","cluster":"kind-cloudops-local","environment":"local-demo","namespace":"demo","service":"demo","deployment":"demo"},"annotations":{"summary":"DML-only ingress proof"},"startsAt":"2026-07-18T12:00:00.123456Z","endsAt":"0001-01-01T00:00:00Z","generatorURL":"http://prometheus:9090/graph","fingerprint":"%s"}]}`, fingerprint)
		status := runtimeHTTPStatus(t, http.MethodPost, "http://"+internalListener.Addr().String()+"/webhooks/alertmanager", bytes.NewBufferString(body), "application/json")
		if status != http.StatusAccepted {
			t.Fatalf("Alertmanager ingress status=%d", status)
		}
		var signalCount int
		if err := verifier.QueryRow(`SELECT COUNT(*) FROM incident_signals WHERE source = 'alertmanager' AND fingerprint = ? AND canonical_schema_version = 2 AND correlation_key_version = 2`, fingerprint).Scan(&signalCount); err != nil {
			t.Fatal(err)
		}
		if signalCount != 1 {
			t.Fatalf("durable Alertmanager signal count=%d, want 1", signalCount)
		}
	}
	cancel()
	if err := <-apiDone; err != nil {
		t.Fatal(err)
	}

	workerContext, stopWorker := context.WithCancel(context.Background())
	workerConfig := WorkerConfig{
		Application:       application,
		TaskOperations:    testRuntimeTaskOperations(),
		ManagementAddr:    "127.0.0.1:18081",
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
		WriteTimeout:      time.Second,
		IdleTimeout:       time.Second,
	}
	worker, err := NewWorker(workerContext, workerConfig)
	if err != nil {
		t.Fatalf("start Worker with DML-only user: %v", err)
	}
	workerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	workerDone := make(chan error, 1)
	go func() { workerDone <- worker.Serve(workerContext, workerListener) }()
	waitForHTTPStatus(t, "http://"+workerListener.Addr().String()+"/readyz", expectedReadiness)
	stopWorker()
	if err := <-workerDone; err != nil {
		t.Fatal(err)
	}

}

func runtimeHTTPStatus(t *testing.T, method, url string, body io.Reader, contentType string) int {
	t.Helper()
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

func testRuntimeTaskOperations() taskhandler.Config {
	operation := func(context.Context, asyncjob.Execution) asyncjob.Result { return asyncjob.Succeeded(nil) }
	return taskhandler.Config{
		AgentRunIdentity: agent.RunModelIdentity{
			Provider: "fixture", ActualModel: "fixture-model", PromptVersion: "prompt/v1",
			PromptHash: strings.Repeat("a", 64), ToolSchemaVersion: "tools/v1", ToolSchemaHash: strings.Repeat("b", 64),
		},
		InvestigationStep: operation, RemediationPrepare: operation, ChangeEnsurePR: operation,
		DeliveryObserve: operation, VerificationAdvance: operation,
	}
}

func runtimeTableCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
