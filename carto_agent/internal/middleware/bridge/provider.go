package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/logger"
	"github.com/autocarto/carto_agent/internal/security"
	"github.com/autocarto/carto_agent/internal/shared/toolutils"
	"github.com/autocarto/carto_agent/internal/toolcontract"
)

const (
	msgTypeInstanceConnect      = "instance_connect"
	msgTypeInstanceHeartbeat    = "instance_heartbeat"
	msgTypeInstanceProjectEvent = "instance_project_event"
	msgTypeInstanceDisconnect   = "instance_disconnect"
	msgTypeToolInvoke           = "tool_invoke"
	msgTypeToolCancel           = "tool_cancel"
	msgTypeToolResult           = "tool_result"
	msgTypeToolListRequest      = "tool_list_request"
	msgTypeToolListResult       = "tool_list_result"
	msgTypeToolEngineContract   = "tool_engine_contract"
	bridgeProtocolVersion       = 1

	defaultToolInvokeTimeout = 5 * time.Minute
	pluginPongWait           = 90 * time.Second
	pluginPingInterval       = 30 * time.Second
	pluginPingWriteTimeout   = 10 * time.Second
	disconnectGracePeriod    = 1 * time.Second
)

type wsMessage struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

type pluginConnection struct {
	conn        *websocket.Conn
	writeMu     sync.Mutex
	instanceID  string
	pid         int
	qgisVersion string
	pythonPath  string
	pluginPath  string
	skillsDir   string
	projectPath string
	tools       []ToolDefinition
}

type pendingDisconnect struct {
	timer *time.Timer
}

type InstanceRuntime struct {
	InstanceID  string
	PythonPath  string
	PluginPath  string
	ProjectPath string
}

type ToolContractProvider func() ([]toolcontract.ExecutionManifestEntry, error)

// Provider 实现基于 QGIS 插件反向连接的 Bridge Provider。
type Provider struct {
	id               string
	upgrader         websocket.Upgrader
	instanceRegistry *discovery.InstanceRegistry
	invokeTimeout    time.Duration
	defaultSource    string

	mu                 sync.RWMutex
	pluginClients      map[*websocket.Conn]*pluginConnection
	instancePlugins    map[string]*pluginConnection
	pendingDisconnects map[string]*pendingDisconnect
	activeInstance     string
	pendingRequests    sync.Map

	onCatalogChanged     func()
	dynamicToolRegistry  DynamicToolRegistry
	toolContractProvider ToolContractProvider
}

type DynamicToolRegistry interface {
	RegisterInstanceTools(instanceID string, defs []ToolDefinition)
	UnregisterInstanceTools(instanceID string, toolNames []string)
}

// NewProvider 创建 Bridge Provider。
func NewProvider(providerID string, instanceRegistry *discovery.InstanceRegistry) *Provider {
	return &Provider{
		id: providerID,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return security.IsAllowedLocalWebSocketRequest(r) },
		},
		instanceRegistry:   instanceRegistry,
		pluginClients:      make(map[*websocket.Conn]*pluginConnection),
		instancePlugins:    make(map[string]*pluginConnection),
		pendingDisconnects: make(map[string]*pendingDisconnect),
		invokeTimeout:      defaultToolInvokeTimeout,
		defaultSource:      discovery.SourceReverseWS,
	}
}

func (p *Provider) ID() string {
	return p.id
}

// SetInvokeTimeout 设置工具调用默认超时。
func (p *Provider) SetInvokeTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	p.mu.Lock()
	p.invokeTimeout = timeout
	p.mu.Unlock()
}

// SetCatalogChangedHook 设置工具目录变更回调。
func (p *Provider) SetCatalogChangedHook(hook func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onCatalogChanged = hook
}

func (p *Provider) SetDynamicToolRegistry(registry DynamicToolRegistry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dynamicToolRegistry = registry
}

func (p *Provider) SetToolContractProvider(provider ToolContractProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toolContractProvider = provider
}

func (p *Provider) DynamicToolRegistry() DynamicToolRegistry {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dynamicToolRegistry
}

func (p *Provider) SetDefaultInstanceSource(source string) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return
	}
	p.mu.Lock()
	p.defaultSource = trimmed
	p.mu.Unlock()
}

