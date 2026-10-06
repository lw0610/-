package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/logger"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
)

const (
	errCodeInstanceRegistryUnavailable = "INSTANCE_REGISTRY_UNAVAILABLE"
	errCodeInstanceNotFound            = "INSTANCE_NOT_FOUND"
	errCodeInstanceRegisterInvalid     = "INSTANCE_REGISTER_INVALID"
	errCodeInstanceRegisterFailed      = "INSTANCE_REGISTER_FAILED"
	errCodeInstanceCloseInvalid        = "INSTANCE_CLOSE_INVALID"
	errCodeInstanceCloseUnavailable    = "INSTANCE_CLOSE_UNAVAILABLE"
	errCodeInstanceCloseFailed         = "INSTANCE_CLOSE_FAILED"
	errCodeInstanceToolInvokeInvalid   = "INSTANCE_TOOL_INVOKE_INVALID"
	errCodeInstanceToolInvokerMissing  = "INSTANCE_TOOL_INVOKER_UNAVAILABLE"
	errCodeInstanceToolInvokeForbidden = "INSTANCE_TOOL_INVOKE_FORBIDDEN"
	errCodeInstanceToolInvokeFailed    = "INSTANCE_TOOL_INVOKE_FAILED"
)

type toolAccessPolicyResolver interface {
	ResolveToolAccessPolicy(ctx context.Context, toolName string) (string, []string, bool, error)
}

type instanceToolInvokeDeniedError struct {
	toolName   string
	visibility string
	scopes     []string
	endpoint   string
	instanceID string
}

func (e *instanceToolInvokeDeniedError) Error() string {
	return "工具调用已被 Go 策略拒绝"
}

// handleListInstances GET /api/instances
func (a *API) handleListInstances(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instances_list") {
		return
	}
	snapshot := a.loadInstanceSnapshot(r.Context())
	Success(w, map[string]any{
		"instances":      snapshot.Instances,
		"active_id":      snapshot.ActiveID,
		"qgis_available": snapshot.QGISAvailable,
		"instance_count": snapshot.InstanceCount,
	})
}

// handleGetCapabilities GET /api/capabilities
func (a *API) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "capabilities") {
		return
	}
	snapshot := a.loadInstanceSnapshot(r.Context())
	Success(w, map[string]any{
		"qgis_available": snapshot.QGISAvailable,
		"instance_count": snapshot.InstanceCount,
	})
}

// handleGetInstance GET /api/instances/{id}
func (a *API) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_get") {
		return
	}
	id := mux.Vars(r)["id"]
	if inst, err := a.loadInstance(r.Context(), id); err == nil {
		Success(w, inst)
		return
	}

	if a.instanceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			errCodeInstanceRegistryUnavailable,
			"实例注册表未初始化",
			"discovery",
			"instance_lookup",
			map[string]any{"dependency": "instance_registry", "operation": "get_instance"},
		), nil)
		return
	}
	inst := a.instanceRegistry.Get(id)
	if inst == nil {
		writeControlPlaneError(w, r, http.StatusNotFound, nil, newRequestEnvelope(
			errCodeInstanceNotFound,
			"实例不存在: "+id,
			"discovery",
			"instance_lookup",
			"请确认实例 ID 是否存在后重试。",
			map[string]any{"instance_id": id, "operation": "get_instance"},
		), nil)
		return
	}
	Success(w, inst)
}

// handleInstanceRegister POST /api/instances/register
func (a *API) handleInstanceRegister(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_register") {
		return
	}
	a.handleInstanceRegisterInternal(w, r)
}

