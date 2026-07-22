package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
	"github.com/05allan1213/CloudOps-Copilot/internal/schemaversion"
)

type command string

const (
	commandUp             command = "up"
	commandCutoverCheck   command = "cutover-check"
	commandCutoverWrite   command = "cutover-write"
	commandCutoverPrepare command = "cutover-prepare"
	commandCutoverExport  command = "cutover-export"
	commandBackfillV3     command = "backfill-v3"
)

const commandUsage = "usage: cloudops-migrate [up|cutover-check|cutover-export|backfill-v3 --plan-version N --source-exact-sha SHA --binary-image-digest sha256:... [--batch-size N]|cutover-prepare --plan-version N --source-exact-sha SHA --binary-image-digest sha256:... [--backfill-batch-size N] --observed-ingress-writers 0 --observed-mutation-writers 0 --observed-legacy-workers 0 --observed-unknown-external-writes 0|cutover-write --plan-version N --source-exact-sha SHA --binary-image-digest sha256:... --source-schema-version N --target-schema-version N --quiesce-ledger-id UUID --reconciliation-ledger-id UUID --converter-audit-ledger-id UUID --old-worker-count 0 --confirm-irreversible CUTOVER-V3]"

type invocation struct {
	command  command
	write    cutover.WriteRequest
	prepare  cutover.PrepareRequest
	backfill cutover.BackfillRequest
}

