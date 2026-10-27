package websocket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/websocket"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/knowledge"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/logger"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/prompts"
	"github.com/autocarto/carto_agent/internal/runtimepersist"
	"github.com/autocarto/carto_agent/internal/security"
	"github.com/autocarto/carto_agent/internal/session"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

// upgrader WebSocket升级器
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return security.IsAllowedLocalWebSocketRequest(r)
	},
}

const (
	clientPongWait           = 90 * time.Second
	clientPingInterval       = 30 * time.Second
	clientPingWriteTimeout   = 10 * time.Second
	geeProgressWriteTimeout  = 250 * time.Millisecond
	defaultToolInvokeTimeout = 5 * time.Minute
)

// DeepServer 基于 DeepAgents 的 WebSocket 服务器
type DeepServer struct {
	flowLifecycle             context.Context
	eventSequenceMu           sync.Mutex
	flowMu                    sync.Mutex
	flowCompiler              flowrun.Compiler
	flowController            *flowrun.Controller
	flowContexts              map[string]flowSessionContext
	pendingFlows              map[string]flowrun.StartRequest
	flowMessageMeta           map[string]map[string]any
	port                      int
	httpServer                *http.Server
	clients                   map[*websocket.Conn]bool
	connWriteMu               map[*websocket.Conn]*sync.Mutex
	planAgent                 adk.Agent
	buildAgent                adk.Agent
	runtime                   *agentruntime.Runtime
	runtimeFactory            agentruntime.Builder
	taskCancels               map[*websocket.Conn]map[string]context.CancelFunc
	sessionTaskCancels        map[string]context.CancelFunc
	sessionTaskWorkers        map[string]map[*sessionTaskWorker]struct{}
	deletedSessions           map[string]bool
	sessionManager            *session.Manager
	currentAgent              map[*websocket.Conn]map[string]string // 跟踪每个连接各会话当前的智能体
	sessionCurrentAgent       map[string]string
	activeSessionRuns         map[string]SessionRuntimeState
	sessionFailureOverrides   map[string]sessionFailureOverride
	sessionSubscribers        map[string]map[*websocket.Conn]struct{}
	connSubscribedSessions    map[*websocket.Conn]map[string]struct{}
	sessionOwners             map[string]*websocket.Conn
	sessionRunEventSeq        map[string]uint64
	sessionRunEventBuffers    map[string][]Message
	taskRequestLedger         map[string]taskRequestAck
	sessionRuntimeProjections map[string]*sessionRuntimeProjection
	mu                        sync.Mutex
	// HITL 中断确认
	checkpointStore          runtimepersist.CheckpointStore
	checkpointDBPath         string
	pendingInterrupts        map[*websocket.Conn]map[string]*PendingInterrupt
	sessionPendingInterrupts map[string]*PendingInterrupt
	knowledgeReviews         *knowledge.ReviewStore
	knowledgeConfirmMu       sync.Mutex
	// 配置管理器
	configService      *config.ConfigService
	userManager        *config.UserManager
	agentConfigManager *config.AgentConfigManager
	skillsDir          string
	builtinSkillsDir   string
	userSkillsDir      string
	// agent 重建配置
	agentRebuildConfig *AgentRebuildConfig
	// skills 目录监听
	skillsWatchStarted bool
	skillsWatchStopCh  chan struct{}
	skillsSnapshot     map[string]string
	// 会话策略维护
	sessionMaintenanceStarted bool
	sessionMaintenanceStopCh  chan struct{}
	knownSessionBaseDirs      map[string]time.Time
	sessionFileLocks          map[string]*sessionFileLock
	projectRegistryMu         sync.Mutex
	// 运行时实例状态
	activeInstanceID            string
	toolSchemaRevision          string
	toolAvailabilityRevision    string
	pendingRuntimeRebuildReason string
	lastRuntimeRebuildReason    string
	lastRuntimeRebuildAt        time.Time
	lastAppliedToolSchemaRev    string
	toolInvokeTimeout           time.Duration
	localBridgeCleanupInterval  time.Duration
	localRuntimeProjection      middlewareclient.RuntimeSnapshot
	localBridgeProvider         *bridgeprovider.Provider
	localBridgeProjection       middlewareclient.RuntimeSnapshot
	localBridgeEventsStopCh     chan struct{}
	toolCatalogProvider         func(context.Context) ([]middlewareclient.ToolCatalogItem, error)
	runtimeCache                *RuntimeCache
	middlewareToolReloader      func(context.Context) error
	aggregator                  *toolrouter.ToolAggregator
	capabilitySnapshotStore     *capability.SnapshotStore
	capabilityRefresher         capabilityRefreshRequester
	capabilitySnapshotMode      string
	promptRegistry              *prompts.Registry
	settingsMutationService     control.SettingsMutationService
	assetCatalogService         control.AssetCatalogService
	realtimeHub                 control.RealtimeHub
	skillCatalogStore           control.SkillCatalogStore
}

type capabilityRefreshRequester interface {
	RequestRefresh(reason string)
}

// NewDeepServer 创建 DeepServer
func NewDeepServer(port int, agent adk.Agent) *DeepServer {
	server := &DeepServer{
		port:                      port,
		clients:                   make(map[*websocket.Conn]bool),
		connWriteMu:               make(map[*websocket.Conn]*sync.Mutex),
		planAgent:                 agent,
		buildAgent:                agent,
		taskCancels:               make(map[*websocket.Conn]map[string]context.CancelFunc),
		sessionTaskCancels:        make(map[string]context.CancelFunc),
		sessionManager:            session.NewManager(50, 30*time.Minute),
		currentAgent:              make(map[*websocket.Conn]map[string]string),
		sessionCurrentAgent:       make(map[string]string),
		activeSessionRuns:         make(map[string]SessionRuntimeState),
		sessionFailureOverrides:   make(map[string]sessionFailureOverride),
		sessionSubscribers:        make(map[string]map[*websocket.Conn]struct{}),
		connSubscribedSessions:    make(map[*websocket.Conn]map[string]struct{}),
		sessionOwners:             make(map[string]*websocket.Conn),
		sessionRunEventSeq:        make(map[string]uint64),
		sessionRunEventBuffers:    make(map[string][]Message),
		taskRequestLedger:         make(map[string]taskRequestAck),
		sessionRuntimeProjections: make(map[string]*sessionRuntimeProjection),
		pendingInterrupts:         make(map[*websocket.Conn]map[string]*PendingInterrupt),
		sessionPendingInterrupts:  make(map[string]*PendingInterrupt),
		knownSessionBaseDirs:      make(map[string]time.Time),
		sessionFileLocks:          make(map[string]*sessionFileLock),
		toolInvokeTimeout:         defaultToolInvokeTimeout,
		runtimeCache:              NewRuntimeCache(),
		aggregator:                toolrouter.NewToolAggregator(),
		promptRegistry:            prompts.NewRegistry(),
	}
	server.checkpointStore = runtimepersist.NewMemoryCheckpointStore(server.currentRuntimeMetadata)
	return server
}

