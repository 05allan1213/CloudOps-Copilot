package migrate

import (
	"context"
	"testing"
)

func TestCommandAllowsOnlyForwardMigration(t *testing.T) {
	for _, args := range [][]string{{"down"}, {"status"}, {"up", "unexpected"}, {"cutover-check"}} {
		if err := Run(context.Background(), args); err == nil {
			t.Fatalf("cloudops-migrate accepted %v", args)
		}
	}
}
