// Package recoveryverificationread provides the bounded observation source for
// operational Incident recovery. It reads only current-cycle Alert relations
// from MySQL and the current workload from the Worker-owned Kubernetes gateway.
package recoveryverificationread

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/infrastructure"
	"github.com/05allan1213/CloudOps-Copilot/internal/taskhandler"
	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type topologyReader interface {
	Read(context.Context, infrastructure.ReadRequest) (infrastructure.Projection, error)
}

type Config struct {
	DB         *sql.DB
	Kubernetes topologyReader
	Now        func() time.Time
}

type Source struct {
	cfg Config
}

var _ taskhandler.VerificationObservationSource = (*Source)(nil)

func New(config Config) (*Source, error) {
	if config.DB == nil || config.Kubernetes == nil {
		return nil, errors.New("operational recovery source requires MySQL and Kubernetes topology reads")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Source{cfg: config}, nil
}

func (s *Source) Observe(ctx context.Context, run verification.Run, check verification.Check) (verification.Observation, error) {
	if s == nil || run.ID == 0 || run.IncidentID == 0 || check.VerificationRunID != run.ID ||
		run.Plan.TriggerType != "operational_recovery" || run.Plan.ProfileID != verification.OperationalRecoveryProfileID ||
		check.ProfileID != verification.OperationalRecoveryProfileID || strings.TrimSpace(check.Subject.Cluster) == "" ||
		strings.TrimSpace(check.Subject.Environment) == "" || strings.TrimSpace(check.Subject.Namespace) == "" ||
		strings.TrimSpace(check.Subject.Service) == "" || strings.TrimSpace(check.Subject.WorkloadKind) == "" ||
		strings.TrimSpace(check.Subject.WorkloadName) == "" || check.Subject.Repository != "" || check.Subject.PullRequest != 0 ||
		check.Subject.Revision != "" || check.Subject.ArgoApplication != "" || check.Subject.ArgoProject != "" {
		return verification.Observation{}, verification.ErrInvalidArgument
	}
	switch check.Type {
	case verification.CheckIncidentAlertsResolved:
		return s.observeAlertRelations(ctx, run)
	case verification.CheckWorkloadReady:
		return s.observeWorkload(ctx, check)
	default:
		return verification.Observation{}, verification.ErrInvalidArgument
	}
}

func (s *Source) observeAlertRelations(ctx context.Context, run verification.Run) (verification.Observation, error) {
	var total, unresolved int
	err := s.cfg.DB.QueryRowContext(ctx, `SELECT COUNT(*),
       COALESCE(SUM(CASE WHEN alert.status = 'resolved' THEN 0 ELSE 1 END), 0)
FROM verification_runs recovery_run
JOIN alert_incident_links relation
  ON relation.incident_id = recovery_run.incident_id
 AND relation.incident_cycle_no = recovery_run.cycle_no
JOIN alerts alert ON alert.id = relation.alert_id
WHERE recovery_run.id = ? AND recovery_run.incident_id = ?
  AND recovery_run.trigger_type = 'operational_recovery'`, run.ID, run.IncidentID).Scan(&total, &unresolved)
	if err != nil {
		return unavailable("mysql_alert_relations", "alert_relations_unavailable"), nil
	}
	return booleanObservation(total > 0 && unresolved == 0, unresolved, s.cfg.Now(),
		"mysql://alert_incident_links/"+run.IncidentPublicID), nil
}

func (s *Source) observeWorkload(ctx context.Context, check verification.Check) (verification.Observation, error) {
	projection, err := s.cfg.Kubernetes.Read(ctx, infrastructure.ReadRequest{
		ClusterID: check.Subject.Cluster, Namespaces: []string{check.Subject.Namespace}, Limit: infrastructure.DefaultLimit,
	})
	if err != nil {
		return unavailable("kubernetes_read", "provider_unavailable"), nil
	}
	reference := "kubernetes://" + check.Subject.Cluster + "/" + check.Subject.Namespace + "/" +
		strings.ToLower(check.Subject.WorkloadKind) + "/" + check.Subject.WorkloadName
	if projection.Partial || projection.Truncated || projection.Source.ClusterID != check.Subject.Cluster || projection.Source.CollectedAt.IsZero() {
		return unavailable(reference, "topology_incomplete"), nil
	}
	matches, healthy := 0, false
	for _, resource := range projection.Nodes {
		if strings.EqualFold(resource.Kind, check.Subject.WorkloadKind) && resource.Namespace == check.Subject.Namespace && resource.Name == check.Subject.WorkloadName {
			matches++
			healthy = resource.Health.State == infrastructure.HealthHealthy
		}
	}
	return booleanObservation(matches == 1 && healthy, max(matches-1, 0), s.cfg.Now(), reference), nil
}

func booleanObservation(matched bool, mismatches int, sampledAt time.Time, reference string) verification.Observation {
	value := 0.0
	if matched {
		value = 1
	} else if mismatches == 0 {
		mismatches = 1
	}
	return verification.Observation{
		Status: verification.ObservationAvailable, Value: value, MatchedCount: mismatches, SampleCount: 1,
		SampledAt: sampledAt.UTC(), QueryValid: true, SourceHealthy: true, RetentionCovered: true,
		SourceReference: reference,
	}
}

func unavailable(reference, reason string) verification.Observation {
	return verification.Observation{
		Status: verification.ObservationUnavailable, ReasonCode: reason, SourceReference: reference + "://unavailable",
	}
}