// SetRuntime 设置当前统一 Runtime。
func (s *DeepServer) SetRuntime(runtime *agentruntime.Runtime) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime = runtime
	if runtime == nil {
		return
	}
	s.planAgent = runtime.PlanAgent
	s.buildAgent = runtime.BuildAgent
}

// SetRuntimeFactory 设置 runtime 重建工厂。
func (s *DeepServer) SetRuntimeFactory(builder agentruntime.Builder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeFactory = builder
}

func (s *DeepServer) SetLocalBridgeProvider(provider *bridgeprovider.Provider) {
	var stopCh chan struct{}
	var registryEvents <-chan discovery.InstanceEvent
	var registryCleanupStarter interface{ StartCleanup(time.Duration) }

	s.mu.Lock()
	if s.localBridgeEventsStopCh != nil {
		close(s.localBridgeEventsStopCh)
		s.localBridgeEventsStopCh = nil
	}
	s.localBridgeProvider = provider
	if provider != nil {
		if registry := provider.InstanceRegistry(); registry != nil {
			stopCh = make(chan struct{})
			s.localBridgeEventsStopCh = stopCh
			registryEvents = registry.Events()
			registryCleanupStarter = registry
		}
	}
	s.mu.Unlock()
	s.refreshLocalBridgeProjection(nil)
	if stopCh != nil && registryEvents != nil {
		go s.consumeLocalBridgeRegistryEvents(registryEvents, stopCh)
	}
	if registryCleanupStarter != nil {
		registryCleanupStarter.StartCleanup(s.localBridgeCleanupInterval)
	}
}

// SetToolInvokeTimeout 设置插件工具调用默认超时。
func (s *DeepServer) SetToolInvokeTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	s.mu.Lock()
	s.toolInvokeTimeout = timeout
	s.mu.Unlock()
}

func (s *DeepServer) toolInvokeTimeoutOrDefault() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolInvokeTimeout <= 0 {
		return defaultToolInvokeTimeout
	}
	return s.toolInvokeTimeout
}

// SetManagers 设置配置管理器
func (s *DeepServer) SetManagers(cs *config.ConfigService, um *config.UserManager, acm *config.AgentConfigManager) {
	s.mu.Lock()
	s.configService = cs
	s.userManager = um
	s.agentConfigManager = acm
	s.mu.Unlock()
	if cs != nil {
		if fullCfg, err := cs.GetFullConfig(); err == nil && fullCfg != nil {
			s.mutateAgentRebuildConfig(func(cfg *AgentRebuildConfig) {
				cfg.BaseConfig = fullCfg
			})
		}
	}
	if err := s.configureCheckpointStore(); err != nil {
		logger.Warn("配置持久化 checkpoint store 失败: %v", err)
	}
	s.resetControlServices()
	if rebuildConfig := s.agentRebuildConfigSnapshot(); rebuildConfig != nil && rebuildConfig.ChatModel != nil {
		if err := s.rebuildAgent(); err != nil {
			logger.Warn("设置管理器后重建智能体失败: %v", err)
		}
	}
}

// GetToolAggregatorSnapshot 返回当前中间件工具聚合快照。
func (s *DeepServer) GetToolAggregatorSnapshot() toolrouter.Snapshot {
	if s.aggregator == nil {
		return toolrouter.Snapshot{}
	}
	return s.aggregator.Snapshot()
}

func (s *DeepServer) SetCapabilitySnapshotRuntime(store *capability.SnapshotStore, refresher capabilityRefreshRequester, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.capabilitySnapshotStore = store
	s.capabilityRefresher = refresher
	s.capabilitySnapshotMode = strings.TrimSpace(mode)
}

func (s *DeepServer) GetCapabilitySnapshot() *capability.CapabilitySnapshot {
	if s == nil || s.capabilitySnapshotStore == nil {
		return nil
	}
	return s.capabilitySnapshotStore.Get()
}

func (s *DeepServer) requestCapabilityRefresh(reason string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	refresher := s.capabilityRefresher
	s.mu.Unlock()
	if refresher == nil {
		return
	}
	refresher.RequestRefresh(strings.TrimSpace(reason))
}

func (s *DeepServer) GetRuntimeToolCatalogCache() []middlewareclient.ToolCatalogItem {
	if s == nil || s.runtimeCache == nil {
		return nil
	}
	return s.runtimeCache.ToolCatalogCache()
}

func (s *DeepServer) SetSkillDirs(builtinDir, userDir string) {
	s.skillsDir = strings.TrimSpace(userDir)
	s.builtinSkillsDir = strings.TrimSpace(builtinDir)
	s.userSkillsDir = strings.TrimSpace(userDir)
	s.resetControlServices()
}
func (s *DeepServer) SetSkillsDir(dir string) {
	s.SetSkillDirs("", dir)
}

func (s *DeepServer) currentRuntimeSkillDirs() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	builtinSkillsDir := strings.TrimSpace(s.builtinSkillsDir)
	userSkillsDir := strings.TrimSpace(s.userSkillsDir)
	provider := s.localBridgeProvider
	s.mu.Unlock()
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if builtinSkillsDir == "" && userSkillsDir == "" {
		return nil
	}
	pluginSkillsDir := ""
	if provider != nil {
		pluginSkillsDir = strings.TrimSpace(provider.PluginSkillsDir())
	}
	if pluginSkillsDir == "" && rebuildConfig != nil {
		pluginSkillsDir = strings.TrimSpace(rebuildConfig.PluginSkillsDir)
	}
	configBaseDir := ""
	activeConfigName := ""
	if s.agentConfigManager != nil {
		configBaseDir = strings.TrimSpace(s.agentConfigManager.ConfigBaseDir())
		activeConfigName = strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
	}
	if pluginSkillsDir == "" && configBaseDir == "" && activeConfigName == "" {
		return config.ResolveRuntimeSkillDirs(builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)
	}
	return config.ResolveRuntimeSkillDirsWithPlugin(pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)
}

