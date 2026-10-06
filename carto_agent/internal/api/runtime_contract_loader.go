package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/protocol"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolcontract"
)

var (
	errAssetCatalogReadTimeout      = errors.New("asset catalog read timed out")
	errExecutionManifestReadTimeout = errors.New("execution manifest read timed out")
	errRuntimeSnapshotReadTimeout   = errors.New("runtime snapshot read timed out")
	errServiceRegistryReadTimeout   = errors.New("service registry read timed out")
)

var (
	assetCatalogReadTimeout      = 1500 * time.Millisecond
	executionManifestReadTimeout = 1500 * time.Millisecond
	runtimeSnapshotReadTimeout   = 1500 * time.Millisecond
	serviceRegistryReadTimeout   = 1000 * time.Millisecond
)

type asyncResult[T any] struct {
	value T
	err   error
}

func runWithTimeout[T any](
	ctx context.Context,
	timeout time.Duration,
	timeoutErr error,
	fn func(context.Context) (T, error),
) (T, error) {
	var zero T
	if fn == nil {
		return zero, nil
	}

	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if timeout <= 0 {
		return fn(baseCtx)
	}

	callCtx, cancel := context.WithTimeout(baseCtx, timeout)
	defer cancel()

	resultCh := make(chan asyncResult[T], 1)
	go func() {
		value, err := fn(callCtx)
		select {
		case resultCh <- asyncResult[T]{value: value, err: err}:
		case <-callCtx.Done():
		}
	}()

	select {
	case result := <-resultCh:
		return result.value, result.err
	case <-callCtx.Done():
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) && timeoutErr != nil {
			return zero, timeoutErr
		}
		return zero, callCtx.Err()
	}
}

func (a *API) loadToolCatalogView(ctx context.Context, scope string) (control.ToolCatalogView, error) {
	if a == nil || a.assetCatalogService == nil {
		return control.ToolCatalogView{}, nil
	}
	return runWithTimeout(
		ctx,
		assetCatalogReadTimeout,
		errAssetCatalogReadTimeout,
		func(callCtx context.Context) (control.ToolCatalogView, error) {
			return a.assetCatalogService.GetToolCatalog(callCtx, scope)
		},
	)
}

func (a *API) loadExecutionManifestContract(ctx context.Context) ([]toolcontract.ExecutionManifestEntry, error) {
	return runWithTimeout(
		ctx,
		executionManifestReadTimeout,
		errExecutionManifestReadTimeout,
		func(context.Context) ([]toolcontract.ExecutionManifestEntry, error) {
			return loadExecutionManifestContract()
		},
	)
}

func (a *API) loadRuntimeSnapshotView(ctx context.Context) (middlewareclient.RuntimeSnapshot, error) {
	return runWithTimeout(
		ctx,
		runtimeSnapshotReadTimeout,
		errRuntimeSnapshotReadTimeout,
		func(callCtx context.Context) (middlewareclient.RuntimeSnapshot, error) {
			if a == nil || a.runtimeCoordinator == nil {
				return middlewareclient.RuntimeSnapshot{}, nil
			}
			return a.runtimeCoordinator.Snapshot(callCtx), nil
		},
	)
}

func (a *API) loadRuntimeServiceEntry(ctx context.Context, serviceName string) (discovery.ServicePortEntry, error) {
	return runWithTimeout(
		ctx,
		serviceRegistryReadTimeout,
		errServiceRegistryReadTimeout,
		func(context.Context) (discovery.ServicePortEntry, error) {
			if a == nil || a.serviceRegistry == nil {
				return discovery.ServicePortEntry{}, nil
			}
			return a.serviceRegistry.GetPort(serviceName)
		},
	)
}

func runtimeDescriptorFallbackEntry(r *http.Request) discovery.ServicePortEntry {
	port := 0
	host := "127.0.0.1"
	if r != nil {
		candidateHost := strings.TrimSpace(r.URL.Host)
		if candidateHost == "" {
			candidateHost = strings.TrimSpace(r.Host)
		}
		if candidateHost != "" {
			parsedHost, parsedPort, err := net.SplitHostPort(candidateHost)
			if err == nil {
				if parsedHost = strings.TrimSpace(parsedHost); parsedHost != "" {
					host = parsedHost
				}
				if value, convErr := strconv.Atoi(strings.TrimSpace(parsedPort)); convErr == nil {
					port = value
				}
			} else {
				host = candidateHost
			}
		}
	}
	return discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        host,
		Port:        port,
		Protocol:    "http",
		Status:      "running",
	}
}