func Run(ctx context.Context, args []string) (retErr error) {
	operation, err := parseCommand(args)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	log, err := logger.Init("cloudops-migrate")
	if err != nil {
		return err
	}
	defer logger.Sync(log)

	db, err := sql.Open("mysql", cfg.MySQL.DSN())
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, db.Close()) }()
	pingCtx, pingCancel := context.WithTimeout(ctx, cfg.MySQL.PingTimeout)
	err = db.PingContext(pingCtx)
	pingCancel()
	if err != nil {
		return fmt.Errorf("connect migration database: %w", err)
	}

	commandCtx, commandCancel := context.WithTimeout(ctx, cfg.CommandTimeout)
	defer commandCancel()
	if operation.command == commandCutoverCheck {
		guard, err := cutover.NewSQLRuntimeGuard(db, cutover.CurrentRuntimeGeneration)
		if err != nil {
			return fmt.Errorf("initialize cutover marker check: %w", err)
		}
		if err := guard.Check(commandCtx); err != nil {
			return fmt.Errorf("cutover marker check: %w", err)
		}
		return nil
	}
	if operation.command == commandCutoverWrite {
		writer, err := cutover.NewSQLMarkerWriter(db, cfg.LockTimeout)
		if err != nil {
			return fmt.Errorf("initialize cutover marker writer: %w", err)
		}
		if _, err := writer.Write(commandCtx, operation.write); err != nil {
			return fmt.Errorf("write cutover marker: %w", err)
		}
		return nil
	}
	if operation.command == commandCutoverPrepare {
		preparer, err := cutover.NewPhase7APreparer(db, cfg.LockTimeout)
		if err != nil {
			return fmt.Errorf("initialize phase7a preparer: %w", err)
		}
		reconciler, err := newLegacyChangeReconciler(cfg.GitHub)
		if err != nil {
			return fmt.Errorf("initialize Phase 7A external reconciliation: %w", err)
		}
		preparer.WithLegacyChangeReconciler(reconciler)
		report, err := preparer.Prepare(commandCtx, operation.prepare)
		if err != nil {
			return fmt.Errorf("prepare phase7a cutover: %w", err)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if operation.command == commandBackfillV3 {
		backfiller, err := cutover.NewPhase7ABackfiller(db, cfg.LockTimeout)
		if err != nil {
			return fmt.Errorf("initialize Phase 7A backfiller: %w", err)
		}
		report, err := backfiller.Run(commandCtx, operation.backfill)
		if err != nil {
			return fmt.Errorf("run BACKFILL-V3: %w", err)
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	if operation.command == commandCutoverExport {
		if err := cutover.ExportAudit(commandCtx, db, os.Stdout); err != nil {
			return fmt.Errorf("export phase7a audit: %w", err)
		}
		return nil
	}

	runner, err := migrationrunner.NewRunner(ctx, db, cfg.LockTimeout)
	if err != nil {
		return err
	}
	if _, err := runner.Up(commandCtx); err != nil {
		return err
	}
	version, err := runner.Version(commandCtx)
	if err != nil {
		return err
	}
	if version != migrationrunner.LatestVersion {
		return fmt.Errorf("migration completed at schema version %d, want %d", version, migrationrunner.LatestVersion)
	}
	return nil
}

func parseCommand(args []string) (invocation, error) {
	if len(args) == 0 {
		return invocation{command: commandUp}, nil
	}
	switch command(args[0]) {
	case commandUp, commandCutoverCheck, commandCutoverExport:
		if len(args) != 1 {
			return invocation{}, errors.New(commandUsage)
		}
		return invocation{command: command(args[0])}, nil
	case commandCutoverPrepare:
		var result invocation
		result.command = commandCutoverPrepare
		flags := flag.NewFlagSet(string(commandCutoverPrepare), flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.Uint64Var(&result.prepare.PlanVersion, "plan-version", 0, "cutover plan version")
		flags.StringVar(&result.prepare.SourceExactSHA, "source-exact-sha", "", "exact source SHA")
		flags.StringVar(&result.prepare.BinaryImageDigest, "binary-image-digest", "", "exact binary image digest")
		flags.Uint64Var(&result.prepare.BackfillBatchSize, "backfill-batch-size", 0, "bounded BACKFILL-V3 batch size")
		flags.Uint64Var(&result.prepare.ObservedIngressWriters, "observed-ingress-writers", 1, "observed ingress writers")
		flags.Uint64Var(&result.prepare.ObservedMutationWriters, "observed-mutation-writers", 1, "observed mutation writers")
		flags.Uint64Var(&result.prepare.ObservedLegacyWorkers, "observed-legacy-workers", 1, "observed legacy workers")
		flags.Uint64Var(&result.prepare.ObservedUnknownExternalWrite, "observed-unknown-external-writes", 1, "observed unknown external writes")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return invocation{}, errors.New(commandUsage)
		}
		if err := result.prepare.Validate(); err != nil {
			return invocation{}, fmt.Errorf("%s: %w", commandUsage, err)
		}
		return result, nil
	case commandBackfillV3:
		var result invocation
		result.command = commandBackfillV3
		flags := flag.NewFlagSet(string(commandBackfillV3), flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.Uint64Var(&result.backfill.Identity.PlanVersion, "plan-version", 0, "backfill plan version")
		flags.StringVar(&result.backfill.Identity.SourceExactSHA, "source-exact-sha", "", "exact source SHA")
		flags.StringVar(&result.backfill.Identity.BinaryImageDigest, "binary-image-digest", "", "exact binary image digest")
		flags.Uint64Var(&result.backfill.BatchSize, "batch-size", 0, "bounded BACKFILL-V3 batch size")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return invocation{}, errors.New(commandUsage)
		}
		result.backfill.Identity.SourceSchema = uint64(schemaversion.Latest - 1)
		result.backfill.Identity.TargetSchema = uint64(schemaversion.Latest)
		if err := result.backfill.Validate(); err != nil {
			return invocation{}, fmt.Errorf("%s: %w", commandUsage, err)
		}
		return result, nil
	case commandCutoverWrite:
		var result invocation
		result.command = commandCutoverWrite
		result.write.OldWorkerCount = -1
		flags := flag.NewFlagSet(string(commandCutoverWrite), flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		flags.Uint64Var(&result.write.PlanVersion, "plan-version", 0, "cutover plan version")
		flags.StringVar(&result.write.SourceExactSHA, "source-exact-sha", "", "exact source SHA")
		flags.StringVar(&result.write.BinaryImageDigest, "binary-image-digest", "", "exact binary image digest")
		flags.Uint64Var(&result.write.SourceSchemaVersion, "source-schema-version", 0, "source schema version")
		flags.Uint64Var(&result.write.TargetSchemaVersion, "target-schema-version", 0, "target schema version")
		flags.StringVar(&result.write.QuiesceLedgerPublicID, "quiesce-ledger-id", "", "passed quiesce ledger UUID")
		flags.StringVar(&result.write.ReconciliationLedgerPublicID, "reconciliation-ledger-id", "", "passed reconciliation ledger UUID")
		flags.StringVar(&result.write.ConverterAuditLedgerPublicID, "converter-audit-ledger-id", "", "passed converter audit ledger UUID")
		flags.Int64Var(&result.write.OldWorkerCount, "old-worker-count", -1, "observed old worker count")
		flags.StringVar(&result.write.Confirmation, "confirm-irreversible", "", "must equal CUTOVER-V3")
		if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
			return invocation{}, errors.New(commandUsage)
		}
		if err := result.write.Validate(); err != nil {
			return invocation{}, fmt.Errorf("%s: %w", commandUsage, err)
		}
		return result, nil
	default:
		return invocation{}, errors.New(commandUsage)
	}
}