func (a *API) handleInstanceRegisterInternal(w http.ResponseWriter, r *http.Request) {
	if a.instanceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			errCodeInstanceRegistryUnavailable,
			"实例注册表未初始化",
			"discovery",
			"instance_registration",
			map[string]any{"dependency": "instance_registry", "operation": "register_instance"},
		), nil)
		return
	}

	var req discovery.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeControlPlaneError(w, r, http.StatusBadRequest, err, newRequestEnvelope(
			errCodeInstanceRegisterInvalid,
			"请求体解析失败",
			"discovery",
			"instance_registration",
			"请提供合法的实例注册请求体。",
			map[string]any{"operation": "register_instance"},
		), nil)
		return
	}

	req.InstanceID = strings.TrimSpace(req.InstanceID)
	if req.InstanceID == "" || req.PID <= 0 {
		writeControlPlaneError(w, r, http.StatusBadRequest, nil, newRequestEnvelope(
			errCodeInstanceRegisterInvalid,
			"instance_id 和 pid 为必填项",
			"discovery",
			"instance_registration",
			"请补齐 instance_id 与 pid 后重试。",
			map[string]any{"operation": "register_instance"},
		), nil)
		return
	}

	if err := a.instanceRegistry.Register(req); err != nil {
		writeControlPlaneError(w, r, http.StatusInternalServerError, err, newRequestEnvelope(
			errCodeInstanceRegisterFailed,
			"注册失败",
			"discovery",
			"instance_registration",
			"请检查实例注册表状态后重试。",
			map[string]any{"instance_id": req.InstanceID, "operation": "register_instance"},
		), nil)
		return
	}
	Success(w, map[string]string{"instance_id": req.InstanceID})
}

// handleInstanceHeartbeat POST /api/instances/{id}/heartbeat
func (a *API) handleInstanceHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_heartbeat") {
		return
	}
	id := mux.Vars(r)["id"]
	if a.instanceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			errCodeInstanceRegistryUnavailable,
			"实例注册表未初始化",
			"discovery",
			"instance_heartbeat",
			map[string]any{"dependency": "instance_registry", "operation": "heartbeat_instance"},
		), nil)
		return
	}
	if err := a.instanceRegistry.Heartbeat(id); err != nil {
		writeControlPlaneError(w, r, http.StatusNotFound, err, newRequestEnvelope(
			errCodeInstanceNotFound,
			err.Error(),
			"discovery",
			"instance_heartbeat",
			"请确认实例 ID 是否存在后重试。",
			map[string]any{"instance_id": id, "operation": "heartbeat_instance"},
		), nil)
		return
	}
	Success(w, map[string]string{"status": "ok"})
}

// handleInstanceEvent POST /api/instances/{id}/event
func (a *API) handleInstanceEvent(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_event") {
		return
	}
	id := mux.Vars(r)["id"]
	if a.instanceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			errCodeInstanceRegistryUnavailable,
			"实例注册表未初始化",
			"discovery",
			"instance_event",
			map[string]any{"dependency": "instance_registry", "operation": "instance_event"},
		), nil)
		return
	}

	var req discovery.EventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeControlPlaneError(w, r, http.StatusBadRequest, err, newRequestEnvelope(
			errCodeInstanceRegisterInvalid,
			"请求体解析失败",
			"discovery",
			"instance_event",
			"请提供合法的实例事件请求体。",
			map[string]any{"instance_id": id, "operation": "instance_event"},
		), nil)
		return
	}

	if err := a.instanceRegistry.HandleEvent(id, req); err != nil {
		writeControlPlaneError(w, r, http.StatusNotFound, err, newRequestEnvelope(
			errCodeInstanceNotFound,
			err.Error(),
			"discovery",
			"instance_event",
			"请确认实例 ID 是否存在后重试。",
			map[string]any{"instance_id": id, "operation": "instance_event"},
		), nil)
		return
	}
	Success(w, map[string]string{"status": "ok"})
}

