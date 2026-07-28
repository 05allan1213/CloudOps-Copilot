package bootstrap

import (
	"net/http"

	"github.com/05allan1213/CloudOps-Copilot/internal/infra/alertmanagerapi"
	"github.com/05allan1213/CloudOps-Copilot/internal/infra/alertmanagergateway"
	"github.com/05allan1213/CloudOps-Copilot/internal/settings"
)

func alertmanagerGatewayHandler(configuration *settings.Service) (http.Handler, error) {
	adapter, err := alertmanagerapi.New(configuration)
	if err != nil {
		return nil, err
	}
	return alertmanagergateway.NewServer(adapter)
}
