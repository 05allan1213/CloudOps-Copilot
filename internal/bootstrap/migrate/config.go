package migrate

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/database"
)

type Config struct {
	MySQL          database.MySQLConfig
	LockTimeout    time.Duration
	CommandTimeout time.Duration
	GitHub         GitHubReconcileConfig
}

type GitHubReconcileConfig struct {
	BaseURL             string
	TokenFile           string
	AppID               int64
	InstallationID      int64
	PrivateKeyFile      string
	AllowedRepositories []string
	Timeout             time.Duration
	MaxRetries          int
}

func (c GitHubReconcileConfig) Configured() bool {
	return strings.TrimSpace(c.TokenFile) != "" || c.AppID != 0 || c.InstallationID != 0 ||
		strings.TrimSpace(c.PrivateKeyFile) != "" || len(c.AllowedRepositories) != 0
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
		GitHub: GitHubReconcileConfig{
			BaseURL:             configutil.NonEmptyString("CUTOVER_GITHUB_API_BASE_URL", "https://api.github.com"),
			TokenFile:           configutil.String("CUTOVER_GITHUB_TOKEN_FILE", ""),
			AppID:               int64(configutil.NonNegativeInt("CUTOVER_GITHUB_APP_ID", 0)),
			InstallationID:      int64(configutil.NonNegativeInt("CUTOVER_GITHUB_INSTALLATION_ID", 0)),
			PrivateKeyFile:      configutil.String("CUTOVER_GITHUB_PRIVATE_KEY_FILE", ""),
			AllowedRepositories: configutil.List("CUTOVER_GITHUB_ALLOWED_REPOSITORIES"),
			Timeout:             configutil.DurationSeconds("CUTOVER_GITHUB_TIMEOUT_SECONDS", 10),
			MaxRetries:          configutil.NonNegativeInt("CUTOVER_GITHUB_MAX_RETRIES", 1),
		},
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
	if !c.GitHub.Configured() {
		return nil
	}
	if err := configutil.ValidateHTTPURL("CUTOVER_GITHUB_API_BASE_URL", c.GitHub.BaseURL); err != nil ||
		!strings.HasPrefix(strings.ToLower(c.GitHub.BaseURL), "https://") {
		return errors.New("CUTOVER_GITHUB_API_BASE_URL must be a valid https URL")
	}
	fileAuth := strings.TrimSpace(c.GitHub.TokenFile) != ""
	appAuth := c.GitHub.AppID > 0 && c.GitHub.InstallationID > 0 && strings.TrimSpace(c.GitHub.PrivateKeyFile) != ""
	if fileAuth == appAuth {
		return errors.New("cutover GitHub reconciliation requires exactly one read-only token-file or App credential")
	}
	if len(c.GitHub.AllowedRepositories) == 0 || c.GitHub.Timeout <= 0 || c.GitHub.MaxRetries < 0 || c.GitHub.MaxRetries > 3 {
		return errors.New("cutover GitHub reconciliation allowlist or bounds are invalid")
	}
	for _, repository := range c.GitHub.AllowedRepositories {
		if strings.Count(strings.Trim(strings.TrimSpace(repository), "/"), "/") != 1 {
			return fmt.Errorf("invalid cutover GitHub repository allowlist entry %q", repository)
		}
	}
	return nil
}