// HandleWebSocket 处理插件反向连接。
func (p *Provider) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := p.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("middleware bridge websocket 升级失败: %v", err)
		return
	}

	state := &pluginConnection{conn: conn}
	p.mu.Lock()
	p.pluginClients[conn] = state
	p.mu.Unlock()

	logger.Info("middleware bridge 插件连接建立: %s", conn.RemoteAddr())

	_ = conn.SetReadDeadline(time.Now().Add(pluginPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pluginPongWait))
	})

	stopPing := make(chan struct{})
	go p.keepalivePluginConnection(state, stopPing)
	defer close(stopPing)
	defer p.cleanupPluginConnection(state, "connection closed")

	for {
		_, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			logger.Debug("middleware bridge 读取消息失败: %v", readErr)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pluginPongWait))
		p.handlePluginMessage(state, payload)
	}
}

func (p *Provider) keepalivePluginConnection(state *pluginConnection, stop <-chan struct{}) {
	if state == nil || state.conn == nil {
		return
	}

	ticker := time.NewTicker(pluginPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			state.writeMu.Lock()
			err := state.conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(pluginPingWriteTimeout))
			state.writeMu.Unlock()
			if err != nil {
				logger.Debug("middleware bridge keepalive ping 失败: %v", err)
				_ = state.conn.Close()
				return
			}
		}
	}
}

// ListTools 返回当前可用交互工具列表。
func (p *Provider) ListTools(ctx context.Context) ([]ToolDefinition, error) {
	_ = ctx
	return nil, nil
}

// InvokeTool 调用交互工具。
func (p *Provider) InvokeTool(ctx context.Context, toolName string, args map[string]any) (*InvokeResult, error) {
	targetID, plugin, err := p.resolveTargetPlugin(args)
	if err != nil {
		return nil, err
	}
	params := cloneToolParams(args)
	if targetID != "" {
		params["target_instance_id"] = targetID
	}
	requestID := uuid.NewString()
	pending := &pendingRequest{ch: make(chan pendingResponse, 1)}
	p.pendingRequests.Store(requestID, pending)
	defer p.pendingRequests.Delete(requestID)

	if err := p.sendPluginMessage(plugin, wsMessage{Type: msgTypeToolInvoke, Data: map[string]any{
		"request_id": requestID,
		"tool_name":  strings.TrimSpace(toolName),
		"params":     params,
	}}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		p.sendBestEffortToolCancel(plugin, requestID, toolName, targetID)
		return nil, ctx.Err()
	case result := <-pending.ch:
		if result.invoke == nil {
			return nil, fmt.Errorf("plugin tool invoke returned nil result")
		}
		return result.invoke, nil
	}
}

func (p *Provider) sendBestEffortToolCancel(plugin *pluginConnection, requestID, toolName, targetID string) {
	if p == nil || plugin == nil {
		return
	}
	data := map[string]any{
		"request_id": strings.TrimSpace(requestID),
		"tool_name":  strings.TrimSpace(toolName),
	}
	if strings.TrimSpace(targetID) != "" {
		data["target_instance_id"] = strings.TrimSpace(targetID)
	}
	if err := p.sendPluginMessage(plugin, wsMessage{Type: msgTypeToolCancel, Data: data}); err != nil {
		logger.Debug("middleware bridge 发送 tool_cancel 失败: request_id=%s tool=%s err=%v", strings.TrimSpace(requestID), strings.TrimSpace(toolName), err)
	}
}

func (p *Provider) ListInstanceTools(ctx context.Context, instanceID string) ([]map[string]any, error) {
	_ = ctx
	_ = instanceID
	return nil, nil
}

func (p *Provider) ListPrompts(ctx context.Context) ([]PromptDefinition, error) {
	_ = ctx
	return nil, nil
}

func (p *Provider) GetPrompt(ctx context.Context, promptName string, args map[string]string) (*PromptResult, error) {
	_ = ctx
	_ = promptName
	_ = args
	return nil, fmt.Errorf("bridge provider does not support prompts")
}