func (s *DeepServer) sharedSkillCatalog() control.SkillCatalogStore {
	s.mu.Lock()
	store := s.skillCatalogStore
	builtinSkillsDir := strings.TrimSpace(s.builtinSkillsDir)
	userSkillsDir := strings.TrimSpace(s.userSkillsDir)
	localBridgeProvider := s.localBridgeProvider
	agentConfigManager := s.agentConfigManager
	rebuildConfig := cloneAgentRebuildConfig(s.agentRebuildConfig)
	s.mu.Unlock()
	if store != nil {
		return store
	}
	if builtinSkillsDir == "" && userSkillsDir == "" {
		return nil
	}

	builtinToolsDir := config.ResolveBuiltinToolsDir()
	userToolsDir := settings.InferUserToolsDirFromSkillsDir(userSkillsDir)
	configBaseDir := ""
	activeConfigName := ""
	if agentConfigManager != nil {
		configBaseDir = agentConfigManager.ConfigBaseDir()
		activeConfigName = agentConfigManager.GetActiveConfigName()
	}
	globalSkillsDir := config.ResolveGlobalSkillsDir(configBaseDir)
	pluginSkillsDir := ""
	if localBridgeProvider != nil {
		pluginSkillsDir = strings.TrimSpace(localBridgeProvider.PluginSkillsDir())
	}
	if pluginSkillsDir == "" && rebuildConfig != nil {
		pluginSkillsDir = strings.TrimSpace(rebuildConfig.PluginSkillsDir)
	}
	pluginToolsDir := settings.InferSiblingToolsDirFromSkillsDir(pluginSkillsDir)
	var dynamicToolRegistry control.DynamicToolRegistry
	if localBridgeProvider != nil {
		if registry, ok := localBridgeProvider.DynamicToolRegistry().(*skill.BridgeDynamicRegistry); ok {
			dynamicToolRegistry = registry
		}
	}
	var registryGovernance settings.RuntimeRegistryGovernance
	if rebuildConfig != nil {
		registryGovernance = rebuildConfig.RegistryGovernance
	}
	if registryGovernance == nil {
		registryGovernance = control.NewRegistryGovernance(control.RegistryGovernanceDeps{
			ResolveToolDirs: func() []string {
				return config.ResolveRuntimeToolDirsWithPlugin(pluginToolsDir, builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
			},
			ResolveSkillDirs:    s.currentRuntimeSkillDirs,
			DynamicToolRegistry: dynamicToolRegistry,
		})
	}
	newStore := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		BuiltinSkillsDir: builtinSkillsDir,
		UserSkillsDir:    userSkillsDir,
		GlobalSkillsDir:  globalSkillsDir,
		BuiltinToolsDir:  builtinToolsDir,
		UserToolsDir:     userToolsDir,
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
			return registryGovernance
		},
		ResolveRuntimeToolDirs: func() []string {
			return config.ResolveRuntimeToolDirsWithPlugin(pluginToolsDir, builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
		},
		ResolveRuntimeSkillDirs: s.currentRuntimeSkillDirs,
		ResolveActiveConfigName: func() string {
			if agentConfigManager == nil {
				return ""
			}
			return agentConfigManager.GetActiveConfigName()
		},
		ResolveExclusiveSkillDir: func(configName string) string {
			if agentConfigManager == nil {
				return ""
			}
			return agentConfigManager.ExclusiveSkillsDir(configName)
		},
	}, agentConfigManager)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skillCatalogStore != nil {
		return s.skillCatalogStore
	}
	s.skillCatalogStore = newStore
	return s.skillCatalogStore
}

// SetAgentRebuildConfig 设置 agent 重建所需的原始依赖
func (s *DeepServer) SetAgentRebuildConfig(cfg *AgentRebuildConfig) {
	snapshot := cloneAgentRebuildConfig(cfg)
	s.mu.Lock()
	s.agentRebuildConfig = snapshot
	s.runtimeFactory = nil
	s.mu.Unlock()
	if err := s.configureCheckpointStore(); err != nil {
		logger.Warn("配置持久化 checkpoint store 失败: %v", err)
	}
	if snapshot != nil && snapshot.ChatModel != nil && s.agentConfigManager != nil {
		if err := s.rebuildAgent(); err != nil {
			logger.Warn("设置重建配置后重建智能体失败: %v", err)
		}
	}
}

// rebuildAgent 从当前活动的智能体配置重建 agent 实例
func (s *DeepServer) rebuildAgent() error {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil {
		return fmt.Errorf("agentRebuildConfig 未设置")
	}
	if s.agentConfigManager == nil {
		return fmt.Errorf("agentConfigManager 未设置")
	}
	if rebuildConfig.ChatModel == nil {
		return fmt.Errorf("ChatModel 未初始化，无法重建智能体")
	}

	factory := s.getRuntimeFactory()
	if factory == nil {
		var err error
		factory, err = s.buildRuntimeFactory()
		if err != nil {
			return err
		}
		s.SetRuntimeFactory(factory)
	}

	ctx := context.Background()
	runtime, err := factory.BuildActive(ctx, s.agentConfigManager)
	if err != nil {
		return fmt.Errorf("创建 AgentRuntime 失败: %w", err)
	}

	rebuildReason := s.peekRuntimeRebuildReason()
	if strings.TrimSpace(rebuildReason) == "" {
		rebuildReason = "runtime_rebuild"
	}

	setPlanRuntimeExecutionModeInstruction("")
	s.SetRuntime(runtime)
	s.markRuntimeRebuildSuccess(rebuildReason, time.Now())
	s.realtimeService().BroadcastRuntimeSnapshot(s.GetRuntimeSnapshot())
	logger.Info("智能体重建完成: config=%s revision=%s reason=%s", runtime.ActiveConfigName, runtime.Revision, rebuildReason)
	if issue, ok := runtime.ModeIssue("plan"); ok {
		logger.Warn("Plan 模式当前不可用: code=%s reason=%s", issue.Code, issue.Message)
	}
	return nil
}

// GetAgent 根据执行模式返回对应的智能体。
func (s *DeepServer) GetAgent(mode executionMode) adk.Agent {
	agent, _ := s.getAgentWithFallback(mode)
	return agent
}

