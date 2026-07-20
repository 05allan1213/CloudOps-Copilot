package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	"github.com/05allan1213/CloudOps-Copilot/internal/cutover"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

type command string

const (
	commandUp           command = "up"
	commandCutoverCheck command = "cutover-check"
)

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
	if operation == commandCutoverCheck {
		guard, err := cutover.NewSQLRuntimeGuard(db, cutover.CurrentRuntimeGeneration)
		if err != nil {
			return fmt.Errorf("initialize cutover marker check: %w", err)
		}
		if err := guard.Check(commandCtx); err != nil {
			return fmt.Errorf("cutover marker check: %w", err)
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

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return commandUp, nil
	}
	if len(args) != 1 {
		return "", fmt.Errorf("usage: cloudops-migrate [up|cutover-check]")
	}
	switch command(args[0]) {
	case commandUp, commandCutoverCheck:
		return command(args[0]), nil
	default:
		return "", fmt.Errorf("usage: cloudops-migrate [up|cutover-check]")
	}
}