// Close 关闭所有插件连接。
func (p *Provider) Close() error {
	p.mu.Lock()
	conns := make([]*websocket.Conn, 0, len(p.pluginClients))
	instanceIDs := make([]string, 0, len(p.instancePlugins)+len(p.pendingDisconnects))
	seen := make(map[string]struct{}, len(p.instancePlugins)+len(p.pendingDisconnects))
	for conn := range p.pluginClients {
		conns = append(conns, conn)
	}
	for instanceID := range p.instancePlugins {
		trimmedID := strings.TrimSpace(instanceID)
		if trimmedID == "" {
			continue
		}
		if _, ok := seen[trimmedID]; ok {
			continue
		}
		seen[trimmedID] = struct{}{}
		instanceIDs = append(instanceIDs, trimmedID)
	}
	for instanceID, pending := range p.pendingDisconnects {
		if pending != nil && pending.timer != nil {
			pending.timer.Stop()
		}
		trimmedID := strings.TrimSpace(instanceID)
		if trimmedID == "" {
			continue
		}
		if _, ok := seen[trimmedID]; ok {
			continue
		}
		seen[trimmedID] = struct{}{}
		instanceIDs = append(instanceIDs, trimmedID)
	}
	p.pluginClients = make(map[*websocket.Conn]*pluginConnection)
	p.instancePlugins = make(map[string]*pluginConnection)
	p.pendingDisconnects = make(map[string]*pendingDisconnect)
	p.activeInstance = ""
	p.mu.Unlock()

	for _, conn := range conns {
		_ = conn.Close()
	}
	for _, instanceID := range instanceIDs {
		if p.instanceRegistry != nil {
			if err := p.instanceRegistry.Unregister(instanceID); err != nil {
				logger.Debug("middleware bridge provider 关闭时注销实例失败: %v", err)
			}
		}
	}
	return nil
}

// InstanceRegistry 返回实例注册表。
func (p *Provider) InstanceRegistry() *discovery.InstanceRegistry {
	return p.instanceRegistry
}

// SetActiveInstance 切换活动实例。
func (p *Provider) SetActiveInstance(instanceID string) {
	normalized := strings.TrimSpace(instanceID)

	p.mu.Lock()
	changed := p.activeInstance != normalized
	p.activeInstance = normalized
	p.mu.Unlock()

	if changed {
		p.notifyCatalogChanged()
	}
}

// ActiveInstance 返回当前活动实例 ID。
func (p *Provider) ActiveInstance() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return strings.TrimSpace(p.activeInstance)
}

func (p *Provider) ProviderID() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return strings.TrimSpace(p.id)
}

// HasInstanceConnected 返回指定实例是否存在有效桥接连接。
func (p *Provider) HasInstanceConnected(instanceID string) bool {
	normalized := strings.TrimSpace(instanceID)
	if normalized == "" {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.instancePlugins[normalized] != nil
}

func (p *Provider) ConnectedInstanceIDs() []string {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.sortedConnectedInstanceIDsLocked()
}

// PluginPythonPath 返回当前可用实例上报的 Python 解释器路径。
func (p *Provider) PluginPythonPath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeID := strings.TrimSpace(p.activeInstance)
	if activeID != "" {
		if active := p.instancePlugins[activeID]; active != nil {
			if pythonPath := strings.TrimSpace(active.pythonPath); pythonPath != "" {
				return pythonPath
			}
		}
	}

	for _, state := range p.instancePlugins {
		if state == nil {
			continue
		}
		if pythonPath := strings.TrimSpace(state.pythonPath); pythonPath != "" {
			return pythonPath
		}
	}
	return ""
}

// PluginPath 返回当前可用实例上报的插件目录路径。
func (p *Provider) PluginPath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeID := strings.TrimSpace(p.activeInstance)
	if activeID != "" {
		if active := p.instancePlugins[activeID]; active != nil {
			if pluginPath := strings.TrimSpace(active.pluginPath); pluginPath != "" {
				return pluginPath
			}
		}
	}

	for _, state := range p.instancePlugins {
		if state == nil {
			continue
		}
		if pluginPath := strings.TrimSpace(state.pluginPath); pluginPath != "" {
			return pluginPath
		}
	}
	return ""
}