func (s *DeepServer) getAgentWithFallback(mode executionMode) (adk.Agent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.runtime != nil {
		return s.runtime.ResolveAgent(string(mode))
	}

	switch mode {
	case executionModePlan:
		if s.planAgent != nil {
			return s.planAgent, false
		}
		if s.buildAgent != nil {
			return s.buildAgent, true
		}
	default:
		if s.buildAgent != nil {
			return s.buildAgent, false
		}
		if s.planAgent != nil {
			return s.planAgent, true
		}
	}

	return nil, false
}

func (s *DeepServer) getRuntimeFactory() agentruntime.Builder {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeFactory
}

func (s *DeepServer) buildRuntimeFactory() (agentruntime.Builder, error) {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil {
		return nil, fmt.Errorf("agentRebuildConfig 未设置")
	}
	s.mu.Lock()
	capabilityStore := s.capabilitySnapshotStore
	capabilityMode := strings.TrimSpace(s.capabilitySnapshotMode)
	s.mu.Unlock()
	if rebuildConfig != nil && rebuildConfig.CapabilityStore != nil {
		capabilityStore = rebuildConfig.CapabilityStore
	}
	if rebuildConfig != nil && strings.TrimSpace(rebuildConfig.CapabilityMode) != "" {
		capabilityMode = strings.TrimSpace(rebuildConfig.CapabilityMode)
	}
	configBaseDir := ""
	if rebuildConfig.BaseConfig != nil {
		configBaseDir = strings.TrimSpace(rebuildConfig.BaseConfig.ConfigBaseDir)
	}
	return agentruntime.NewFactory(&agentruntime.FactoryConfig{
		DefaultChatModel:   rebuildConfig.ChatModel,
		DefaultProviderCfg: s.defaultRuntimeProviderConfig(),
		ResolveChatModel: func(provider string) (model.ToolCallingChatModel, error) {
			return s.resolveRuntimeChatModel(provider)
		},
		ResolvePlanChatModel: func(provider string) (model.ToolCallingChatModel, error) {
			return s.resolvePlanChatModel(provider)
		},
		ResolveProviderCfg: func(provider string) *config.LLMProviderConfig {
			return s.resolveRuntimeProviderConfig(provider)
		},
		BaseTools:          cloneBaseTools(rebuildConfig.Tools),
		MaxIterations:      rebuildConfig.MaxIteration,
		BuiltinSkillsDir:   rebuildConfig.BuiltinSkillsDir,
		UserSkillsDir:      rebuildConfig.UserSkillsDir,
		PluginSkillsDir:    strings.TrimSpace(rebuildConfig.PluginSkillsDir),
		ConfigBaseDir:      configBaseDir,
		RegistryGovernance: rebuildConfig.RegistryGovernance,
		CapabilityStore:    capabilityStore,
		CapabilityMode:     capabilityMode,
	})
}

func (s *DeepServer) currentPluginSkillsDir() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	provider := s.localBridgeProvider
	s.mu.Unlock()
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if provider != nil {
		if dir := strings.TrimSpace(provider.PluginSkillsDir()); dir != "" {
			return dir
		}
	}
	if rebuildConfig == nil {
		return ""
	}
	return strings.TrimSpace(rebuildConfig.PluginSkillsDir)
}

func (s *DeepServer) currentBridgeDynamicRegistry() *skill.BridgeDynamicRegistry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	provider := s.localBridgeProvider
	s.mu.Unlock()
	if provider == nil {
		return nil
	}
	registry, ok := provider.DynamicToolRegistry().(*skill.BridgeDynamicRegistry)
	if !ok {
		return nil
	}
	return registry
}

func (s *DeepServer) runtimeModeIssue(mode executionMode) (agentruntime.ModeIssue, bool) {
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()
	if runtime == nil {
		return agentruntime.ModeIssue{}, false
	}
	return runtime.ModeIssue(string(mode))
}

func cloneBaseTools(tools []tool.BaseTool) []tool.BaseTool {
	if len(tools) == 0 {
		return nil
	}
	cp := make([]tool.BaseTool, len(tools))
	copy(cp, tools)
	return cp
}

func cloneAgentRebuildConfig(cfg *AgentRebuildConfig) *AgentRebuildConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	cp.Tools = cloneBaseTools(cfg.Tools)
	return &cp
}

func (s *DeepServer) agentRebuildConfigSnapshot() *AgentRebuildConfig {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneAgentRebuildConfig(s.agentRebuildConfig)
}

func (s *DeepServer) mutateAgentRebuildConfig(mutate func(*AgentRebuildConfig)) *AgentRebuildConfig {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.agentRebuildConfig == nil {
		return nil
	}
	if mutate != nil {
		mutate(s.agentRebuildConfig)
	}
	return cloneAgentRebuildConfig(s.agentRebuildConfig)
}

func (s *DeepServer) currentRuntimeMetadata() runtimepersist.StoreMetadata {
	s.mu.Lock()
	runtime := s.runtime
	s.mu.Unlock()

	if runtime != nil {
		return runtimepersist.StoreMetadata{
			RuntimeRevision: strings.TrimSpace(runtime.Revision),
			ActiveConfig:    strings.TrimSpace(runtime.ActiveConfigName),
		}
	}

	activeConfigName := ""
	if s.agentConfigManager != nil {
		activeConfigName = strings.TrimSpace(s.agentConfigManager.GetActiveConfigName())
	}
	return runtimepersist.StoreMetadata{
		ActiveConfig: activeConfigName,
	}
}

func (s *DeepServer) configureCheckpointStore() error {
	rebuildConfig := s.agentRebuildConfigSnapshot()
	if rebuildConfig == nil || rebuildConfig.BaseConfig == nil {
		return nil
	}

	configBaseDir := strings.TrimSpace(rebuildConfig.BaseConfig.ConfigBaseDir)
	if configBaseDir == "" {
		return nil
	}

	dbPath := runtimepersist.ResolveCheckpointStorePath(configBaseDir)

	s.mu.Lock()
	currentPath := s.checkpointDBPath
	currentStore := s.checkpointStore
	s.mu.Unlock()

	if dbPath == currentPath {
		return nil
	}

	store, err := runtimepersist.NewSQLiteCheckpointStore(dbPath, s.currentRuntimeMetadata)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.checkpointStore = store
	s.checkpointDBPath = dbPath
	s.mu.Unlock()

	if closer, ok := currentStore.(interface{ Close() error }); ok && currentStore != nil {
		_ = closer.Close()
	}

	return nil
}

