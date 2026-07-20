package api

import (
	"fmt"
	"net"
	"strings"

	"github.com/05allan1213/CloudOps-Copilot/internal/bootstrap/configutil"
	appconfig "github.com/05allan1213/CloudOps-Copilot/internal/config"
)

type APIConfig struct {
	Application        appconfig.Config
	InternalListenAddr string
}

func LoadAPIConfig() (APIConfig, error) {
	result := APIConfig{
		Application:        appconfig.Load(),
		InternalListenAddr: configutil.String("INTERNAL_LISTEN_ADDR", ":8082"),
	}
	if err := result.Validate(); err != nil {
		return APIConfig{}, err
	}
	return result, nil
}

func (c APIConfig) Validate() error {
	c = c.normalized()
	if err := c.Application.Validate(); err != nil {
		return fmt.Errorf("invalid cloudops-api config: %w", err)
	}
	if err := configutil.ValidateListenAddr("INTERNAL_LISTEN_ADDR", c.InternalListenAddr); err != nil {
		return err
	}
	if strings.TrimSpace(c.Application.ListenAddr) == strings.TrimSpace(c.InternalListenAddr) {
		return fmt.Errorf("LISTEN_ADDR and INTERNAL_LISTEN_ADDR must be different")
	}
	userHost, userPort, _ := net.SplitHostPort(c.Application.ListenAddr)
	_, internalPort, _ := net.SplitHostPort(c.InternalListenAddr)
	if userPort == internalPort {
		return fmt.Errorf("LISTEN_ADDR and INTERNAL_LISTEN_ADDR must use different ports")
	}
	if c.Application.V3ProxyAuthEnabled && userHost != "127.0.0.1" {
		return fmt.Errorf("LISTEN_ADDR must bind 127.0.0.1 when V3_PROXY_AUTH_ENABLED is true")
	}
	return nil
}

func (c APIConfig) normalized() APIConfig {
	if strings.TrimSpace(c.InternalListenAddr) == "" {
		c.InternalListenAddr = ":8082"
	}
	return c
}