func (p *Provider) PluginSkillsDir() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeID := strings.TrimSpace(p.activeInstance)
	if activeID != "" {
		if active := p.instancePlugins[activeID]; active != nil {
			if skillsDir := strings.TrimSpace(active.skillsDir); skillsDir != "" {
				return skillsDir
			}
		}
	}

	for _, state := range p.instancePlugins {
		if state == nil {
			continue
		}
		if skillsDir := strings.TrimSpace(state.skillsDir); skillsDir != "" {
			return skillsDir
		}
	}
	return ""
}

func (p *Provider) handlePluginMessage(state *pluginConnection, payload []byte) {
	var msg wsMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		logger.Warn("middleware bridge 解析插件消息失败: %v", err)
		return
	}

	switch msg.Type {
	case msgTypeInstanceConnect:
		p.handleInstanceConnect(state, msg.Data)
	case msgTypeInstanceHeartbeat:
		p.handleHeartbeat(msg.Data)
	case msgTypeInstanceProjectEvent:
		p.handleProjectEvent(msg.Data)
	case msgTypeInstanceDisconnect:
		p.handleInstanceDisconnect(msg.Data)
	case msgTypeToolResult:
		p.handleToolResult(msg.Data)
	case msgTypeToolListResult:
		p.handleToolListResult(msg.Data)
	default:
		logger.Debug("middleware bridge 忽略未知消息: %s", msg.Type)
	}
}

func (p *Provider) cleanupPluginConnection(state *pluginConnection, reason string) {
	if state == nil {
		return
	}
	instanceID := state.instanceID
	nextActiveID := ""
	shouldScheduleDisconnect := false

	p.mu.Lock()
	delete(p.pluginClients, state.conn)
	if instanceID != "" {
		if current, ok := p.instancePlugins[instanceID]; ok && current == state {
			delete(p.instancePlugins, instanceID)
			shouldScheduleDisconnect = true
		}
		if p.activeInstance == instanceID {
			nextActiveID = p.pickNextRunningInstanceLocked(instanceID)
			p.activeInstance = nextActiveID
		}
	}
	p.mu.Unlock()

	if instanceID != "" && shouldScheduleDisconnect {
		p.schedulePendingDisconnect(instanceID, reason)
		p.notifyCatalogChanged()
	}

	if state.conn != nil {
		_ = state.conn.Close()
	}
	if instanceID != "" && nextActiveID != "" {
		logger.Info("middleware bridge 活跃实例已自动切换: from=%s to=%s reason=%s", instanceID, nextActiveID, reason)
	}
	logger.Info("middleware bridge 插件连接已清理: instance=%s reason=%s", instanceID, reason)
}

func (p *Provider) handleInstanceConnect(state *pluginConnection, data map[string]any) {
	instanceID, _ := data["instance_id"].(string)
	if strings.TrimSpace(instanceID) == "" {
		logger.Warn("middleware bridge 实例握手缺少 instance_id")
		return
	}
	instanceID = strings.TrimSpace(instanceID)

	pid := toolutils.ToInt(data["pid"])
	qgisVersion, _ := data["qgis_version"].(string)
	pythonPath, _ := data["python_path"].(string)
	pluginPath, _ := data["plugin_path"].(string)
	skillsDir, _ := data["skills_dir"].(string)
	projectPath, _ := data["project_path"].(string)
	machineFingerprint, _ := data["machine_fingerprint"].(string)
	startTime, _ := data["start_time"].(string)
	if startTime == "" {
		if unixTs := toolutils.ToInt64(data["start_time"]); unixTs > 0 {
			startTime = time.Unix(unixTs, 0).Format(time.RFC3339)
		}
	}
	source, _ := data["source"].(string)
	if strings.TrimSpace(source) == "" {
		p.mu.RLock()
		source = strings.TrimSpace(p.defaultSource)
		p.mu.RUnlock()
	}
	if strings.TrimSpace(source) == "" {
		source = discovery.SourceReverseWS
	}

	state.instanceID = instanceID
	state.pid = pid
	state.qgisVersion = qgisVersion
	state.pythonPath = strings.TrimSpace(pythonPath)
	state.pluginPath = strings.TrimSpace(pluginPath)
	state.skillsDir = strings.TrimSpace(skillsDir)
	state.projectPath = projectPath

	p.mu.Lock()
	p.cancelPendingDisconnectLocked(instanceID)
	if prev, ok := p.instancePlugins[instanceID]; ok && prev != state && prev.conn != nil {
		_ = prev.conn.Close()
	}
	p.instancePlugins[instanceID] = state
	if p.activeInstance == "" {
		p.activeInstance = instanceID
	}
	p.mu.Unlock()

	if p.instanceRegistry != nil {
		_ = p.instanceRegistry.Register(discovery.RegisterRequest{
			InstanceID:         instanceID,
			MachineFingerprint: machineFingerprint,
			StartTime:          startTime,
			PID:                pid,
			QGISVersion:        qgisVersion,
			ProjectPath:        projectPath,
			Source:             source,
		})
	}

	logger.Info("middleware bridge 实例已连接: instance=%s", instanceID)
	p.sendToolEngineContract(state)
	p.notifyCatalogChanged()
}