// rebuildChatModelAndAgent 用新的 provider 信息重建 ChatModel 和 Agent
func (s *DeepServer) rebuildChatModelAndAgent(provider *config.LLMProviderConfig) error {
	if s.agentRebuildConfigSnapshot() == nil {
		return fmt.Errorf("agentRebuildConfig 未设置")
	}

	// 创建新的 ChatModel
	llmConfig := &llm.Config{
		Provider:        provider.Provider,
		Model:           provider.Model,
		APIKey:          provider.APIKey,
		BaseURL:         provider.BaseURL,
		MaxTokens:       provider.MaxTokens,
		Temperature:     provider.Temperature,
		ThinkingEnabled: provider.ThinkingEnabled,
		ThinkingBudget:  provider.ThinkingBudget,
	}

	chatModel, err := llm.NewChatModel(llmConfig)
	if err != nil {
		return fmt.Errorf("创建 ChatModel 失败: %w", err)
	}

	// 更新 rebuildConfig 中的 ChatModel
	s.setPendingRuntimeRebuildReason("llm_provider_switch")
	fullCfg := (*config.Config)(nil)
	if s.configService != nil {
		if latestCfg, cfgErr := s.configService.GetFullConfig(); cfgErr == nil && latestCfg != nil {
			fullCfg = latestCfg
		}
	}
	updated := s.mutateAgentRebuildConfig(func(cfg *AgentRebuildConfig) {
		cfg.ChatModel = chatModel
		if fullCfg != nil {
			cfg.BaseConfig = fullCfg
		}
	})
	if updated == nil {
		return fmt.Errorf("agentRebuildConfig 未设置")
	}
	s.SetRuntimeFactory(nil)

	// 重建 agent
	return s.rebuildAgent()
}

// HandleConnection 导出的 WebSocket 连接处理方法，供外部 router 注册
func (s *DeepServer) HandleConnection(w http.ResponseWriter, r *http.Request) {
	s.handleConnection(w, r)
}

// Start 启动服务器，接受可选的外部 http.Handler（用于集成 REST API 路由）
func (s *DeepServer) Start(handler ...http.Handler) error {
	s.startSkillsWatcher()
	defer s.stopSkillsWatcher()
	s.startSessionMaintenance()
	defer s.stopSessionMaintenance()

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	var serveHandler http.Handler

	if len(handler) > 0 && handler[0] != nil {
		serveHandler = handler[0]
		logger.Info("服务器启动: %s (REST API + WebSocket)", addr)
	} else {
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", s.handleConnection)
		serveHandler = mux
		logger.Info("服务器启动: %s (WebSocket only)", addr)
	}

	server := &http.Server{Addr: addr, Handler: serveHandler}
	s.mu.Lock()
	s.httpServer = server
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.httpServer == server {
			s.httpServer = nil
		}
		s.mu.Unlock()
	}()

	return server.ListenAndServe()
}

func (s *DeepServer) Shutdown(ctx context.Context) error {
	s.stopSkillsWatcher()
	s.stopSessionMaintenance()
	s.mu.Lock()
	if s.localBridgeEventsStopCh != nil {
		close(s.localBridgeEventsStopCh)
		s.localBridgeEventsStopCh = nil
	}
	s.mu.Unlock()

	s.mu.Lock()
	server := s.httpServer
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

// handleConnection 处理 WebSocket 连接
func (s *DeepServer) handleConnection(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("role")), "plugin") {
		s.mu.Lock()
		provider := s.localBridgeProvider
		s.mu.Unlock()
		if provider == nil {
			http.Error(w, "local bridge unavailable", http.StatusServiceUnavailable)
			return
		}
		provider.HandleWebSocket(w, r)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket升级失败: %v", err)
		return
	}
	defer conn.Close()

	s.mu.Lock()
	s.clients[conn] = true
	if s.connWriteMu == nil {
		s.connWriteMu = make(map[*websocket.Conn]*sync.Mutex)
	}
	s.connWriteMu[conn] = &sync.Mutex{}
	s.mu.Unlock()
	s.sendCurrentRuntimeSnapshot(conn)

	defer func() {
		s.cleanupClientConnection(conn, false)
	}()

	clientAddr := conn.RemoteAddr().String()
	logger.Info("客户端连接: %s", clientAddr)
	s.sendSessionState(conn, "build")
	s.sendCurrentSessionRuntimeStates(conn)

	_ = conn.SetReadDeadline(time.Now().Add(clientPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(clientPongWait))
	})

	stopPing := make(chan struct{})
	go s.keepaliveClientConnection(conn, s.getConnWriteMu(conn), stopPing)
	defer close(stopPing)

	// 消息处理循环
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Debug("读取消息失败: %v", err)
			break
		}

		s.handleMessage(conn, message)
	}

	logger.Info("客户端断开: %s", clientAddr)
}

func (s *DeepServer) cleanupClientConnection(conn *websocket.Conn, cancelActiveTasks bool) {
	if conn == nil {
		return
	}
	if cancelActiveTasks {
		s.cancelAllTasks(conn)
	}
	s.detachAllSessionSubscriptions(conn)
	s.mu.Lock()
	delete(s.clients, conn)
	delete(s.connWriteMu, conn)
	delete(s.currentAgent, conn)
	delete(s.taskCancels, conn)
	delete(s.pendingInterrupts, conn)
	s.mu.Unlock()
}

func (s *DeepServer) keepaliveClientConnection(conn *websocket.Conn, writeMu *sync.Mutex, stop <-chan struct{}) {
	if writeMu == nil {
		return
	}

	ticker := time.NewTicker(clientPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			writeMu.Lock()
			err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(clientPingWriteTimeout))
			writeMu.Unlock()
			if err != nil {
				logger.Debug("客户端 keepalive ping 失败: %v", err)
				_ = conn.Close()
				return
			}
		}
	}
}

// processTask 处理任务

// consumeEvents 共享的事件消费方法，供 processTask 和 resumeTask 共用

// handleInterruptResponse 处理用户的中断响应

