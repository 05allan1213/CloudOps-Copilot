package migrate

import (
	"context"
	"testing"
)

func TestCommandRejectsNonForwardOperation(t *testing.T) {
	if err := Run(context.Background(), []string{"down"}); err == nil {
		t.Fatal("cloudops-migrate accepted a down operation")
	}
}
