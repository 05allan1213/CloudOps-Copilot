// Command migrate applies the explicit CloudOps V2 Goose migrations.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/pressly/goose/v3"

	"server-monitor/pkg/configutil"
	"server-web/internal/infra/database"
	"server-web/migrations"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migration failed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (retErr error) {
	if len(args) != 1 {
		return fmt.Errorf("usage: migrate <up|down|status|version>")
	}
	client, err := database.OpenMySQL(ctx, database.MySQLConfig{
		Host: configutil.String("MYSQL_HOST", ""), Port: configutil.String("MYSQL_PORT", "3306"),
		User: configutil.String("MYSQL_USER", ""), Password: configutil.String("MYSQL_PASSWORD", ""),
		Database: configutil.String("MYSQL_DATABASE", ""), PingTimeout: configutil.DurationSeconds("MYSQL_PING_TIMEOUT_SECONDS", 3),
	})
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("MYSQL_HOST, MYSQL_USER and MYSQL_DATABASE are required")
	}
	defer func() { retErr = errors.Join(retErr, client.Close()) }()
	sqlDB, err := client.DB().DB()
	if err != nil {
		return err
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	switch args[0] {
	case "up":
		return goose.UpContext(commandCtx, sqlDB, ".")
	case "down":
		return goose.DownContext(commandCtx, sqlDB, ".")
	case "status":
		return goose.StatusContext(commandCtx, sqlDB, ".")
	case "version":
		return goose.VersionContext(commandCtx, sqlDB, ".")
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