// resumeTask 恢复中断的任务执行
func marshalOutboundFrames(msg Message, mirrored *Message) ([][]byte, error) {
	frames := make([][]byte, 0, 2)
	data, err := json.Marshal(withWebSocketProtocolVersion(msg))
	if err != nil {
		return nil, err
	}
	frames = append(frames, data)

	runEvent := mirrored
	if runEvent == nil {
		if built, ok := buildRunEventMessage(msg); ok {
			runEvent = &built
		}
	}
	if runEvent != nil {
		runEventData, marshalErr := json.Marshal(withWebSocketProtocolVersion(*runEvent))
		if marshalErr != nil {
			logger.Warn("序列化 run_event 失败: type=%s err=%v", msg.Type, marshalErr)
		} else {
			frames = append(frames, runEventData)
		}
	}
	return frames, nil
}

func (s *DeepServer) writeMessageFrames(conn *websocket.Conn, frames [][]byte) error {
	writeMu := s.getConnWriteMu(conn)
	if writeMu == nil {
		return fmt.Errorf("connection write lock unavailable")
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	for _, frame := range frames {
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			return err
		}
	}
	return nil
}

// Progress may be dropped under backpressure: the latest state is retained in
// the session projection. A stalled UI must not hold up download cleanup.
func (s *DeepServer) writeGEEProgressFrames(conn *websocket.Conn, frames [][]byte) error {
	return s.writeDownloadProgressFrames(conn, frames)
}

func (s *DeepServer) writeDownloadProgressFrames(conn *websocket.Conn, frames [][]byte) error {
	writeMu := s.getConnWriteMu(conn)
	if writeMu == nil {
		return fmt.Errorf("connection write lock unavailable")
	}
	if !writeMu.TryLock() {
		return nil
	}
	defer writeMu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(geeProgressWriteTimeout)); err != nil {
		return err
	}
	defer conn.SetWriteDeadline(time.Time{})
	for _, frame := range frames {
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			// Gorilla connections cannot be reused after a write timeout. Closing
			// allows the existing client reconnect/replay path to recover promptly.
			_ = conn.Close()
			return err
		}
	}
	return nil
}

func (s *DeepServer) sendMessage(conn *websocket.Conn, msg Message) error {
	frames, err := marshalOutboundFrames(msg, nil)
	if err != nil {
		return err
	}
	return s.writeMessageFrames(conn, frames)
}

func (s *DeepServer) getConnWriteMu(conn *websocket.Conn) *sync.Mutex {
	if conn == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.connWriteMu == nil {
		s.connWriteMu = make(map[*websocket.Conn]*sync.Mutex)
	}
	if mu, ok := s.connWriteMu[conn]; ok {
		return mu
	}
	mu := &sync.Mutex{}
	s.connWriteMu[conn] = mu
	return mu
}

func (s *DeepServer) startSkillsWatcher() {
	roots := s.currentRuntimeSkillDirs()
	if len(roots) == 0 {
		return
	}

	s.mu.Lock()
	if s.skillsWatchStarted {
		s.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	s.skillsWatchStopCh = stopCh
	s.skillsWatchStarted = true
	s.skillsSnapshot = make(map[string]string)
	s.mu.Unlock()

	go s.watchSkillsDirectory(stopCh)
	logger.Info("Skills watcher 已启动: %s", strings.Join(roots, ","))
}

func (s *DeepServer) stopSkillsWatcher() {
	s.mu.Lock()
	if !s.skillsWatchStarted {
		s.mu.Unlock()
		return
	}
	stopCh := s.skillsWatchStopCh
	s.skillsWatchStopCh = nil
	s.skillsWatchStarted = false
	s.mu.Unlock()

	if stopCh != nil {
		close(stopCh)
	}
}

func (s *DeepServer) watchSkillsDirectory(stopCh chan struct{}) {
	defer s.finishSkillsWatcher(stopCh)
	ticker := time.NewTicker(skillsWatcherPollInterval)
	defer ticker.Stop()
	if s.areAllRuntimeSkillDirsMissing() {
		logger.Info("Skills watcher 已停止：所有技能目录均不可用")
		return
	}

	inputSnapshot, inputErr := s.collectSkillsWatchInputs()
	if inputErr != nil {
		inputSnapshot = nil
	}
	initialSnapshot, err := s.collectSkillsSnapshot()
	if err != nil {
		inputSnapshot = nil
		if s.areAllRuntimeSkillDirsMissing() {
			logger.Info("Skills watcher 已停止：所有技能目录均不可用")
			return
		}
		logger.Warn("Skills watcher 初始扫描失败: %v", err)
		initialSnapshot = make(map[string]string)
	}
	s.updateSkillsSnapshot(initialSnapshot)

	lastChangeAt := time.Time{}
	pendingNotify := false

	for {
		select {
		case <-stopCh:
			logger.Info("Skills watcher 已停止")
			return
		case <-ticker.C:
			if s.areAllRuntimeSkillDirsMissing() {
				logger.Info("Skills watcher 已停止：所有技能目录均不可用")
				return
			}
			latestInputs, inputErr := s.collectSkillsWatchInputs()
			if inputErr != nil {
				logger.Warn("Skills watcher 文件检查失败: %v", inputErr)
				continue
			}
			// Unchanged files need no registry parsing. Keep processing debounce
			// below so the prior change still produces its refresh notification.
			if len(latestInputs) == 0 || !skillsSnapshotEqual(inputSnapshot, latestInputs) {
				latestSnapshot, scanErr := s.collectSkillsSnapshot()
				if scanErr != nil {
					if s.areAllRuntimeSkillDirsMissing() {
						logger.Info("Skills watcher 已停止：所有技能目录均不可用")
						return
					}
					logger.Warn("Skills watcher 扫描失败: %v", scanErr)
					continue
				}
				inputSnapshot = latestInputs
				prevSnapshot := s.getSkillsSnapshot()
				if !skillsSnapshotEqual(prevSnapshot, latestSnapshot) {
					s.updateSkillsSnapshot(latestSnapshot)
					lastChangeAt = time.Now()
					pendingNotify = true
					logger.Debug("Skills watcher 检测到目录变化，等待去抖后推送")
					continue
				}
			}

			if pendingNotify && time.Since(lastChangeAt) >= skillsWatcherDebounce {
				pendingNotify = false
				logger.Debug("Skills watcher 推送目录变更: action=refresh, source=watcher")
				s.requestCapabilityRefresh("skills_watcher")
				s.realtimeService().BroadcastSkillCatalogChanged("refresh", "watcher", "")
			}
		}
	}
}

// collectSkillsWatchInputs only reads filesystem metadata. Include tool YAML
// as well as skill YAML so registry inputs cannot change behind the cache.
func (s *DeepServer) collectSkillsWatchInputs() (map[string]string, error) {
	roots := s.currentRuntimeSkillDirs()
	if catalog, ok := s.sharedSkillCatalog().(interface{ SkillCatalogWatchDirs() []string }); ok {
		roots = catalog.SkillCatalogWatchDirs()
	}
	snapshot := make(map[string]string)
	for index, root := range roots {
		root = filepath.Clean(root)
		// Root order determines overlay precedence, including empty roots.
		snapshot[fmt.Sprintf("root:%d", index)] = root
		if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if os.IsNotExist(walkErr) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" && !strings.EqualFold(entry.Name(), "SKILL.md") {
				return nil
			}
			info, err := entry.Info()
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			snapshot["file:"+path] = fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

func (s *DeepServer) areAllRuntimeSkillDirsMissing() bool {
	if s == nil {
		return false
	}
	roots := s.currentRuntimeSkillDirs()
	if len(roots) == 0 {
		return false
	}
	hasRoot := false
	for _, root := range roots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		hasRoot = true
		if _, err := os.Stat(trimmed); err == nil {
			return false
		} else if !os.IsNotExist(err) && !errors.Is(err, fs.ErrNotExist) {
			return false
		}
	}
	return hasRoot
}

func isSkillsDirMissingError(err error, skillsDir string) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) || errors.Is(err, fs.ErrNotExist) {
		return true
	}
	trimmedDir := strings.TrimSpace(skillsDir)
	lowerMessage := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(lowerMessage, "no such file or directory") {
		if trimmedDir == "" || strings.Contains(lowerMessage, strings.ToLower(trimmedDir)) {
			return true
		}
		return true
	}
	message := strings.TrimSpace(err.Error())
	if trimmedDir != "" && strings.Contains(message, trimmedDir) && strings.Contains(lowerMessage, "no such file or directory") {
		return true
	}
	return false
}

