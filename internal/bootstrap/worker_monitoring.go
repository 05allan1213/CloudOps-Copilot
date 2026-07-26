package bootstrap

import (
	"net/http"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/monitoringgateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/monitoringprometheus"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func monitoringGatewayHandler(configuration *settings.Service) (http.Handler, error) {
	adapter, err := monitoringprometheus.New(configuration)
	if err != nil {
		return nil, err
	}
	return monitoringgateway.NewServer(adapter)
}
