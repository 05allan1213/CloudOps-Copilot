package incidentmysql

import (
	"database/sql"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/remediationmysql"
)

// V3RemediationRepository remains as a compatibility alias for existing API
// and integration callers. New production Worker assembly imports the narrow
// remediationmysql package directly.
type V3RemediationRepository = remediationmysql.V3RemediationRepository

func NewV3RemediationRepository(db *sql.DB) (*V3RemediationRepository, error) {
	return remediationmysql.NewV3RemediationRepository(db)
}