func (s *DeepServer) finishSkillsWatcher(stopCh chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skillsWatchStopCh == stopCh {
		s.skillsWatchStopCh = nil
		s.skillsWatchStarted = false
		return
	}
	if s.skillsWatchStopCh == nil {
		s.skillsWatchStarted = false
	}
}

func (s *DeepServer) collectSkillsSnapshot() (map[string]string, error) {
	snapshot := make(map[string]string)
	store := s.sharedSkillCatalog()
	if store == nil {
		return snapshot, nil
	}
	packages, err := store.ListSkillPackages()
	if err != nil {
		return nil, err
	}
	for _, item := range packages {
		packageID := strings.TrimSpace(item.PackageID)
		if packageID == "" {
			continue
		}
		// The catalog listing already contains the resolved visible package
		// path. Looking up each package again reloads the entire tool registry.
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		if err := filepath.WalkDir(item.Path, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			relPath, relErr := filepath.Rel(item.Path, path)
			if relErr != nil {
				return relErr
			}
			if !isTrackedSkillSnapshotPath(filepath.Join(packageID, relPath)) {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			snapshot[filepath.ToSlash(filepath.Join(packageID, relPath))] = fmt.Sprintf("%s|%s|%s|%d:%d", filepath.Clean(item.Path), strings.TrimSpace(item.Scope), strings.TrimSpace(item.OwnerConfig), info.ModTime().UnixNano(), info.Size())
			return nil
		}); err != nil {
			return nil, err
		}
	}

	return snapshot, nil
}

func isTrackedSkillSnapshotPath(path string) bool {
	trimmed := filepath.ToSlash(strings.TrimSpace(path))
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return false
	}
	if len(parts) == 2 && strings.EqualFold(parts[1], "SKILL.md") {
		return true
	}
	if len(parts) == 2 && (strings.EqualFold(parts[1], "config.yaml") || strings.EqualFold(parts[1], "config.yml")) {
		return true
	}
	return false
}

func (s *DeepServer) getSkillsSnapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make(map[string]string, len(s.skillsSnapshot))
	for k, v := range s.skillsSnapshot {
		cp[k] = v
	}
	return cp
}

func (s *DeepServer) updateSkillsSnapshot(snapshot map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skillsSnapshot = make(map[string]string, len(snapshot))
	for k, v := range snapshot {
		s.skillsSnapshot[k] = v
	}
}

func skillsSnapshotEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func (s *DeepServer) broadcastSkillCatalogChanged(action, source, skillName string) {
	msg := Message{
		Type: MsgTypeSkillCatalogChange,
		Data: map[string]any{
			"action":     action,
			"source":     source,
			"skill_name": skillName,
		},
	}

	connections := s.listClientConnections()
	for _, conn := range connections {
		if err := s.sendMessage(conn, msg); err != nil {
			logger.Warn("推送 skill_catalog_changed 失败: %v", err)
		}
	}
}

func (s *DeepServer) listClientConnections() []*websocket.Conn {
	s.mu.Lock()
	defer s.mu.Unlock()

	connections := make([]*websocket.Conn, 0, len(s.clients))
	for conn := range s.clients {
		connections = append(connections, conn)
	}
	return connections
}

// sendError 发送错误消息
func (s *DeepServer) sendError(conn *websocket.Conn, errMsg string) {
	s.sendMessage(conn, Message{
		Type:    "error",
		Content: errMsg,
	})
}

func (s *DeepServer) sendSessionMessage(conn *websocket.Conn, msg Message) error {
	_, err := s.sendSessionMessageSequenced(conn, msg)
	return err
}

func (s *DeepServer) sendSessionMessageSequenced(conn *websocket.Conn, msg Message) (uint64, error) {
	sessionID := strings.TrimSpace(msg.SessionID)
	if sessionID == "" {
		return 0, s.sendMessage(conn, msg)
	}

	s.flowMu.Lock()
	meta := s.flowMessageMeta[sessionID]
	s.flowMu.Unlock()
	if len(meta) > 0 {
		msg.Data = copyMessageData(msg.Data)
		for k, v := range meta {
			msg.Data[k] = v
		}
	}
	var sequence uint64
	var mirrored *Message
	if eventSeq := s.nextSessionRunEventSequence(sessionID); eventSeq > 0 {
		if runEvent, ok := buildRunEventMessageWithOptions(msg, runEventOptions{
			Sequence:        eventSeq,
			TimestampMillis: time.Now().UnixMilli(),
		}); ok {
			sequence = eventSeq
			mirrored = &runEvent
			s.recordSessionRunEvent(sessionID, runEvent)
		}
	}

	projectionChanged := s.applySessionWorkflowMessage(msg)

	frames, err := marshalOutboundFrames(msg, mirrored)
	if err != nil {
		return sequence, err
	}

	connections := s.listSessionSubscribers(sessionID)
	if len(connections) == 0 && conn != nil {
		connections = []*websocket.Conn{conn}
	}
	if len(connections) == 0 {
		if projectionChanged {
			s.persistSessionRuntimeProjection(sessionID)
		}
		return sequence, nil
	}

	var lastErr error
	source := stringValue(msg.Data["source"])
	downloadProgress := msg.Type == MsgTypeAgentWorkflow && msg.Phase == "progress" && (source == "gee" || source == "osm")
	for _, clientConn := range connections {
		var writeErr error
		if downloadProgress {
			writeErr = s.writeDownloadProgressFrames(clientConn, frames)
		} else {
			writeErr = s.writeMessageFrames(clientConn, frames)
		}
		if writeErr != nil {
			lastErr = writeErr
		}
	}
	if projectionChanged {
		s.persistSessionRuntimeProjection(sessionID)
	}
	return sequence, lastErr
}

