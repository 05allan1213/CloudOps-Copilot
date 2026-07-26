package migrate

import (
	"errors"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
)

type Config struct {
	MySQL          database.MySQLConfig
	LockTimeout    time.Duration
	CommandTimeout time.Duration
}

func LoadConfig() (Config, error) {
	result := Config{
		MySQL: database.MySQLConfig{
			Host:        configutil.String("MYSQL_HOST", ""),
			Port:        configutil.String("MYSQL_PORT", "3306"),
			User:        configutil.String("MYSQL_USER", ""),
			Password:    configutil.String("MYSQL_PASSWORD", ""),
			Database:    configutil.String("MYSQL_DATABASE", ""),
			PingTimeout: configutil.DurationSeconds("MYSQL_PING_TIMEOUT_SECONDS", 3),
		},
		LockTimeout:    configutil.DurationSeconds("MIGRATION_LOCK_TIMEOUT_SECONDS", 60),
		CommandTimeout: configutil.DurationSeconds("MIGRATION_COMMAND_TIMEOUT_SECONDS", 120),
	}
	if err := result.Validate(); err != nil {
		return Config{}, err
	}
	return result, nil
}

func (c Config) Validate() error {
	if c.MySQL.Host == "" || c.MySQL.User == "" || c.MySQL.Database == "" {
		return errors.New("MYSQL_HOST, MYSQL_USER and MYSQL_DATABASE are required")
	}
	if err := configutil.ValidatePort("MYSQL_PORT", c.MySQL.Port); err != nil {
		return err
	}
	if c.MySQL.PingTimeout <= 0 || c.LockTimeout <= 0 || c.CommandTimeout <= 0 {
		return errors.New("migration timeouts must be positive")
	}
	return nil
}
