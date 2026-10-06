package api

import (
	"net/http"
	"strings"

	"github.com/autocarto/carto_agent/internal/protocol"
)

const (
	httpContractVersionHeader           = "X-AutoCarto-Contract-Version"
	httpSupportedContractVersionsHeader = "X-AutoCarto-Supported-Contract-Versions"
	httpContractVersionQuery            = "contract_version"
)

func writeHTTPContractHeaders(w http.ResponseWriter) {
	w.Header().Set(httpContractVersionHeader, protocol.ContractVersionV1)
	w.Header().Set(httpSupportedContractVersionsHeader, protocol.ContractVersionV1)
}

func ensureHTTPControlContractVersion(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	writeHTTPContractHeaders(w)
	if r != nil && strings.EqualFold(strings.TrimSpace(r.Method), http.MethodOptions) {
		return true
	}
	requested := ""
	if r != nil {
		requested = strings.TrimSpace(r.Header.Get(httpContractVersionHeader))
		if requested == "" && r.URL != nil {
			requested = strings.TrimSpace(r.URL.Query().Get(httpContractVersionQuery))
		}
	}
	if requested == "" || requested == protocol.ContractVersionV1 {
		return true
	}
	envelope := newRequestEnvelope(
		"HTTP_CONTRACT_VERSION_UNSUPPORTED",
		"不支持的 HTTP runtime/control contract 版本",
		"control",
		"runtime_contract",
		"请升级客户端或改用受支持的 contract 版本后重试。",
		map[string]any{
			"endpoint":                    strings.TrimSpace(endpoint),
			"requested_contract_version":  requested,
			"supported_contract_versions": protocol.SupportedContractVersions(),
		},
	)
	writeControlPlaneError(w, r, http.StatusBadRequest, nil, envelope, map[string]any{
		"contract_version":            protocol.ContractVersionV1,
		"requested_contract_version":  requested,
		"supported_contract_versions": protocol.SupportedContractVersions(),
	})
	return false
}
