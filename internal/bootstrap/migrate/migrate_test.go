package migrate

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
)

func TestCommandRejectsNonForwardOperation(t *testing.T) {
	if err := Run(context.Background(), []string{"down"}); err == nil {
		t.Fatal("cloudops-migrate accepted a down operation")
	}
}

func TestParseCommandAllowsOnlyExplicitForwardAndCutoverCommands(t *testing.T) {
	tests := []struct {
		args []string
		want command
	}{
		{args: nil, want: commandUp},
		{args: []string{"up"}, want: commandUp},
		{args: []string{"cutover-check"}, want: commandCutoverCheck},
		{args: validCutoverWriteArgs(), want: commandCutoverWrite},
	}
	for _, test := range tests {
		got, err := parseCommand(test.args)
		if err != nil || got.command != test.want {
			t.Fatalf("parseCommand(%v)=(%q,%v), want %q", test.args, got.command, err, test.want)
		}
	}
	for _, args := range [][]string{{"down"}, {"status"}, {"cutover-check", "up"}, {"cutover-write"}, append(validCutoverWriteArgs(), "unexpected")} {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parseCommand(%v) unexpectedly succeeded", args)
		}
	}
}

func TestParseCutoverWriteRequiresExplicitIrreversibleInputs(t *testing.T) {
	base := validCutoverWriteArgs()
	for _, flag := range []string{"--plan-version", "--source-exact-sha", "--binary-image-digest", "--source-schema-version", "--target-schema-version", "--quiesce-ledger-id", "--reconciliation-ledger-id", "--converter-audit-ledger-id", "--old-worker-count", "--confirm-irreversible"} {
		args := append([]string(nil), base...)
		for index := 1; index < len(args); index++ {
			if args[index] == flag {
				args = append(args[:index], args[index+2:]...)
				break
			}
		}
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("cutover-write accepted missing %s", flag)
		}
	}
}

func validCutoverWriteArgs() []string {
	version := fmt.Sprint(schemaversion.Latest)
	return []string{
		"cutover-write", "--plan-version", "7", "--source-exact-sha", strings.Repeat("a", 40),
		"--binary-image-digest", "sha256:" + strings.Repeat("b", 64),
		"--source-schema-version", version, "--target-schema-version", version,
		"--quiesce-ledger-id", "11111111-1111-4111-8111-111111111111",
		"--reconciliation-ledger-id", "22222222-2222-4222-8222-222222222222",
		"--converter-audit-ledger-id", "33333333-3333-4333-8333-333333333333",
		"--old-worker-count", "0", "--confirm-irreversible", cutover.IrreversibleConfirmation,
	}
}
