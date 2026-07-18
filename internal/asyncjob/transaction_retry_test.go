package asyncjob

import (
	"context"
	"errors"
	"fmt"
	"testing"

	drivermysql "github.com/go-sql-driver/mysql"
)

func TestRetryTransactionValueRetriesOnlyLockConflicts(t *testing.T) {
	t.Parallel()
	for _, code := range []uint16{1205, 1213} {
		attempts := 0
		value, err := retryTransactionValue(context.Background(), func() (string, error) {
			attempts++
			if attempts < maxTransactionAttempts {
				return "", fmt.Errorf("transaction attempt %d: %w", attempts, &drivermysql.MySQLError{Number: code, Message: "retry"})
			}
			return "committed", nil
		})
		if err != nil || value != "committed" || attempts != maxTransactionAttempts {
			t.Fatalf("mysql %d value/attempts/error=%q/%d/%v", code, value, attempts, err)
		}
	}

	attempts := 0
	want := errors.New("not retryable")
	if _, err := retryTransactionValue(context.Background(), func() (struct{}, error) {
		attempts++
		return struct{}{}, want
	}); !errors.Is(err, want) || attempts != 1 {
		t.Fatalf("non-retryable attempts/error=%d/%v", attempts, err)
	}
}