func (p *Provider) sendToolEngineContract(state *pluginConnection) {
	if p == nil || state == nil {
		return
	}
	p.mu.RLock()
	provider := p.toolContractProvider
	p.mu.RUnlock()
	if provider == nil {
		return
	}
	executionManifest, err := provider()
	if err != nil {
		logger.Warn("middleware bridge 下发 tool engine contract 失败: %v", err)
		return
	}
	if len(executionManifest) == 0 {
		logger.Warn("middleware bridge 下发 tool engine contract 为空")
	}
	if err := p.sendPluginMessage(state, wsMessage{
		Type: msgTypeToolEngineContract,
		Data: map[string]any{
			"execution_manifest": executionManifest,
			"tool_catalog":       []any{},
		},
	}); err != nil {
		logger.Warn("middleware bridge 发送 tool engine contract 失败: %v", err)
	}
}

func (p *Provider) handleHeartbeat(data map[string]any) {
	instanceID, _ := data["instance_id"].(string)
	if strings.TrimSpace(instanceID) == "" || p.instanceRegistry == nil {
		return
	}
	if err := p.instanceRegistry.Heartbeat(strings.TrimSpace(instanceID)); err != nil {
		logger.Debug("middleware bridge 实例心跳更新失败: %v", err)
	}
}

func (p *Provider) handleProjectEvent(data map[string]any) {
	instanceID, _ := data["instance_id"].(string)
	eventType, _ := data["event_type"].(string)
	if strings.TrimSpace(instanceID) == "" || strings.TrimSpace(eventType) == "" || p.instanceRegistry == nil {
		return
	}
	eventData := map[string]any{}
	if raw, ok := data["data"].(map[string]any); ok {
		eventData = raw
	}
	_ = p.instanceRegistry.HandleEvent(strings.TrimSpace(instanceID), discovery.EventRequest{
		EventType: strings.TrimSpace(eventType),
		Data:      eventData,
	})
	p.notifyCatalogChanged()
}

func (p *Provider) handleInstanceDisconnect(data map[string]any) {
	instanceID, _ := data["instance_id"].(string)
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}

	nextActiveID := ""
	p.mu.Lock()
	delete(p.instancePlugins, instanceID)
	if p.activeInstance == instanceID {
		nextActiveID = p.pickNextRunningInstanceLocked(instanceID)
		p.activeInstance = nextActiveID
	}
	p.mu.Unlock()

	p.schedulePendingDisconnect(instanceID, msgTypeInstanceDisconnect)
	p.notifyCatalogChanged()
	if nextActiveID != "" {
		logger.Info("middleware bridge 活跃实例已自动切换: from=%s to=%s reason=instance_disconnect", instanceID, nextActiveID)
	}
}

func (p *Provider) schedulePendingDisconnect(instanceID string, reason string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}

	p.mu.Lock()
	if existing := p.pendingDisconnects[instanceID]; existing != nil && existing.timer != nil {
		existing.timer.Stop()
	}
	pending := &pendingDisconnect{}
	pending.timer = time.AfterFunc(disconnectGracePeriod, func() {
		p.finalizePendingDisconnect(instanceID, pending, reason)
	})
	p.pendingDisconnects[instanceID] = pending
	p.mu.Unlock()
}

