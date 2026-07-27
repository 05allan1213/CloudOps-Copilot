package startup

import (
	"context"
	"encoding/json"
	"errors"

	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
	contractapi "github.com/05allan1213/CloudOps-Copilot/internal/api"
)

type alertInvestigationStarter struct {
	commands contractapi.CommandPort
}

func (s alertInvestigationStarter) StartInvestigation(ctx context.Context, incidentID string, version uint64, idempotencyKey, reason string, actor alertdomain.Actor) error {
	body, _ := json.Marshal(map[string]any{"expected_version": version, "reason": reason})
	_, err := s.commands.Execute(ctx, contractapi.CommandRequest{
		Kind: contractapi.CommandStartInvestigation, ResourceID: incidentID,
		Actor:          contractapi.OwnerIdentity{Subject: "local-owner", Provider: actor.Provider, Login: actor.Login, Role: actor.Role},
		IdempotencyKey: idempotencyKey, ExpectedVersion: version, CanonicalBody: body,
		RequestID: "alert-" + idempotencyKey[:16], TraceID: "alert-" + idempotencyKey[:16],
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, contractapi.ErrNotFound):
		return alertdomain.ErrNotFound
	case errors.Is(err, contractapi.ErrStaleVersion):
		return alertdomain.ErrStaleVersion
	case errors.Is(err, contractapi.ErrConflict), errors.Is(err, contractapi.ErrInvalidTransition):
		return alertdomain.ErrConflict
	case errors.Is(err, contractapi.ErrInvalidArgument), errors.Is(err, contractapi.ErrForbidden):
		return alertdomain.ErrInvalid
	default:
		return alertdomain.ErrProviderUnavailable
	}
}