// handleInstanceUnregister DELETE /api/instances/{id}
func (a *API) handleInstanceUnregister(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_unregister") {
		return
	}
	id := mux.Vars(r)["id"]
	if a.instanceRegistry == nil {
		writeControlPlaneError(w, r, http.StatusServiceUnavailable, nil, newAvailabilityEnvelope(
			errCodeInstanceRegistryUnavailable,
			"实例注册表未初始化",
			"discovery",
			"instance_unregister",
			map[string]any{"dependency": "instance_registry", "operation": "unregister_instance"},
		), nil)
		return
	}
	if err := a.instanceRegistry.Unregister(id); err != nil {
		writeControlPlaneError(w, r, http.StatusNotFound, err, newRequestEnvelope(
			errCodeInstanceNotFound,
			err.Error(),
			"discovery",
			"instance_unregister",
			"请确认实例 ID 是否存在后重试。",
			map[string]any{"instance_id": id, "operation": "unregister_instance"},
		), nil)
		return
	}
	Success(w, map[string]string{"status": "removed"})
}

// handleCloseInstance POST /api/instances/{id}/close
func (a *API) handleCloseInstance(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_close") {
		return
	}
	instanceID := strings.TrimSpace(mux.Vars(r)["id"])
	if instanceID == "" {
		ErrorWithCode(w, http.StatusBadRequest, errCodeInstanceCloseInvalid, "实例 ID 不能为空")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	instance, err := a.loadInstance(r.Context(), instanceID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			ErrorWithCode(w, http.StatusNotFound, errCodeInstanceNotFound, "实例不存在: "+instanceID)
			return
		}
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceCloseUnavailable, "实例关闭能力未初始化")
		return
	}
	if a.localBridgeProvider == nil {
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceCloseUnavailable, "实例关闭能力未初始化")
		return
	}
	if !a.localBridgeProvider.HasInstanceConnected(instanceID) {
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceCloseUnavailable, "实例桥接连接未就绪")
		return
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), "close_instance", "instance_close", instanceID); err != nil {
		var denied *instanceToolInvokeDeniedError
		if errors.As(err, &denied) {
			ErrorWithCode(w, http.StatusForbidden, errCodeInstanceToolInvokeForbidden, denied.Error())
			return
		}
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例项目操作策略未初始化")
		return
	}

	resp, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, "close_instance", map[string]any{
		"instance_id": instanceID,
		"delay_ms":    200,
	})
	if err != nil {
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceCloseFailed, "关闭实例失败: "+err.Error(), bridgeErrorDetailsFromError(err))
		return
	}
	if resp == nil || !resp.Success {
		errMsg := "关闭实例失败"
		if resp != nil && strings.TrimSpace(resp.Error) != "" {
			errMsg = "关闭实例失败: " + strings.TrimSpace(resp.Error)
		}
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceCloseFailed, errMsg, bridgeErrorDetailsFromInvokeResponse(resp))
		return
	}

	payload := map[string]any{
		"status":       "closing",
		"instance_id":  instanceID,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
		"instance":     instance,
	}
	if activeID := strings.TrimSpace(a.localBridgeProvider.ActiveInstance()); activeID != "" {
		payload["active_id"] = activeID
	}
	Success(w, payload)
}

func (a *API) handleOpenInstanceProject(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_open_project") {
		return
	}
	a.handleInstanceProjectAction(w, r, "open_project")
}

func (a *API) handleCreateInstanceProject(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "instance_create_project") {
		return
	}
	a.handleInstanceProjectAction(w, r, "create_project")
}