func (p *Provider) finalizePendingDisconnect(instanceID string, pending *pendingDisconnect, reason string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}

	p.mu.Lock()
	current := p.pendingDisconnects[instanceID]
	if current == nil || current != pending {
		p.mu.Unlock()
		return
	}
	delete(p.pendingDisconnects, instanceID)
	p.mu.Unlock()

	if p.instanceRegistry != nil {
		if err := p.instanceRegistry.Unregister(instanceID); err != nil {
			logger.Debug("middleware bridge 宽限期注销实例失败: %v", err)
		}
	}
	p.notifyCatalogChanged()
	logger.Info("middleware bridge 实例断连宽限期到期，已注销: instance=%s reason=%s", instanceID, reason)
}

func (p *Provider) cancelPendingDisconnectLocked(instanceID string) {
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return
	}
	pending := p.pendingDisconnects[instanceID]
	if pending == nil {
		return
	}
	delete(p.pendingDisconnects, instanceID)
	if pending.timer != nil {
		pending.timer.Stop()
	}
}

func (p *Provider) ResolveInstanceRuntime(args map[string]any) (*InstanceRuntime, error) {
	targetID, plugin, err := p.resolveTargetPlugin(args)
	if err != nil {
		return nil, err
	}
	return &InstanceRuntime{
		InstanceID:  targetID,
		PythonPath:  strings.TrimSpace(plugin.pythonPath),
		PluginPath:  strings.TrimSpace(plugin.pluginPath),
		ProjectPath: strings.TrimSpace(plugin.projectPath),
	}, nil
}

func cloneToolParams(args map[string]any) map[string]any {
	if len(args) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(args))
	for key, value := range args {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || trimmed == "instance_id" || trimmed == "target_instance_id" {
			continue
		}
		out[trimmed] = value
	}
	return out
}

func (p *Provider) handleToolResult(data map[string]any) {
	requestID, _ := data["request_id"].(string)
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	rawPending, ok := p.pendingRequests.Load(requestID)
	if !ok {
		return
	}
	pending, _ := rawPending.(*pendingRequest)
	if pending == nil {
		return
	}
	result := &InvokeResult{
		Success:             toolutils.ToBool(data["success"]),
		Output:              data["output"],
		Error:               strings.TrimSpace(toolutils.ToString(data["error"])),
		ErrorCode:           strings.TrimSpace(toolutils.ToString(data["error_code"])),
		OutputLayers:        parseOutputLayers(data["output_layers"]),
		Status:              strings.TrimSpace(toolutils.ToString(data["status"])),
		TerminalState:       strings.TrimSpace(toolutils.ToString(data["terminal_state"])),
		Progress:            toFloat64(data["progress"]),
		Message:             strings.TrimSpace(toolutils.ToString(data["message"])),
		ProviderDiagnostics: parseDiagnosticsMap(data["provider_diagnostics"]),
	}
	select {
	case pending.ch <- pendingResponse{invoke: result}:
	default:
	}
}

func (p *Provider) handleToolListResult(data map[string]any) {
	requestID, _ := data["request_id"].(string)
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	rawPending, ok := p.pendingRequests.Load(requestID)
	if !ok {
		return
	}
	pending, _ := rawPending.(*pendingRequest)
	if pending == nil {
		return
	}
	select {
	case pending.ch <- pendingResponse{tools: normalizeRawTools(data["tools"])}:
	default:
	}
}

func normalizeRawTools(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	tools := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if tool, ok := item.(map[string]any); ok {
			tools = append(tools, toolutils.DeepCopyMap(tool))
		}
	}
	return tools
}

func parseOutputLayers(raw any) []OutputLayer {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	layers := make([]OutputLayer, 0, len(items))
	for _, item := range items {
		layer, ok := item.(map[string]any)
		if !ok {
			continue
		}
		path := strings.TrimSpace(toolutils.ToString(layer["path"]))
		if path == "" {
			continue
		}
		layers = append(layers, OutputLayer{Path: path, AutoLoad: toolutils.ToBool(layer["auto_load"])})
	}
	return layers
}

func parseDiagnosticsMap(raw any) map[string]any {
	diagnostics, ok := raw.(map[string]any)
	if !ok || len(diagnostics) == 0 {
		return nil
	}
	return cloneDiagnosticsMap(diagnostics)
}

