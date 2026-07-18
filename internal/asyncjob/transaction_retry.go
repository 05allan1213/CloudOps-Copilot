package asyncjob

import (
	"context"
	"errors"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
)

const maxTransactionAttempts = 3

func retryTransactionValue[T any](ctx context.Context, operation func() (T, error)) (T, error) {
	var zero T
	for attempt := 1; attempt <= maxTransactionAttempts; attempt++ {
		value, err := operation()
		if err == nil {
			return value, nil
		}
		if !retryableTransactionError(err) || attempt == maxTransactionAttempts {
			return zero, err
		}
		timer := time.NewTimer(time.Duration(attempt) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
	return zero, errors.New("async task transaction retry invariant violated")
}

func retryTransactionError(ctx context.Context, operation func() error) error {
	_, err := retryTransactionValue(ctx, func() (struct{}, error) {
		return struct{}{}, operation()
	})
	return err
}

func retryableTransactionError(err error) bool {
	var mysqlError *drivermysql.MySQLError
	return errors.As(err, &mysqlError) && (mysqlError.Number == 1205 || mysqlError.Number == 1213)
}
