package api

import (
	"net/http"
	"strings"
)

func (a *API) handleGetServicePorts(w http.ResponseWriter, r *http.Request) {
	if a.serviceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			"SERVICE_REGISTRY_UNAVAILABLE",
			"ServiceRegistry 未初始化",
			"discovery",
			"discovery_api",
			map[string]any{"dependency": "service_registry", "operation": "get_service_ports"},
		), nil)
		return
	}
	entries, err := a.serviceRegistry.GetAllPorts()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "database disk image is malformed") {
			Success(w, map[string]any{})
			return
		}
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			"SERVICE_PORTS_READ_FAILED",
			"读取服务端口失败",
			"discovery",
			"discovery_api",
			"请检查服务注册状态后重试。",
			map[string]any{"operation": "get_service_ports"},
		), nil)
		return
	}
	portMap := make(map[string]map[string]any, len(entries))
	for _, e := range entries {
		portMap[e.ServiceName] = map[string]any{
			"host":     e.Host,
			"port":     e.Port,
			"protocol": e.Protocol,
			"pid":      e.PID,
			"status":   e.Status,
		}
	}
	Success(w, portMap)
}
