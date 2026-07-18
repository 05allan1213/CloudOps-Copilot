package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/logger"
	migrationrunner "github.com/05allan1213/CloudOps-Copilot/internal/migration"
)

func Run(ctx context.Context, args []string) (retErr error) {
	if len(args) > 1 || (len(args) == 1 && args[0] != "up") {
		return fmt.Errorf("usage: cloudops-migrate [up]")
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

	runner, err := migrationrunner.NewRunner(ctx, db, cfg.LockTimeout)
	if err != nil {
		return err
	}
	commandCtx, commandCancel := context.WithTimeout(ctx, cfg.CommandTimeout)
	defer commandCancel()
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
