package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/autocarto/carto_agent/internal/integrations"
)

func (a *API) handleCheckIntegration(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "integration_check") {
		return
	}
	if a.configService == nil {
		Error(w, http.StatusServiceUnavailable, "扩展服务配置不可用")
		return
	}
	var body struct {
		Service string `json:"service"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "请提供有效的 service 参数")
		return
	}
	if body.Service != "gee" && body.Service != "browser" && body.Service != "osm" {
		Error(w, http.StatusBadRequest, "service 必须为 gee、browser 或 osm")
		return
	}
	c, err := a.configService.GetIntegrationsConfig()
	if err != nil {
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	Success(w, integrations.Check(ctx, body.Service, c))
}