func fallbackRuntimeDescriptor(
	now time.Time,
	entry discovery.ServicePortEntry,
	reasonCode string,
	reasonMessage string,
	snapshot middlewareclient.RuntimeSnapshot,
	executionManifest []toolcontract.ExecutionManifestEntry,
	publicToolCatalog []toolcatalog.Item,
) control.DiscoveryDescriptor {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	serviceName := strings.TrimSpace(entry.ServiceName)
	if serviceName == "" {
		serviceName = discovery.ServiceNameAgentQGISBridge
	}
	host := strings.TrimSpace(entry.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	if entry.Port <= 0 || entry.Port > 65535 {
		entry.Port = 3000
	}
	if strings.TrimSpace(reasonCode) == "" {
		reasonCode = "DISCOVERY_DESCRIPTOR_DEGRADED"
	}
	if strings.TrimSpace(reasonMessage) == "" {
		reasonMessage = "运行时 descriptor 降级发布，请等待后端恢复。"
	}
	observedAt := snapshot.Timestamp.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	return control.DiscoveryDescriptor{
		Version:           protocol.ContractVersionV1,
		Revision:          snapshot.Version,
		ServiceName:       serviceName,
		Port:              entry.Port,
		BackendURL:        "http://" + net.JoinHostPort(host, strconv.Itoa(entry.Port)),
		WSURL:             "ws://" + net.JoinHostPort(host, strconv.Itoa(entry.Port)) + "/ws",
		Readiness:         control.DiscoveryReadinessDegraded,
		ReasonCode:        strings.TrimSpace(reasonCode),
		ReasonMessage:     strings.TrimSpace(reasonMessage),
		Degraded:          true,
		RetryHints:        control.DiscoveryRetryHints{Retryable: true, AfterMS: 2000},
		DiscoveryRevision: control.ResolveDiscoveryRevision(snapshot),
		RuntimeRevision:   strings.TrimSpace(snapshot.RuntimeRevision),
		ActiveConfig:      strings.TrimSpace(snapshot.ActiveConfig),
		ActiveInstanceID:  strings.TrimSpace(snapshot.Instances.ActiveID),
		ServicePhase:      strings.TrimSpace(snapshot.ServicePhase),
		RestartSequence:   snapshot.Version,
		ExecutionManifest: append([]toolcontract.ExecutionManifestEntry(nil), executionManifest...),
		ToolCatalog:       append([]toolcatalog.Item(nil), publicToolCatalog...),
		ObservedAt:        observedAt.Format(time.RFC3339Nano),
		ExpiresAt:         observedAt.Add(runtimeDescriptorTTL).Format(time.RFC3339Nano),
	}
}

func fallbackRuntimeSnapshot(
	now time.Time,
	snapshot middlewareclient.RuntimeSnapshot,
	reasonCode string,
	reasonMessage string,
) middlewareclient.RuntimeSnapshot {
	now = now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	reasonCode = strings.TrimSpace(reasonCode)
	if reasonCode == "" {
		reasonCode = "RUNTIME_SNAPSHOT_TIMEOUT"
	}
	reasonMessage = strings.TrimSpace(reasonMessage)
	if reasonMessage == "" {
		reasonMessage = "运行时快照读取超时，当前返回降级快照。"
	}

	snapshot.Timestamp = now
	snapshot.Stale = true
	if strings.TrimSpace(snapshot.ServicePhase) == "" || strings.EqualFold(strings.TrimSpace(snapshot.ServicePhase), "ready") {
		snapshot.ServicePhase = "degraded"
	}
	if snapshot.Services == nil {
		snapshot.Services = map[string]middlewareclient.ServiceStatus{}
	}
	if _, exists := snapshot.Services["agent"]; !exists {
		snapshot.Services["agent"] = middlewareclient.ServiceStatus{
			ID:      "agent",
			Status:  "running",
			Source:  "sidecar",
			Enabled: true,
		}
	}
	if snapshot.Instances.Items == nil {
		snapshot.Instances.Items = []*discovery.QGISInstance{}
	}
	if snapshot.ModeIssues == nil {
		snapshot.ModeIssues = map[string]middlewareclient.ModeIssue{}
	}
	snapshot.ModeIssues["runtime"] = middlewareclient.ModeIssue{
		Code:    reasonCode,
		Message: reasonMessage,
	}
	if snapshot.ModeReadiness == nil {
		snapshot.ModeReadiness = map[string]middlewareclient.ModeReadiness{}
	}
	snapshot.ModeReadiness["runtime"] = middlewareclient.ModeReadiness{
		Enabled: true,
		Ready:   false,
		Code:    reasonCode,
		Message: reasonMessage,
	}
	snapshot.Services = control.NormalizePublicRuntimeServices(snapshot.Services)
	if qgisService, exists := snapshot.Services[skill.RuntimeIDQGIS]; exists && snapshot.Instances.RunningCount <= 0 {
		qgisService.Status = "stopped"
		qgisService.InstanceCount = snapshot.Instances.InstanceCount
		qgisService.RunningCount = snapshot.Instances.RunningCount
		snapshot.Services[skill.RuntimeIDQGIS] = qgisService
	}
	return snapshot
}

func degradedPublicCatalogContractForStartup() toolcatalog.Contract {
	return degradedPublicCatalogContract()
}
