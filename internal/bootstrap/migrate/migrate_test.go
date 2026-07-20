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

func TestParseCommandAllowsOnlyForwardMigrationOrReadOnlyCutoverCheck(t *testing.T) {
	tests := []struct {
		args []string
		want command
	}{
		{args: nil, want: commandUp},
		{args: []string{"up"}, want: commandUp},
		{args: []string{"cutover-check"}, want: commandCutoverCheck},
	}
	for _, test := range tests {
		got, err := parseCommand(test.args)
		if err != nil || got != test.want {
			t.Fatalf("parseCommand(%v)=(%q,%v), want %q", test.args, got, err, test.want)
		}
	}
	for _, args := range [][]string{{"down"}, {"status"}, {"cutover-check", "up"}} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%v) unexpectedly succeeded", args)
		}
	}
}
