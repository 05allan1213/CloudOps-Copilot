package api

import (
	"fmt"

	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

type APIConfig struct {
	Application appconfig.Config
}

func LoadAPIConfig() (APIConfig, error) {
	result := APIConfig{Application: appconfig.Load()}
	if err := result.Validate(); err != nil {
		return APIConfig{}, err
	}
	return result, nil
}

func (c APIConfig) Validate() error {
	if err := c.Application.Validate(); err != nil {
		return fmt.Errorf("invalid cloudops-api config: %w", err)
	}
	return nil
}
