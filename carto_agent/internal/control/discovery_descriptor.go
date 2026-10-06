package control

import (
	"fmt"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolcontract"
)

const (
	DiscoveryReadinessReady    = "ready"
	DiscoveryReadinessBlock    = "block"
	DiscoveryReadinessDegraded = "degraded"

	discoveryRetryAfterBlockMS    = 1000
	discoveryRetryAfterDegradedMS = 2000
)

type DiscoveryRetryHints struct {
	Retryable bool `json:"retryable"`
	AfterMS   int  `json:"after_ms,omitempty"`
}

type DiscoveryDescriptor struct {
	Version           string                                `json:"version"`
	Revision          uint64                                `json:"revision"`
	ServiceName       string                                `json:"service_name"`
	Port              int                                   `json:"port"`
	BackendURL        string                                `json:"backend_url"`
	WSURL             string                                `json:"ws_url"`
	Readiness         string                                `json:"readiness"`
	ReasonCode        string                                `json:"reason_code,omitempty"`
	ReasonMessage     string                                `json:"reason_message,omitempty"`
	Degraded          bool                                  `json:"degraded"`
	RetryHints        DiscoveryRetryHints                   `json:"retry_hints"`
	DiscoveryRevision string                                `json:"discovery_revision,omitempty"`
	RuntimeRevision   string                                `json:"runtime_revision,omitempty"`
	ActiveConfig      string                                `json:"active_config,omitempty"`
	ActiveInstanceID  string                                `json:"active_instance_id,omitempty"`
	ServicePhase      string                                `json:"service_phase,omitempty"`
	RestartSequence   uint64                                `json:"restart_sequence,omitempty"`
	ExecutionManifest []toolcontract.ExecutionManifestEntry `json:"execution_manifest,omitempty"`
	ToolCatalog       []toolcatalog.Item                    `json:"tool_catalog,omitempty"`
	ObservedAt        string                                `json:"observed_at"`
	ExpiresAt         string                                `json:"expires_at"`
}

func BuildDiscoveryDescriptor(snapshot middlewareclient.RuntimeSnapshot, entry discovery.ServicePortEntry, now time.Time, ttl time.Duration) (DiscoveryDescriptor, error) {
	return BuildDiscoveryDescriptorWithCatalog(snapshot, entry, now, ttl, toolcatalog.Contract{})
}

