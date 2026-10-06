package control

import (
	"strings"

	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

type SharedErrorDictionary struct {
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	UserAction string `json:"user_action,omitempty"`
	Retryable  bool   `json:"retryable"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

type sharedErrorDictionarySeed struct {
	message    string
	userAction string
	retryable  bool
}

var sharedErrorDictionarySeeds = map[string]sharedErrorDictionarySeed{
	"provider_missing": {
		message:    "Provider 未配置，当前能力已降级。",
		userAction: "请先配置至少一个 Provider 后重试。",
		retryable:  true,
	},
	"unsupported_runtime": {
		message:    "当前运行时不支持该能力。",
		userAction: "请切换到受支持的运行时或启动对应服务。",
		retryable:  false,
	},
	toolcatalog.MissingReasonAbnormalEmptyCatalog: {
		message:    "工具目录异常为空，当前能力已降级。",
		userAction: "请刷新工具目录或检查运行态注册是否完成。",
		retryable:  true,
	},
	"catalog_degraded": {
		message:    "工具目录处于降级状态。",
		userAction: "请刷新工具目录并检查运行态健康状态。",
		retryable:  true,
	},
	"instance_expired": {
		message:    "QGIS 实例已过期。",
		userAction: "请等待实例重新注册或重新启动 QGIS。",
		retryable:  true,
	},
	"descriptor_blocked": {
		message:    "runtime discovery descriptor 尚未就绪。",
		userAction: "请等待 descriptor ready 后再重试。",
		retryable:  true,
	},
}

func RetryableForSharedErrorCode(code string) bool {
	normalized := normalizeSharedErrorCode(code)
	if seed, ok := sharedErrorDictionarySeeds[normalized]; ok {
		return seed.retryable
	}
	return false
}

func SharedErrorDictionaryFromErrorEnvelope(envelope *ErrorEnvelope) SharedErrorDictionary {
	if envelope == nil {
		return SharedErrorDictionary{}
	}
	seed := resolveSharedErrorDictionarySeed(normalizeSharedErrorCode(envelope.Code))
	return SharedErrorDictionary{
		Code:       normalizeSharedErrorCode(envelope.Code),
		Message:    firstNonEmptyString(strings.TrimSpace(envelope.Message), seed.message),
		UserAction: firstNonEmptyString(strings.TrimSpace(envelope.UserAction), seed.userAction),
		Retryable:  envelope.Retryable || seed.retryable,
		EvidenceID: strings.TrimSpace(envelope.EvidenceID),
	}
}

func SharedErrorDictionaryFromRegistryAvailability(status RegistryAvailability, evidenceID string) SharedErrorDictionary {
	code := normalizeSharedErrorCode(status.Code)
	seed := resolveSharedErrorDictionarySeed(code)
	return SharedErrorDictionary{
		Code:       code,
		Message:    firstNonEmptyString(strings.TrimSpace(status.Message), seed.message),
		UserAction: seed.userAction,
		Retryable:  seed.retryable,
		EvidenceID: strings.TrimSpace(evidenceID),
	}
}

func SharedErrorDictionaryFromToolCatalogContract(contract toolcatalog.Contract, evidenceID string) SharedErrorDictionary {
	code := normalizeSharedCatalogCode(contract)
	seed := resolveSharedErrorDictionarySeed(code)
	return SharedErrorDictionary{
		Code:       code,
		Message:    seed.message,
		UserAction: seed.userAction,
		Retryable:  seed.retryable,
		EvidenceID: strings.TrimSpace(evidenceID),
	}
}

func normalizeSharedCatalogCode(contract toolcatalog.Contract) string {
	code := normalizeSharedErrorCode(contract.MissingDependencyReason)
	if code != "" {
		return code
	}
	switch strings.TrimSpace(contract.DegradationStatus) {
	case toolcatalog.DegradationStatusProviderMissing:
		return "provider_missing"
	case toolcatalog.DegradationStatusUnsupportedRuntime:
		return "unsupported_runtime"
	case toolcatalog.DegradationStatusAbnormalEmpty:
		return toolcatalog.MissingReasonAbnormalEmptyCatalog
	case toolcatalog.DegradationStatusMissingDependency:
		return "catalog_degraded"
	default:
		return ""
	}
}

func normalizeSharedErrorCode(code string) string {
	normalized := strings.TrimSpace(strings.ToLower(code))
	switch normalized {
	case "lease_expired":
		return "instance_expired"
	case toolcatalog.MissingReasonRuntimeCatalogMissing:
		return "catalog_degraded"
	default:
		return normalized
	}
}

func resolveSharedErrorDictionarySeed(code string) sharedErrorDictionarySeed {
	if seed, ok := sharedErrorDictionarySeeds[code]; ok {
		return seed
	}
	return sharedErrorDictionarySeed{}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
