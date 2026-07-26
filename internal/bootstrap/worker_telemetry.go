package bootstrap

import (
	"net/http"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/telemetrygateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/telemetryread"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func telemetryGatewayHandler(configuration *settings.Service) (http.Handler, error) {
	provider, err := telemetryread.New(configuration)
	if err != nil {
		return nil, err
	}
	return telemetrygateway.NewServer(provider)
}