func BuildDiscoveryDescriptorWithCatalog(snapshot middlewareclient.RuntimeSnapshot, entry discovery.ServicePortEntry, now time.Time, ttl time.Duration, catalog toolcatalog.Contract) (DiscoveryDescriptor, error) {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if ttl <= 0 {
		return DiscoveryDescriptor{}, newDiscoveryDescriptorInvalidError("discovery descriptor ttl 无效", map[string]any{"ttl_ms": ttl.Milliseconds()})
	}

	serviceName := strings.TrimSpace(entry.ServiceName)
	if serviceName == "" {
		return DiscoveryDescriptor{}, newDiscoveryDescriptorInvalidError("discovery descriptor 缺少服务名", nil)
	}
	host := strings.TrimSpace(entry.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	if entry.Port <= 0 || entry.Port > 65535 {
		return DiscoveryDescriptor{}, newDiscoveryDescriptorInvalidError(
			fmt.Sprintf("discovery descriptor 端口无效: %d", entry.Port),
			map[string]any{"service_name": serviceName, "host": host, "port": entry.Port},
		)
	}

	backendScheme, wsScheme, err := normalizeDescriptorSchemes(entry.Protocol)
	if err != nil {
		return DiscoveryDescriptor{}, newDiscoveryDescriptorInvalidError(err.Error(), map[string]any{"protocol": strings.TrimSpace(entry.Protocol)})
	}

	observedAt := snapshot.Timestamp.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	expiresAt := observedAt.Add(ttl)
	if !expiresAt.After(now) {
		return DiscoveryDescriptor{}, newDiscoveryDescriptorExpiredError(observedAt, expiresAt, now)
	}

	readiness, reasonCode, reasonMessage, degraded, retryHints := deriveDiscoveryState(snapshot, entry)
	readiness, reasonCode, reasonMessage, degraded, retryHints = deriveDiscoveryCatalogState(
		readiness,
		reasonCode,
		reasonMessage,
		degraded,
		retryHints,
		catalog,
	)

	return DiscoveryDescriptor{
		Version:           protocol.ContractVersionV1,
		Revision:          snapshot.Version,
		ServiceName:       serviceName,
		Port:              entry.Port,
		BackendURL:        fmt.Sprintf("%s://%s:%d", backendScheme, host, entry.Port),
		WSURL:             fmt.Sprintf("%s://%s:%d/ws", wsScheme, host, entry.Port),
		Readiness:         readiness,
		ReasonCode:        reasonCode,
		ReasonMessage:     reasonMessage,
		Degraded:          degraded,
		RetryHints:        retryHints,
		DiscoveryRevision: ResolveDiscoveryRevision(snapshot),
		RuntimeRevision:   strings.TrimSpace(snapshot.RuntimeRevision),
		ActiveConfig:      strings.TrimSpace(snapshot.ActiveConfig),
		ActiveInstanceID:  strings.TrimSpace(snapshot.Instances.ActiveID),
		ServicePhase:      strings.TrimSpace(snapshot.ServicePhase),
		RestartSequence:   snapshot.Version,
		ObservedAt:        observedAt.Format(time.RFC3339Nano),
		ExpiresAt:         expiresAt.Format(time.RFC3339Nano),
	}, nil
}

func deriveDiscoveryCatalogState(readiness, reasonCode, reasonMessage string, degraded bool, retryHints DiscoveryRetryHints, catalog toolcatalog.Contract) (string, string, string, bool, DiscoveryRetryHints) {
	if strings.TrimSpace(readiness) != DiscoveryReadinessReady {
		return readiness, reasonCode, reasonMessage, degraded, retryHints
	}
	sharedError := SharedErrorDictionaryFromToolCatalogContract(catalog, "")
	sharedCode := strings.TrimSpace(sharedError.Code)
	if !shouldDegradeDiscoveryForCatalog(sharedCode) {
		return readiness, reasonCode, reasonMessage, degraded, retryHints
	}
	nextRetryHints := DiscoveryRetryHints{Retryable: sharedError.Retryable}
	if sharedError.Retryable {
		nextRetryHints.AfterMS = discoveryRetryAfterDegradedMS
	}
	return DiscoveryReadinessDegraded,
		strings.ToUpper(sharedCode),
		firstNonEmptyString(strings.TrimSpace(sharedError.Message), strings.TrimSpace(reasonMessage)),
		true,
		nextRetryHints
}

func shouldDegradeDiscoveryForCatalog(sharedCode string) bool {
	switch strings.TrimSpace(strings.ToLower(sharedCode)) {
	case "provider_missing", "unsupported_runtime", toolcatalog.MissingReasonAbnormalEmptyCatalog, "catalog_degraded":
		return true
	default:
		return false
	}
}

func deriveDiscoveryState(snapshot middlewareclient.RuntimeSnapshot, entry discovery.ServicePortEntry) (string, string, string, bool, DiscoveryRetryHints) {
	if snapshot.Stale {
		return DiscoveryReadinessDegraded,
			"RUNTIME_STALE",
			"运行时快照已过期，请等待后端重新发布。",
			true,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterDegradedMS}
	}
	if code, message, ok := firstDiscoveryModeIssue(snapshot.ModeIssues); ok {
		return DiscoveryReadinessDegraded,
			code,
			message,
			true,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterDegradedMS}
	}

	switch strings.ToLower(strings.TrimSpace(snapshot.ServicePhase)) {
	case "ready", "running":
		return DiscoveryReadinessReady, "", "", false, DiscoveryRetryHints{}
	case "waiting_plugin":
		return DiscoveryReadinessDegraded,
			"PLUGIN_REGISTRATION_PENDING",
			"后端已就绪，等待 plugin 完成注册后恢复完整运行态连接。",
			true,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterBlockMS}
	case "bootstrap", "starting", "restarting", "syncing":
		reasonCode, reasonMessage := discoveryBlockReason(snapshot.ServicePhase)
		return DiscoveryReadinessBlock,
			reasonCode,
			reasonMessage,
			false,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterBlockMS}
	case "degraded", "failed", "error":
		return DiscoveryReadinessDegraded,
			"BACKEND_DEGRADED",
			"后端处于 degraded 状态，请等待恢复后重试。",
			true,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterDegradedMS}
	}

	switch strings.ToLower(strings.TrimSpace(entry.Status)) {
	case "starting", "restarting", "syncing":
		return DiscoveryReadinessBlock,
			"BACKEND_STARTING",
			"等待后端监听器完成启动后再重试。",
			false,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterBlockMS}
	case "failed", "error", "stopped":
		return DiscoveryReadinessDegraded,
			"BACKEND_DEGRADED",
			"后端已发布 descriptor，但服务状态尚未恢复。",
			true,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterDegradedMS}
	}

	if snapshot.Instances.RunningCount <= 0 && strings.TrimSpace(snapshot.Instances.ActiveID) == "" {
		return DiscoveryReadinessBlock,
			"PLUGIN_REGISTRATION_PENDING",
			"等待 plugin 完成注册后再开放运行态连接。",
			false,
			DiscoveryRetryHints{Retryable: true, AfterMS: discoveryRetryAfterBlockMS}
	}

	return DiscoveryReadinessReady, "", "", false, DiscoveryRetryHints{}
}