func toFloat64(raw any) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func sortedArgKeys(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		keys = append(keys, trimmed)
	}
	sort.Strings(keys)
	return keys
}

func (p *Provider) resolveTargetPlugin(args map[string]any) (string, *pluginConnection, error) {
	targetID := ""
	explicitTarget := false
	if args != nil {
		if rawTargetID, ok := args["target_instance_id"].(string); ok {
			targetID = strings.TrimSpace(rawTargetID)
			explicitTarget = targetID != ""
		}
		if rawID, ok := args["instance_id"].(string); ok {
			if !explicitTarget {
				targetID = strings.TrimSpace(rawID)
				explicitTarget = targetID != ""
			}
		}
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if explicitTarget {
		if pl := p.instancePlugins[targetID]; pl != nil {
			return targetID, pl, nil
		}
		logger.Warn("middleware bridge 显式指定实例无有效连接，拒绝回退: instance=%s", targetID)
		return "", nil, newToolInvokeError(
			ToolErrorCodeInstanceRequired,
			fmt.Sprintf("显式指定实例无有效连接: %s", targetID),
			true,
			map[string]any{
				"requested_instance_id":  targetID,
				"active_instance_id":     strings.TrimSpace(p.activeInstance),
				"connected_instance_ids": p.sortedConnectedInstanceIDsLocked(),
			},
		)
	}

	targetID = strings.TrimSpace(p.activeInstance)
	if targetID != "" {
		if pl := p.instancePlugins[targetID]; pl != nil {
			return targetID, pl, nil
		}
	}

	for _, id := range p.sortedConnectedInstanceIDsLocked() {
		if pl := p.instancePlugins[id]; pl != nil {
			return id, pl, nil
		}
	}

	return "", nil, newToolInvokeError(
		ToolErrorCodeProviderMissing,
		fmt.Sprintf("无可用 QGIS 插件实例 (active=%q registered=%d)", strings.TrimSpace(p.activeInstance), len(p.instancePlugins)),
		true,
		map[string]any{
			"provider_id":            strings.TrimSpace(p.id),
			"active_instance_id":     strings.TrimSpace(p.activeInstance),
			"connected_instance_ids": p.sortedConnectedInstanceIDsLocked(),
		},
	)
}

// BroadcastMessage 向所有已连接插件广播控制消息。
func (p *Provider) BroadcastMessage(msgType string, data map[string]any) error {
	msgType = strings.TrimSpace(msgType)
	if msgType == "" {
		return errors.New("消息类型不能为空")
	}

	p.mu.RLock()
	states := make([]*pluginConnection, 0, len(p.pluginClients))
	for _, state := range p.pluginClients {
		if state == nil || state.conn == nil {
			continue
		}
		states = append(states, state)
	}
	p.mu.RUnlock()

	var firstErr error
	for _, state := range states {
		if err := p.sendPluginMessage(state, wsMessage{Type: msgType, Data: data}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *Provider) sendPluginMessage(state *pluginConnection, msg wsMessage) error {
	if state == nil || state.conn == nil {
		return errors.New("插件连接不可用")
	}
	if msg.Data == nil {
		msg.Data = make(map[string]any)
	}
	if _, exists := msg.Data["protocol_version"]; !exists {
		msg.Data["protocol_version"] = bridgeProtocolVersion
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	state.writeMu.Lock()
	defer state.writeMu.Unlock()
	return state.conn.WriteMessage(websocket.TextMessage, payload)
}

// BroadcastSessionLogReset 广播会话日志句柄重建通知。
func (p *Provider) BroadcastSessionLogReset(sessionID, projectPath, sessionDir, logsDir string) error {
	_ = sessionID
	_ = projectPath
	_ = sessionDir
	_ = logsDir
	logger.Debug("middleware bridge 忽略 session_log_reset side-channel 广播")
	return nil
}

func (p *Provider) pickToolDefinitions() []ToolDefinition {
	p.mu.RLock()
	defer p.mu.RUnlock()

	positions := make(map[string]int)
	merged := make([]ToolDefinition, 0)
	activeID := strings.TrimSpace(p.activeInstance)

	if activeID != "" {
		merged = appendMergedToolDefinitions(merged, positions, p.instancePlugins[activeID])
	}
	for _, instanceID := range p.sortedConnectedInstanceIDsLocked() {
		if instanceID == activeID {
			continue
		}
		merged = appendMergedToolDefinitions(merged, positions, p.instancePlugins[instanceID])
	}
	if len(merged) == 0 {
		return nil
	}
	for i := range merged {
		sort.Strings(merged[i].InstanceIDs)
	}
	sort.Slice(merged, func(i, j int) bool {
		return strings.TrimSpace(merged[i].Name) < strings.TrimSpace(merged[j].Name)
	})
	return merged
}

func (p *Provider) pickNextRunningInstanceLocked(excludeInstanceID string) string {
	excluded := strings.TrimSpace(excludeInstanceID)
	for _, instanceID := range p.sortedConnectedInstanceIDsLocked() {
		if instanceID == excluded {
			continue
		}
		if p.instancePlugins[instanceID] != nil {
			return instanceID
		}
	}
	return ""
}

func (p *Provider) sortedConnectedInstanceIDsLocked() []string {
	ids := make([]string, 0, len(p.instancePlugins))
	for instanceID, pluginState := range p.instancePlugins {
		if pluginState == nil {
			continue
		}
		trimmedID := strings.TrimSpace(instanceID)
		if trimmedID == "" {
			continue
		}
		ids = append(ids, trimmedID)
	}
	sort.Strings(ids)
	return ids
}

func appendMergedToolDefinitions(out []ToolDefinition, positions map[string]int, pluginState *pluginConnection) []ToolDefinition {
	if pluginState == nil || len(pluginState.tools) == 0 {
		return out
	}
	instanceID := strings.TrimSpace(pluginState.instanceID)
	for _, def := range pluginState.tools {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		if index, exists := positions[name]; exists {
			out[index].InstanceIDs = appendUniqueString(out[index].InstanceIDs, instanceID)
			continue
		}
		copied := def
		copied.Name = name
		copied.Description = strings.TrimSpace(def.Description)
		copied.Category = strings.TrimSpace(def.Category)
		copied.InputSchema = toolutils.DeepCopyMap(def.InputSchema)
		copied.InstanceIDs = appendUniqueString(nil, instanceID)
		positions[name] = len(out)
		out = append(out, copied)
	}
	return out
}

func appendUniqueString(values []string, raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), trimmed) {
			return values
		}
	}
	return append(values, trimmed)
}

func (p *Provider) notifyCatalogChanged() {
	p.mu.RLock()
	hook := p.onCatalogChanged
	p.mu.RUnlock()
	if hook != nil {
		hook()
	}
}

func (p *Provider) registerDynamicTools(instanceID string, defs []ToolDefinition) {
	if strings.TrimSpace(instanceID) == "" || len(defs) == 0 {
		return
	}
	p.mu.RLock()
	registry := p.dynamicToolRegistry
	p.mu.RUnlock()
	if registry == nil {
		return
	}
	cloned := make([]ToolDefinition, len(defs))
	copy(cloned, defs)
	registry.RegisterInstanceTools(strings.TrimSpace(instanceID), cloned)
}

func (p *Provider) unregisterDynamicTools(instanceID string, toolNames []string) {
	if strings.TrimSpace(instanceID) == "" || len(toolNames) == 0 {
		return
	}
	p.mu.RLock()
	registry := p.dynamicToolRegistry
	p.mu.RUnlock()
	if registry == nil {
		return
	}
	registry.UnregisterInstanceTools(strings.TrimSpace(instanceID), append([]string(nil), toolNames...))
}

func toolNamesFromDefinitions(defs []ToolDefinition) []string {
	if len(defs) == 0 {
		return nil
	}
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func parseToolDefinitions(raw any) []ToolDefinition {
	parsed := toolutils.ParseToolDefinitions(raw)
	tools := make([]ToolDefinition, 0, len(parsed))
	for _, item := range parsed {
		tools = append(tools, ToolDefinition{
			Name:        item.Name,
			Description: item.Description,
			Category:    item.Category,
			InputSchema: item.InputSchema,
		})
	}
	return tools
}
