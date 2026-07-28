package taskhandler

import (
	"database/sql"
	"errors"
	"fmt"
)

func joinRowsCloseError(retErr *error, rows *sql.Rows, operation string) {
	if closeErr := rows.Close(); closeErr != nil {
		*retErr = errors.Join(*retErr, fmt.Errorf("%s: %w", operation, closeErr))
	}
}