func discoveryBlockReason(servicePhase string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(servicePhase)) {
	case "waiting_plugin":
		return "PLUGIN_REGISTRATION_PENDING", "等待 plugin 完成注册后再开放运行态连接。"
	default:
		return "BACKEND_STARTING", "等待后端监听器完成启动后再重试。"
	}
}

func firstDiscoveryModeIssue(issues map[string]middlewareclient.ModeIssue) (string, string, bool) {
	for _, mode := range []string{"build", "plan"} {
		issue, ok := issues[mode]
		if !ok {
			continue
		}
		code := strings.TrimSpace(issue.Code)
		if code == "" {
			code = strings.ToUpper(mode) + "_NOT_READY"
		}
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			message = "运行模式尚未就绪，请等待后端恢复。"
		}
		return code, message, true
	}
	for mode, issue := range issues {
		code := strings.TrimSpace(issue.Code)
		if code == "" {
			code = strings.ToUpper(strings.TrimSpace(mode)) + "_NOT_READY"
		}
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			message = "运行模式尚未就绪，请等待后端恢复。"
		}
		return code, message, true
	}
	return "", "", false
}

func newDiscoveryDescriptorInvalidError(message string, debugContext map[string]any) error {
	envelope := NewErrorEnvelope("DISCOVERY_DESCRIPTOR_INVALID", strings.TrimSpace(message), "discovery", "runtime_descriptor")
	envelope.Retryable = true
	envelope.UserAction = "请等待后端重新发布合法的 discovery descriptor 后再重试。"
	return envelope.WithDebugContext(debugContext)
}

func newDiscoveryDescriptorExpiredError(observedAt, expiresAt, now time.Time) error {
	envelope := NewErrorEnvelope("DISCOVERY_DESCRIPTOR_EXPIRED", "discovery descriptor 已过期", "discovery", "runtime_descriptor")
	envelope.Retryable = true
	envelope.UserAction = "请等待后端重新发布最新的 discovery descriptor 后再重试。"
	return envelope.WithDebugContext(map[string]any{
		"observed_at": observedAt.Format(time.RFC3339Nano),
		"expires_at":  expiresAt.Format(time.RFC3339Nano),
		"now":         now.Format(time.RFC3339Nano),
	})
}

func normalizeDescriptorSchemes(protocol string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "http":
		return "http", "ws", nil
	case "https":
		return "https", "wss", nil
	case "ws", "websocket":
		return "http", "ws", nil
	case "wss":
		return "https", "wss", nil
	default:
		return "", "", fmt.Errorf("discovery descriptor 协议无效: %s", strings.TrimSpace(protocol))
	}
}
