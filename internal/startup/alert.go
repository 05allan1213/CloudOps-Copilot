package startup

import (
	"context"
	"database/sql"
	"errors"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
	alertdomain "github.com/05allan1213/CloudOps-Copilot/internal/alert"
)

type alertInvestigationStarter struct {
	workspace *agent.WorkspaceRepository
}

func (s alertInvestigationStarter) StartAlertInvestigationTx(ctx context.Context, tx *sql.Tx, alertID, idempotencyKey, reason string) (string, error) {
	if s.workspace == nil {
		return "", alertdomain.ErrProviderUnavailable
	}
	publicID, err := s.workspace.StartAlertInvestigationTx(ctx, tx, alertID, idempotencyKey, reason)
	switch {
	case err == nil:
		return publicID, nil
	case errors.Is(err, agent.ErrNotFound):
		return "", alertdomain.ErrNotFound
	case errors.Is(err, agent.ErrConflict):
		return "", alertdomain.ErrConflict
	case errors.Is(err, agent.ErrInvalidArgument), errors.Is(err, agent.ErrPermission):
		return "", alertdomain.ErrInvalid
	default:
		return "", alertdomain.ErrProviderUnavailable
	}
}
