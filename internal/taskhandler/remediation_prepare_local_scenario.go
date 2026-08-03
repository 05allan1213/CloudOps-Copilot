package taskhandler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	"github.com/05allan1213/CloudOps-Copilot/internal/asyncjob"
	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/remediation"
	"github.com/google/uuid"
)

type localScenarioRemediationLoader struct {
	db         *sql.DB
	kubernetes infrastructure.Reader
	planTTL    time.Duration
	now        func() time.Time
}

func NewLocalScenarioRemediationPrepareLoader(db *sql.DB, kubernetes infrastructure.Reader, planTTL time.Duration) (RemediationPrepareLoader, error) {
	if db == nil || kubernetes == nil {
		return nil, errors.New("local Scenario remediation requires MySQL and a Kubernetes reader")
	}
	if planTTL == 0 {
		planTTL = defaultRemediationPlanTTL
	}
	if planTTL <= 0 || planTTL > 24*time.Hour {
		return nil, fmt.Errorf("%w: local Scenario remediation Plan TTL is outside bounds", remediation.ErrInvalidArgument)
	}
	return &localScenarioRemediationLoader{db: db, kubernetes: kubernetes, planTTL: planTTL, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (l *localScenarioRemediationLoader) Load(ctx context.Context, task asyncjob.Task) (RemediationPrepareInput, error) {
	if l == nil || l.db == nil || l.kubernetes == nil || task.SubjectID == 0 || task.IncidentID == 0 ||
		task.CycleNo == 0 || task.ExpectedSubjectVersion == 0 || task.CreatedAt.IsZero() || !validSHA256Text(task.DedupeKey) {
		return RemediationPrepareInput{}, fmt.Errorf("%w: local Scenario remediation task fence is incomplete", remediation.ErrInvalidArgument)
	}
	durableReader := &mysqlRemediationPrepareLoader{db: l.db}
	durable, err := durableReader.loadDurableFactsForPolicy(ctx, task, agent.LocalScenarioRequiredEnvClaimPolicy(), remediationPrepareDurableTarget{
		Cluster: "cloudops-local", Environment: "local", Namespace: "demo",
		Kind: "Deployment", Name: "cloudops-scenario-fault",
	}, false)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	scenarioID, err := loadLocalScenarioIdentity(ctx, l.db, task)
	if err != nil {
		return RemediationPrepareInput{}, err
	}
	projection, err := l.kubernetes.Read(ctx, infrastructure.ReadRequest{ClusterID: "cloudops-local", Namespaces: []string{"demo"}, Limit: infrastructure.DefaultLimit})
	if err != nil {
		return RemediationPrepareInput{}, fmt.Errorf("read current local Scenario Deployment: %w", err)
	}
	resource, container, ok := exactLocalScenarioDeployment(projection, scenarioID)
	if !ok {
		return RemediationPrepareInput{}, fmt.Errorf("%w: current local Scenario Deployment is absent, ambiguous, or unsafe", asyncjob.ErrPolicyViolation)
	}
	createdAt := task.CreatedAt.UTC().Truncate(time.Microsecond)
	expiresAt := createdAt.Add(l.planTTL)
	if createdAt.Before(durable.RunCompletedAt) || !l.now().UTC().Before(expiresAt) {
		return RemediationPrepareInput{}, fmt.Errorf("%w: local Scenario remediation task is stale", asyncjob.ErrPolicyViolation)
	}
	return RemediationPrepareInput{
		AgentRunID: task.SubjectID, PlanPublicID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("remediation-plan\x00"+task.DedupeKey)).String(),
		SourceType: remediation.PlanSourceLocalScenario,
		LocalRequest: remediation.LocalScenarioCompileRequest{
			IncidentPublicID: durable.IncidentPublicID, IncidentID: task.IncidentID, CycleNo: uint64(task.CycleNo),
			IncidentVersion: durable.IncidentVersion, CreatedByAgentRunID: durable.AgentRunPublicID,
			DiagnosisHash: durable.Diagnosis.DiagnosisHash, ScenarioID: scenarioID, ClusterID: durable.Cluster,
			Environment: durable.Environment, ResourceUID: resource.SourceUID, ResourceVersion: resource.ResourceVersion,
			Generation: resource.Generation, Target: remediation.TargetResource{APIVersion: "apps/v1", Kind: "Deployment", Namespace: "demo", Name: "cloudops-scenario-fault", Container: "scenario"},
			EnvNames: append([]string(nil), container.EnvNames...), Evidence: append([]remediation.EvidenceBinding(nil), durable.Evidence...),
			CreatedAt: createdAt, ExpiresAt: expiresAt, PlanVersion: durable.PlanVersion,
		},
		MigratedLegacy: durable.MigratedLegacy, MigratedLegacyContext: durable.MigratedLegacyContext,
	}, nil
}

func loadLocalScenarioIdentity(ctx context.Context, db *sql.DB, task asyncjob.Task) (string, error) {
	var scenarioID string
	err := db.QueryRowContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(snapshot.filters_json,'$.scenario_id'))
FROM agent_runs run JOIN context_snapshots snapshot ON snapshot.id=run.context_snapshot_id
WHERE run.id=? AND run.incident_id=? AND run.cycle_no=?`, task.SubjectID, task.IncidentID, task.CycleNo).Scan(&scenarioID)
	if err != nil || !validLocalScenarioIdentity(scenarioID) {
		return "", fmt.Errorf("%w: task has no exact local Scenario identity", asyncjob.ErrPolicyViolation)
	}
	var linked string
	err = db.QueryRowContext(ctx, `SELECT COALESCE(MIN(JSON_UNQUOTE(JSON_EXTRACT(alert.labels_json,'$.scenario_id'))),'')
FROM alert_incident_links relation JOIN alerts alert ON alert.id=relation.alert_id
WHERE relation.incident_id=? AND relation.incident_cycle_no=?
HAVING COUNT(DISTINCT JSON_UNQUOTE(JSON_EXTRACT(alert.labels_json,'$.scenario_id'))) = 1`, task.IncidentID, task.CycleNo).Scan(&linked)
	if err != nil || linked != scenarioID {
		return "", fmt.Errorf("%w: Incident Scenario identity is absent or ambiguous", asyncjob.ErrPolicyViolation)
	}
	return scenarioID, nil
}

func exactLocalScenarioDeployment(projection infrastructure.Projection, scenarioID string) (infrastructure.Resource, infrastructure.ContainerEnvironment, bool) {
	var matched infrastructure.Resource
	count := 0
	for _, resource := range projection.Nodes {
		if resource.APIVersion == "apps/v1" && resource.Kind == "Deployment" && resource.Namespace == "demo" &&
			resource.Name == "cloudops-scenario-fault" && resource.Labels["cloudops.io/scenario-id"] == scenarioID {
			matched, count = resource, count+1
		}
	}
	if count != 1 || projection.Partial || projection.Truncated || len(projection.Issues) > 0 || matched.ContainersTruncated ||
		matched.SourceUID == "" || matched.ResourceVersion == "" || matched.Generation <= 0 {
		return infrastructure.Resource{}, infrastructure.ContainerEnvironment{}, false
	}
	var container infrastructure.ContainerEnvironment
	containerCount := 0
	for _, item := range matched.Containers {
		if item.Name == "scenario" {
			container, containerCount = item, containerCount+1
		}
	}
	if containerCount != 1 || container.EnvNamesTruncated || container.HasEnvFrom || container.HasSecretReference {
		return infrastructure.Resource{}, infrastructure.ContainerEnvironment{}, false
	}
	for _, name := range container.EnvNames {
		if name == "REQUIRED_ENV" {
			return infrastructure.Resource{}, infrastructure.ContainerEnvironment{}, false
		}
	}
	return matched, container, true
}

func validLocalScenarioIdentity(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) <= len("scenario-") || len(value) > 63 || !strings.HasPrefix(value, "scenario-") {
		return false
	}
	for index := len("scenario-"); index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return !strings.HasSuffix(value, "-")
}