func normalizeSessionID(conn *websocket.Conn, sessionID string) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed != "" {
		return trimmed
	}
	if conn == nil || conn.RemoteAddr() == nil {
		return ""
	}
	return conn.RemoteAddr().String()
}

func (s *DeepServer) setSessionTaskCancel(conn *websocket.Conn, sessionID string, cancel context.CancelFunc) {
	if cancel == nil {
		return
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if conn != nil {
		if s.taskCancels[conn] == nil {
			s.taskCancels[conn] = make(map[string]context.CancelFunc)
		}
		s.taskCancels[conn][sid] = cancel
	}
	s.sessionTaskCancels[sid] = cancel
}

// cancelSessionTask 取消指定会话任务。
func (s *DeepServer) cancelSessionTask(conn *websocket.Conn, sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return
	}

	defer func() {
		s.flowMu.Lock()
		delete(s.pendingFlows, sid)
		controller := s.flowController
		s.flowMu.Unlock()
		if controller != nil {
			_, _ = controller.Cancel(context.Background(), sid)
		}
	}()

	// Cancellation and review confirmation must not consume the same pause.
	s.knowledgeConfirmMu.Lock()
	defer s.knowledgeConfirmMu.Unlock()
	state, active := s.activeSessionRuntime(sid)

	var cancel context.CancelFunc
	s.mu.Lock()
	cancel = s.sessionTaskCancels[sid]
	delete(s.sessionTaskCancels, sid)
	for wsConn, connCancels := range s.taskCancels {
		delete(connCancels, sid)
		if len(connCancels) == 0 {
			delete(s.taskCancels, wsConn)
		}
	}
	for wsConn, connAgents := range s.currentAgent {
		delete(connAgents, sid)
		if len(connAgents) == 0 {
			delete(s.currentAgent, wsConn)
		}
	}
	delete(s.sessionCurrentAgent, sid)
	delete(s.sessionPendingInterrupts, sid)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// A paused runner has already returned, so its defer cannot finish the task.
	if active && state.Status == sessionRuntimeStatusInterrupted {
		s.markSessionRuntimeCancelled(sid, state.RunID)
		s.sendTaskRunningChanged(conn, sid, false)
	}
}

// cancelAllTasks 取消当前连接下所有会话任务。
func (s *DeepServer) cancelAllTasks(conn *websocket.Conn) {
	if conn == nil {
		return
	}

	s.mu.Lock()
	sessionIDs := make(map[string]struct{})
	for sid := range s.taskCancels[conn] {
		sessionIDs[sid] = struct{}{}
	}
	for sid := range s.pendingInterrupts[conn] {
		sessionIDs[sid] = struct{}{}
	}
	s.mu.Unlock()

	for sid := range sessionIDs {
		s.cancelSessionTask(conn, sid)
	}
}

// cancelSessionTasksForSession 取消所有连接中指定会话的任务。
func (s *DeepServer) cancelSessionTasksForSession(sessionID string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return
	}

	// Task ownership is session-scoped and survives disconnected clients.
	s.cancelSessionTask(nil, sid)
}

func (s *DeepServer) setSessionAgent(conn *websocket.Conn, sessionID, agent string) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCurrentAgent[sid] = strings.TrimSpace(agent)
	if conn == nil {
		return
	}
	if s.currentAgent[conn] == nil {
		s.currentAgent[conn] = make(map[string]string)
	}
	s.currentAgent[conn][sid] = strings.TrimSpace(agent)
}

func (s *DeepServer) getSessionAgent(conn *websocket.Conn, sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return ""
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if conn != nil {
		if connAgents := s.currentAgent[conn]; connAgents != nil {
			if agent := strings.TrimSpace(connAgents[sid]); agent != "" {
				return agent
			}
		}
	}
	return strings.TrimSpace(s.sessionCurrentAgent[sid])
}

func (s *DeepServer) setPendingInterrupt(conn *websocket.Conn, sessionID string, pending *PendingInterrupt) {
	if pending == nil {
		return
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionPendingInterrupts[sid] = pending
	if conn != nil {
		if s.pendingInterrupts[conn] == nil {
			s.pendingInterrupts[conn] = make(map[string]*PendingInterrupt)
		}
		s.pendingInterrupts[conn][sid] = pending
	}
}

func (s *DeepServer) popPendingInterrupt(conn *websocket.Conn, sessionID string) (*PendingInterrupt, bool) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = normalizeSessionID(conn, sessionID)
	}
	if sid == "" {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	pending, exists := s.sessionPendingInterrupts[sid]
	if !exists {
		return nil, false
	}
	delete(s.sessionPendingInterrupts, sid)
	for wsConn, connPending := range s.pendingInterrupts {
		delete(connPending, sid)
		if len(connPending) == 0 {
			delete(s.pendingInterrupts, wsConn)
		}
	}
	return pending, true
}

// BroadcastFlowCompile delivers background progress independently of requests.
func (s *DeepServer) BroadcastFlowCompile(status flow.CompileJobStatus) {
	raw, _ := json.Marshal(status)
	payload := map[string]any{}
	_ = json.Unmarshal(raw, &payload)
	for _, conn := range s.listClientConnections() {
		if err := s.sendMessage(conn, Message{Type: MsgTypeFlowCompile, Data: payload}); err != nil {
			logger.Warn("推送 flow_compile 失败: %v", err)
		}
	}
}
