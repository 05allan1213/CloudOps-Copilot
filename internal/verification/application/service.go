// Package application implements the repository-backed delivery and verification API surface.
package application

import (
	"context"

	"go.opentelemetry.io/otel"

	"github.com/05allan1213/CloudOps-Copilot/internal/verification"
)

type Config struct {
	DeliveryEnabled     bool
	VerificationEnabled bool
	Repository          verification.Repository
}

type Service struct {
	cfg Config
}

func New(cfg Config) (*Service, error) {
	if (cfg.DeliveryEnabled || cfg.VerificationEnabled) && cfg.Repository == nil {
		return nil, verification.ErrInvalidArgument
	}
	return &Service{cfg: cfg}, nil
}

func (s *Service) DeliveryEnabled() bool {
	return s != nil && s.cfg.DeliveryEnabled
}

func (s *Service) VerificationEnabled() bool {
	return s != nil && s.cfg.VerificationEnabled
}

func (s *Service) GetDelivery(ctx context.Context, incidentID string) (*verification.Delivery, error) {
	if !s.DeliveryEnabled() {
		return nil, verification.ErrUnavailable
	}
	return s.cfg.Repository.GetDeliveryByIncident(ctx, incidentID)
}

func (s *Service) ListRuns(ctx context.Context, incidentID string, page, pageSize int) (verification.RunPage, error) {
	if !s.VerificationEnabled() {
		return verification.RunPage{}, verification.ErrUnavailable
	}
	return s.cfg.Repository.ListRuns(ctx, incidentID, page, pageSize)
}

func (s *Service) GetRun(ctx context.Context, incidentID, runID string) (*verification.Run, []verification.Check, error) {
	if !s.VerificationEnabled() {
		return nil, nil, verification.ErrUnavailable
	}
	run, err := s.cfg.Repository.GetRun(ctx, incidentID, runID)
	if err != nil {
		return nil, nil, err
	}
	checks, err := s.cfg.Repository.ListRunChecks(ctx, incidentID, runID)
	return run, checks, err
}

func (s *Service) GetPostmortem(ctx context.Context, incidentID string) (*verification.Postmortem, error) {
	if !s.VerificationEnabled() {
		return nil, verification.ErrUnavailable
	}
	ctx, span := otel.Tracer("server-web/deliveryverification").Start(ctx, "postmortem.query")
	defer span.End()
	return s.cfg.Repository.GetPostmortem(ctx, incidentID)
}