func (a *API) handleInstanceProjectAction(w http.ResponseWriter, r *http.Request, toolName string) {
	instanceID := strings.TrimSpace(mux.Vars(r)["id"])
	if instanceID == "" {
		ErrorWithCode(w, http.StatusBadRequest, errCodeInstanceToolInvokeInvalid, "实例 ID 不能为空")
		return
	}
	if a.localBridgeProvider == nil {
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例项目操作能力未初始化")
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ErrorWithCode(w, http.StatusBadRequest, errCodeInstanceToolInvokeInvalid, "请求体解析失败: "+err.Error())
		return
	}
	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		ErrorWithCode(w, http.StatusBadRequest, errCodeInstanceToolInvokeInvalid, "path 不能为空")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	if _, err := a.loadInstance(r.Context(), instanceID); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			ErrorWithCode(w, http.StatusNotFound, errCodeInstanceNotFound, "实例不存在: "+instanceID)
			return
		}
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例项目操作能力未初始化")
		return
	}
	if !a.localBridgeProvider.HasInstanceConnected(instanceID) {
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例桥接连接未就绪")
		return
	}
	if err := a.authorizeInstanceToolInvoke(r.Context(), toolName, "instance_project_action", instanceID); err != nil {
		var denied *instanceToolInvokeDeniedError
		if errors.As(err, &denied) {
			ErrorWithCode(w, http.StatusForbidden, errCodeInstanceToolInvokeForbidden, denied.Error())
			return
		}
		ErrorWithCode(w, http.StatusServiceUnavailable, errCodeInstanceToolInvokerMissing, "实例项目操作策略未初始化")
		return
	}

	resp, err := bridgeprovider.NewComputeToolInvoker(a.localBridgeProvider).InvokeTool(ctx, toolName, map[string]any{
		"instance_id": instanceID,
		"path":        req.Path,
	})
	if err != nil {
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceToolInvokeFailed, "实例项目操作失败: "+err.Error(), bridgeErrorDetailsFromError(err))
		return
	}
	if resp == nil || !resp.Success {
		errMsg := "实例项目操作失败"
		if resp != nil && strings.TrimSpace(resp.Error) != "" {
			errMsg = "实例项目操作失败: " + strings.TrimSpace(resp.Error)
		}
		ErrorWithCodeAndDetails(w, http.StatusBadGateway, errCodeInstanceToolInvokeFailed, errMsg, bridgeErrorDetailsFromInvokeResponse(resp))
		return
	}
	if a.instanceRegistry != nil {
		projectPath := req.Path
		if extracted, ok := extractProjectPathFromToolResult(toolName, resp.Output); ok {
			projectPath = extracted
		}
		_ = a.instanceRegistry.HandleEvent(instanceID, discovery.EventRequest{
			EventType: "project_opened",
			Data:      map[string]interface{}{"project_path": projectPath},
		})
	}

	payload := map[string]any{
		"status":       "ok",
		"instance_id":  instanceID,
		"requested_at": time.Now().UTC().Format(time.RFC3339),
		"result":       resp.Output,
	}
	Success(w, payload)
}

func extractProjectPathFromToolResult(toolName string, result interface{}) (string, bool) {
	switch strings.TrimSpace(toolName) {
	case "open_project", "create_project", "save_project":
	default:
		return "", false
	}

	payload, ok := result.(map[string]interface{})
	if !ok {
		return "", false
	}
	if success, ok := payload["success"].(bool); ok && !success {
		return "", false
	}
	if direct, ok := payload["project_path"].(string); ok && strings.TrimSpace(direct) != "" {
		return strings.TrimSpace(direct), true
	}
	if info, ok := payload["info"].(map[string]interface{}); ok {
		if path, ok := info["file_path"].(string); ok && strings.TrimSpace(path) != "" {
			return strings.TrimSpace(path), true
		}
		if path, ok := info["project_path"].(string); ok && strings.TrimSpace(path) != "" {
			return strings.TrimSpace(path), true
		}
	}
	return "", false
}

func bridgeErrorDetailsFromError(err error) map[string]any {
	if err == nil {
		return nil
	}
	var coded interface {
		ToolErrorCode() string
		ToolProviderDiagnostics() map[string]any
	}
	if !errors.As(err, &coded) {
		return nil
	}
	details := map[string]any{}
	if code := strings.TrimSpace(coded.ToolErrorCode()); code != "" {
		details["error_code"] = code
	}
	if diagnostics := coded.ToolProviderDiagnostics(); len(diagnostics) > 0 {
		details["provider_diagnostics"] = diagnostics
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

func bridgeErrorDetailsFromInvokeResponse(resp *compute.InvokeResponse) map[string]any {
	if resp == nil {
		return nil
	}
	details := map[string]any{}
	if code := strings.TrimSpace(resp.ErrorCode); code != "" {
		details["error_code"] = code
	}
	if len(resp.ProviderDiagnostics) > 0 {
		details["provider_diagnostics"] = resp.ProviderDiagnostics
	}
	if len(details) == 0 {
		return nil
	}
	return details
}

func summarizeInstanceAvailability(instances []*discovery.QGISInstance) (runningCount int, qgisAvailable bool) {
	for _, inst := range instances {
		if inst != nil && inst.Status == "running" {
			runningCount++
		}
	}
	return runningCount, runningCount > 0
}

func (a *API) authorizeInstanceToolInvoke(ctx context.Context, toolName, endpoint, instanceID string) error {
	if a == nil || a.assetCatalogService == nil {
		return nil
	}
	resolver, ok := a.assetCatalogService.(toolAccessPolicyResolver)
	if !ok {
		return nil
	}
	visibility, scopes, known, err := resolver.ResolveToolAccessPolicy(ctx, toolName)
	if err != nil {
		return err
	}
	if !known {
		logger.Warn(
			"tool_invoke_denied tool=%s visibility=unknown scopes= endpoint=%s instance_id=%s",
			strings.TrimSpace(toolName),
			strings.TrimSpace(endpoint),
			strings.TrimSpace(instanceID),
		)
		return &instanceToolInvokeDeniedError{
			toolName:   strings.TrimSpace(toolName),
			visibility: "unknown",
			endpoint:   strings.TrimSpace(endpoint),
			instanceID: strings.TrimSpace(instanceID),
		}
	}
	if strings.EqualFold(strings.TrimSpace(visibility), "public") && hasAuthorizationScope(scopes, "tool.invoke") {
		return nil
	}
	logger.Warn(
		"tool_invoke_denied tool=%s visibility=%s scopes=%s endpoint=%s instance_id=%s",
		strings.TrimSpace(toolName),
		strings.TrimSpace(visibility),
		strings.Join(scopes, ","),
		strings.TrimSpace(endpoint),
		strings.TrimSpace(instanceID),
	)
	return &instanceToolInvokeDeniedError{
		toolName:   strings.TrimSpace(toolName),
		visibility: strings.TrimSpace(visibility),
		scopes:     append([]string(nil), scopes...),
		endpoint:   strings.TrimSpace(endpoint),
		instanceID: strings.TrimSpace(instanceID),
	}
}

func hasAuthorizationScope(scopes []string, required string) bool {
	required = strings.TrimSpace(required)
	if required == "" {
		return false
	}
	for _, scope := range scopes {
		if strings.TrimSpace(scope) == required {
			return true
		}
	}
	return false
}

func (a *API) loadInstanceSnapshot(ctx context.Context) middlewareclient.InstanceSnapshot {
	merged := middlewareclient.InstanceSnapshot{Instances: []*discovery.QGISInstance{}}

	if a.runtimeCoordinator != nil {
		runtimeSnapshot := a.runtimeCoordinator.Snapshot(ctx)
		coordinated := middlewareclient.InstanceSnapshotFromRuntime(runtimeSnapshot)
		merged = middlewareclient.MergeInstanceSnapshots(merged, coordinated)
	}

	if a.instanceRegistry != nil {
		instances := a.instanceRegistry.GetAll()
		runningCount, qgisAvailable := summarizeInstanceAvailability(instances)
		local := middlewareclient.InstanceSnapshot{
			Instances:     instances,
			QGISAvailable: qgisAvailable,
			InstanceCount: len(instances),
			RunningCount:  runningCount,
		}
		merged = middlewareclient.MergeInstanceSnapshots(merged, local)
	}
	if merged.Instances == nil {
		merged.Instances = []*discovery.QGISInstance{}
	}
	return merged
}

func (a *API) loadInstance(ctx context.Context, instanceID string) (*discovery.QGISInstance, error) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, errors.New("empty instance id")
	}
	if a.instanceRegistry == nil {
		return nil, errors.New("instance registry unavailable")
	}
	instance := a.instanceRegistry.Get(instanceID)
	if instance == nil {
		return nil, errors.New("instance not found")
	}
	return instance, nil
}
