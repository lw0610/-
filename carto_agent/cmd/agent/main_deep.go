package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/autocarto/carto_agent/internal/agentruntime"
	"github.com/autocarto/carto_agent/internal/api"
	"github.com/autocarto/carto_agent/internal/browserruntime"
	"github.com/autocarto/carto_agent/internal/capability"
	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/creatortools"
	"github.com/autocarto/carto_agent/internal/deepagent/subagents"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/fileops"
	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/autocarto/carto_agent/internal/integrations"
	"github.com/autocarto/carto_agent/internal/llm"
	"github.com/autocarto/carto_agent/internal/logger"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/plantools"
	"github.com/autocarto/carto_agent/internal/runtimecontrol"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	tooldef "github.com/autocarto/carto_agent/internal/tool"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolcontract"
	"github.com/autocarto/carto_agent/internal/websocket"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/gorilla/mux"
)

// 命令行参数
var (
	agentPort = flag.Int("port", 0, "WebSocket 服务端口（覆盖配置文件）")
)

// runDeepAgent 使用 DeepAgents 架构运行智能体系统
func runDeepAgent() {
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()
	parentGone, parentErr := watchDesktopParent(lifecycleCtx, os.Getenv(desktopParentPIDEnv), time.Second)
	if parentErr != nil {
		fmt.Fprintf(os.Stderr, "初始化桌面进程监控失败: %v\n", parentErr)
		return
	}
	select {
	case <-parentGone:
		return
	default:
	}

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载配置失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化日志（使用自定义配置）
	logConfig := &logger.LoggerConfig{
		MaxSize:      cfg.Log.MaxSize,
		MaxBackups:   cfg.Log.MaxBackups,
		MaxAge:       cfg.Log.MaxAge,
		MaxTotalSize: cfg.Log.MaxTotalSize,
		Compress:     cfg.Log.Compress,
	}
	loggerInstance, err := logger.NewLoggerWithConfig(cfg.Log.Dir, logger.ParseLevel(cfg.Log.Level), "agent", logConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化日志失败: %v\n", err)
		os.Exit(1)
	}
	defer loggerInstance.Close()

	// 设置为默认日志记录器
	if err := logger.Init(cfg.Log.Dir, logger.ParseLevel(cfg.Log.Level), "agent"); err != nil {
		fmt.Fprintf(os.Stderr, "初始化默认日志失败: %v\n", err)
		os.Exit(1)
	}

	logger.Info("DeepAgents 智能体系统启动中...")

	defaultConfigPath := config.FindConfigFile()
	userConfigPath := config.FindUserConfigFile()
	configService := config.NewConfigService(nil, cfg, defaultConfigPath, userConfigPath)
	defer browserruntime.DefaultManager.CloseAll()
	defaultConfigDisplayPath := strings.TrimSpace(defaultConfigPath)
	if defaultConfigDisplayPath == "" {
		defaultConfigDisplayPath = "embedded:config.yaml"
	}
	logger.Info("ConfigService 初始化成功，默认配置: %s，用户配置: %s", defaultConfigDisplayPath, userConfigPath)
	if fullCfg, cfgErr := configService.GetFullConfig(); cfgErr != nil {
		logger.Warn("读取完整配置失败，继续使用启动配置: %v", cfgErr)
	} else if fullCfg != nil {
		cfg.LLM = fullCfg.LLM
	}

	// 端口配置：命令行参数 > 配置文件（配置文件支持端口冲突自动回退）
	port := cfg.Ports.AgentSystem.GetPort()
	if *agentPort > 0 {
		port = *agentPort
		logger.Info("使用命令行指定的 WebSocket 端口: %d", port)
	} else {
		resolver := discovery.NewPortResolver("agent")
		resolvedPort, resolveErr := resolver.Resolve(cfg.Ports.AgentSystem)
		if resolveErr != nil {
			logger.Fatal("Agent 端口解析失败: %v", resolveErr)
		}
		if resolvedPort != cfg.Ports.AgentSystem.GetPort() {
			logger.Warn("默认 WebSocket 端口 %d 已占用，自动回退到可用端口 %d", cfg.Ports.AgentSystem.GetPort(), resolvedPort)
		}
		port = resolvedPort
	}

	logger.Info("WebSocket端口: %d", port)

	dataDir, dataErr := config.ResolvePluginDataDir()
	if dataErr != nil {
		logger.Fatal("获取 AutoCarto 数据目录失败: %v", dataErr)
	}
	svcRegistry, svcRegErr := discovery.NewServiceRegistry(
		filepath.Join(dataDir, "instances.db"),
	)
	if svcRegErr != nil {
		logger.Fatal("初始化 ServiceRegistry 失败: %v", svcRegErr)
	}
	defer svcRegistry.Close()

	if regErr := svcRegistry.Register(discovery.ServicePortEntry{
		ServiceName: "agent",
		Host:        "127.0.0.1",
		Port:        port,
		Protocol:    "websocket",
		PID:         os.Getpid(),
		Status:      "running",
	}); regErr != nil {
		logger.Warn("注册 agent 端口失败: %v", regErr)
	}
	if regErr := svcRegistry.Register(discovery.ServicePortEntry{
		ServiceName: discovery.ServiceNameAgentQGISBridge,
		Host:        "127.0.0.1",
		Port:        port,
		Protocol:    "websocket",
		PID:         os.Getpid(),
		Status:      "running",
	}); regErr != nil {
		logger.Warn("注册 agent 本地 bridge 端口失败: %v", regErr)
	}

	localInstanceRegistry := discovery.NewInstanceRegistry()
	cleanupInterval := time.Duration(cfg.Ports.AgentSystem.HealthCheckInterval) * time.Second
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Second
	}
	localInstanceRegistry.StartCleanup(cleanupInterval)
	defer localInstanceRegistry.Close()
	localBridgeProvider := bridgeprovider.NewProvider(discovery.ServiceNameQGISBridge, localInstanceRegistry)
	defer localBridgeProvider.Close()
	localBridgeProvider.SetDefaultInstanceSource(discovery.SourceAgentReverseWS)
	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	localBridgeProvider.SetDynamicToolRegistry(bridgeDynamicRegistry)
	bridgeToolInvokeTimeout := resolveBridgeToolInvokeTimeout(cfg)
	localBridgeProvider.SetInvokeTimeout(bridgeToolInvokeTimeout)
	var localComputeRuntime *compute.LocalRuntime

	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				_ = svcRegistry.HeartbeatOwned("agent", os.Getpid(), port)
				_ = svcRegistry.HeartbeatOwned(discovery.ServiceNameAgentQGISBridge, os.Getpid(), port)
			}
		}
	}()
	defer close(heartbeatDone)

	cleanupRuntimePortFile := func() {}
	if runtimePortFile, pathErr := config.ResolveRuntimePortFile(); pathErr != nil {
		logger.Warn("解析 runtime 端口文件路径失败: %v", pathErr)
	} else {
		if mkdirErr := os.MkdirAll(filepath.Dir(runtimePortFile), 0o755); mkdirErr != nil {
			logger.Warn("创建 runtime 端口文件目录失败: %v", mkdirErr)
		} else {
			if writeErr := os.WriteFile(runtimePortFile, []byte(strconv.Itoa(port)), 0o644); writeErr != nil {
				logger.Warn("写入 runtime 端口文件失败: %v", writeErr)
			} else {
				cleanupRuntimePortFile = func() {
					if removeErr := removeOwnedRuntimePortFile(runtimePortFile, port); removeErr != nil {
						logger.Warn("删除 runtime 端口文件失败: %v", removeErr)
					}
				}
				logger.Info("runtime 端口文件已写入: %s", runtimePortFile)
			}
		}
	}
	defer cleanupRuntimePortFile()

	// 初始化 ChatModel（允许在 API Key 缺失时启动服务）
	var chatModel model.ToolCallingChatModel
	activeProvider := cfg.GetActiveProvider()
	if activeProvider == nil {
		logger.Warn("未找到激活的 LLM 配置: %s，请通过设置页面配置", cfg.LLM.Active)
	} else {
		providerInfo, ok := llm.GetProviderInfo(activeProvider.Provider)
		if ok && providerInfo.RequiresAPIKey && activeProvider.APIKey == "" {
			logger.Warn("LLM API Key 未配置，请通过设置页面配置后重启服务")
		}

		llmConfig := &llm.Config{
			Provider:        activeProvider.Provider,
			Model:           activeProvider.Model,
			APIKey:          activeProvider.APIKey,
			BaseURL:         activeProvider.BaseURL,
			MaxTokens:       activeProvider.MaxTokens,
			Temperature:     activeProvider.Temperature,
			ThinkingEnabled: activeProvider.ThinkingEnabled,
			ThinkingBudget:  activeProvider.ThinkingBudget,
		}
		var err error
		chatModel, err = llm.NewChatModel(llmConfig)
		if err != nil {
			logger.Warn("初始化 ChatModel 失败: %v，请检查配置", err)
		} else {
			logger.Info("ChatModel 初始化成功")
		}
	}

	ctx := context.Background()
	var baseTools []tool.BaseTool
	var wsServer *websocket.DeepServer
	var agentConfigManager *config.AgentConfigManager
	var skillCatalog *settings.FileSkillService
	toolRefreshMu := &sync.Mutex{}
	computeClient, resolvedLocalCompute, computeErr := buildLocalComputeClient(cfg, localBridgeProvider, bridgeToolInvokeTimeout)
	if computeErr != nil {
		logger.Warn("agent 本地 compute CLI 解析失败，将保留 HTTP 兼容回退: %v", computeErr)
	} else {
		localComputeRuntime = resolvedLocalCompute
		logger.Info("agent 本地 compute reverse WS 已启用: python=%s", localComputeRuntime.PythonPath)
	}

	seedRuntimeAssets(cfg)
	builtinSkillsDir := config.ResolveBuiltinSkillsDir()
	if builtinSkillsDir == "" {
		logger.Warn("解析 builtin Skills 目录失败: 未发现插件目录")
	} else {
		logger.Info("Builtin Skills 目录: %s", builtinSkillsDir)
	}
	builtinToolsDir := config.ResolveBuiltinToolsDir()
	if builtinToolsDir == "" {
		logger.Warn("解析 builtin Tools 目录失败: 未发现插件目录")
	}
	userSkillsDir := config.ResolveUserSkillsDir()
	if userSkillsDir == "" {
		logger.Warn("解析用户 Skills 目录失败: 未发现插件目录")
	} else {
		logger.Info("User Skills 目录: %s", userSkillsDir)
	}

	userToolsDir := settings.InferUserToolsDirFromSkillsDir(userSkillsDir)
	if userToolsDir == "" {
		userToolsDir = config.ResolveUserToolsDir()
	}
	activeConfigName := ""
	localBridgeProvider.SetToolContractProvider(func() ([]toolcontract.ExecutionManifestEntry, error) {
		currentActiveConfigName := strings.TrimSpace(activeConfigName)
		if agentConfigManager != nil {
			currentActiveConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
		}
		runtimeToolDirs := config.ResolveRuntimeToolDirsWithPlugin(
			derivePluginToolsDir(localBridgeProvider.PluginSkillsDir()),
			builtinToolsDir,
			userToolsDir,
			cfg.ConfigBaseDir,
			currentActiveConfigName,
		)
		definitions, loadErr := tooldef.LoadAuthorityDefinitions(runtimeToolDirs...)
		if loadErr != nil {
			return nil, loadErr
		}
		return tooldef.BuildExecutionManifestEntries(definitions), nil
	})

	if computeClient == nil {
		logger.Fatal("本地 compute CLI runtime 不可用，HTTP compute fallback 已移除")
	}
	qgisInvokeTool := func(invokeCtx context.Context, toolName string, args map[string]any) (*compute.InvokeResponse, error) {
		if localBridgeProvider == nil {
			return nil, &bridgeprovider.ToolInvokeError{
				Code:      bridgeprovider.ToolErrorCodeProviderMissing,
				Message:   "qgis invoker unavailable",
				Retryable: true,
			}
		}
		resp, err := bridgeprovider.NewComputeToolInvoker(localBridgeProvider).InvokeTool(invokeCtx, toolName, args)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("qgis invoker returned nil response")
		}
		if !resp.Success {
			return nil, toolInvokeErrorFromComputeResponse(resp)
		}
		return resp, nil
	}
	buildOptions := skill.BuildOptions{
		ComputeClient: computeClient,
		QGISInvoker: func(invokeCtx context.Context, toolName string, args map[string]any) (any, error) {
			resp, err := qgisInvokeTool(invokeCtx, toolName, args)
			if err != nil {
				return nil, err
			}
			return resp.Output, nil
		},
		QGISResponseInvoker:  qgisInvokeTool,
		QGISRuntimeInspector: localBridgeProvider,
		ArgsMutator: func(mutateCtx context.Context, args map[string]any) {
			if wsServer == nil {
				return
			}
			wsServer.InjectToolRuntimeArgs(mutateCtx, args)
		},
		InternalInvoker: func(invokeCtx context.Context, toolName string, args map[string]any) (any, error) {
			switch {
			case creatortools.Handles(toolName):
				return creatortools.Invoke(invokeCtx, toolName, args, creatortools.Services{
					SkillCatalog:       skillCatalog,
					AgentConfigManager: agentConfigManager,
				})
			case plantools.Handles(toolName):
				return plantools.Invoke(invokeCtx, toolName, args, plantools.Services{
					AgentConfigManager: agentConfigManager,
					ActiveConfigName:   "",
					ActiveConfig:       nil,
					SkillsDir:          userSkillsDir,
					BuiltinSkillsDir:   builtinSkillsDir,
					UserSkillsDir:      userSkillsDir,
					PluginSkillsDir:    localBridgeProvider.PluginSkillsDir(),
					ConfigBaseDir:      cfg.ConfigBaseDir,
					BaseTools:          baseTools,
				})
			case integrations.Handles(toolName):
				return integrations.Invoke(invokeCtx, toolName, args, configService)
			case fileops.Handles(toolName):
				return fileops.Invoke(invokeCtx, toolName, args)
			case runtimecontrol.Handles(toolName):
				return runtimecontrol.Invoke(invokeCtx, toolName, args)
			default:
				return nil, fmt.Errorf("unsupported internal tool: %s", strings.TrimSpace(toolName))
			}
		},
	}
	buildOptions.AutoLoad = skill.AutoLoadOutputLayersViaQGIS(buildOptions.QGISInvoker)

	agentConfigDir := filepath.Join(cfg.ConfigBaseDir, "agent_configs")
	agentConfigManager = config.NewAgentConfigManager(agentConfigDir)
	logger.Info("AgentConfigManager 初始化成功，配置目录: %s", agentConfigDir)
	activeConfigName = ""
	if agentConfigManager != nil {
		activeConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
	}
	registryGovernance := newRuntimeRegistryGovernance(cfg, localBridgeProvider, builtinSkillsDir, userSkillsDir, agentConfigManager, bridgeDynamicRegistry)

	runtimeTools, loadErr := buildRuntimeTools(ctx, cfg, buildOptions, localBridgeProvider.PluginSkillsDir(), builtinSkillsDir, userSkillsDir, activeConfigName, bridgeDynamicRegistry)
	if loadErr != nil {
		logger.Warn("加载本地运行时工具目录失败，将以最小工具集继续并等待 capability snapshot 刷新: %v", loadErr)
		baseTools = prepareAgentTools(nil)
	} else {
		baseTools = prepareAgentTools(runtimeTools)
		logger.Info("已通过当前运行时工具路径加载工具: %d", len(baseTools))
	}

	capabilityMode := strings.TrimSpace(os.Getenv("CAPABILITY_SNAPSHOT_MODE"))
	if capabilityMode == "" {
		capabilityMode = "primary"
	}
	capabilityStore := capability.NewSnapshotStore()

	// 创建统一 Runtime（仅在 ChatModel 可用时）
	var runtime *agentruntime.Runtime
	var buildAgentForServer agentruntime.Builder
	if chatModel != nil {
		runtimeFactory, err := agentruntime.NewFactory(&agentruntime.FactoryConfig{
			DefaultChatModel:   chatModel,
			DefaultProviderCfg: cloneProviderConfig(cfg.GetActiveProvider()),
			BaseTools:          baseTools,
			MaxIterations:      cfg.Agent.LoopMaxIterations,
			BuiltinSkillsDir:   builtinSkillsDir,
			UserSkillsDir:      userSkillsDir,
			PluginSkillsDir:    localBridgeProvider.PluginSkillsDir(),
			ConfigBaseDir:      cfg.ConfigBaseDir,
			RegistryGovernance: registryGovernance,
			CapabilityStore:    capabilityStore,
			CapabilityMode:     capabilityMode,
			ResolveChatModel: func(provider string) (model.ToolCallingChatModel, error) {
				return resolveChatModelFromRuntimeConfig(cfg, provider)
			},
			ResolvePlanChatModel: func(provider string) (model.ToolCallingChatModel, error) {
				return resolvePlanChatModelFromRuntimeConfig(cfg, provider)
			},
			ResolveProviderCfg: func(provider string) *config.LLMProviderConfig {
				return cloneProviderConfig(cfg.GetProviderByName(provider))
			},
		})
		if err != nil {
			logger.Warn("创建 AgentRuntime 工厂失败: %v，服务将以受限模式运行", err)
		} else {
			buildAgentForServer = runtimeFactory
			runtime, err = runtimeFactory.BuildActive(ctx, agentConfigManager)
			if err != nil {
				logger.Warn("创建 AgentRuntime 失败: %v，服务将以受限模式运行", err)
			} else {
				logger.Info("AgentRuntime 创建成功: config=%s revision=%s", runtime.ActiveConfigName, runtime.Revision)
				if issue, ok := runtime.ModeIssue("plan"); ok {
					logger.Warn("Plan 模式当前不可用: code=%s reason=%s", issue.Code, issue.Message)
				}
			}
		}
	} else {
		logger.Warn("ChatModel 未初始化，服务将以受限模式运行（无法处理用户请求）")
	}

	// 启动 WebSocket 服务器（即使没有 Agent 也启动，以便接收配置更新）
	var initialAgent adk.Agent
	if runtime != nil {
		initialAgent = runtime.BuildAgent
	}
	wsServer = websocket.NewDeepServer(port, initialAgent)
	wsServer.SetLocalBridgeProvider(localBridgeProvider)
	wsServer.SetLocalRuntimeProjection(buildLocalRuntimeSnapshot(localComputeRuntime))
	capabilityRefresher := capability.NewRefresher(capabilityStore, func(refreshCtx context.Context, reason string) (*capability.CapabilitySnapshot, error) {
		activeConfigName := ""
		var activeConfig *config.AgentConfigFile
		if agentConfigManager != nil {
			activeConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
			activeConfig = agentConfigManager.GetActiveConfig()
		}
		registry := skill.NewRegistry()
		if activeConfig != nil {
			builtinToolsDir := config.ResolveBuiltinToolsDir()
			userToolsDir := settings.InferUserToolsDirFromSkillsDir(userSkillsDir)
			toolRegistry, toolErr := loadRuntimeToolRegistry(builtinToolsDir, userToolsDir, derivePluginToolsDir(localBridgeProvider.PluginSkillsDir()), cfg.ConfigBaseDir, activeConfigName)
			if toolErr != nil {
				return nil, toolErr
			}
			bridgeDynamicRegistry.ApplyToRegistry(toolRegistry)
			registry = skill.NewRegistry(toolRegistry)
			if err := registry.LoadDirectories(config.ResolveRuntimeSkillDirsWithPlugin(localBridgeProvider.PluginSkillsDir(), builtinSkillsDir, userSkillsDir, cfg.ConfigBaseDir, activeConfigName)...); err != nil {
				return nil, err
			}
		}
		builtinToolsDir := config.ResolveBuiltinToolsDir()
		userToolsDir := settings.InferUserToolsDirFromSkillsDir(userSkillsDir)
		skillCatalogStore := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
			BuiltinSkillsDir: builtinSkillsDir,
			UserSkillsDir:    userSkillsDir,
			GlobalSkillsDir:  config.ResolveGlobalSkillsDir(cfg.ConfigBaseDir),
			BuiltinToolsDir:  builtinToolsDir,
			UserToolsDir:     userToolsDir,
			ResolveRuntimeToolDirs: func() []string {
				return config.ResolveRuntimeToolDirsWithPlugin(derivePluginToolsDir(localBridgeProvider.PluginSkillsDir()), builtinToolsDir, userToolsDir, cfg.ConfigBaseDir, activeConfigName)
			},
		}, agentConfigManager)
		packageSummaries := []settings.SkillPackageSummary{}
		packageDetails := map[string]settings.SkillPackageDetail{}
		if skillCatalogStore != nil {
			if summaries, err := skillCatalogStore.ListSkillPackagesByScope(settings.SkillPackageScopeAll, activeConfigName); err == nil {
				packageSummaries = summaries
				for _, summary := range summaries {
					detail, detailErr := skillCatalogStore.GetSkillPackageByScope(summary.PackageID, summary.Scope, summary.OwnerConfig)
					if detailErr != nil {
						continue
					}
					packageDetails[summary.Scope+":"+summary.OwnerConfig+":"+summary.PackageID] = detail
				}
			}
		}
		runtimeSnapshot := wsServer.GetRuntimeSnapshot()
		snapshot, err := capability.BuildSnapshot(capability.BuildInput{
			ActiveConfigName:       activeConfigName,
			ActiveConfig:           activeConfig,
			RuntimeSkillConfig:     buildRuntimeSkillConfigProjection(activeConfig, localBridgeProvider.PluginSkillsDir(), builtinSkillsDir, userSkillsDir, cfg.ConfigBaseDir, activeConfigName),
			Registry:               registry,
			ToolAggregatorSnapshot: wsServer.GetToolAggregatorSnapshot(),
			RuntimeSnapshot:        runtimeSnapshot,
			ComputeReady:           computeReadyFromRuntimeSnapshot(runtimeSnapshot),
			PackageSummaries:       packageSummaries,
			PackageDetails:         packageDetails,
		})
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(capabilityMode, "shadow") {
			shadowSummary := summarizeCapabilityShadowDiff(wsServer.GetRuntimeToolCatalogCache(), snapshot)
			if shadowSummary.Changed {
				logger.Warn("capability snapshot shadow diff: reason=%s schema_changed=%t availability_changed=%t added=%v removed=%v modified=%v availability_only=%v", reason, shadowSummary.SchemaChanged, shadowSummary.AvailabilityChanged, shadowSummary.Added, shadowSummary.Removed, shadowSummary.Modified, shadowSummary.AvailabilityOnly)
			} else {
				logger.Debug("capability snapshot shadow diff clean: reason=%s", reason)
			}
		}
		return snapshot, nil
	})
	wsServer.SetCapabilitySnapshotRuntime(capabilityStore, capabilityRefresher, capabilityMode)
	if runtime != nil {
		wsServer.SetRuntime(runtime)
	}
	if cfg.Agent.IsSkillEngineEnabled() {
		wsServer.SetToolCatalogProvider(func(context.Context) ([]middlewareclient.ToolCatalogItem, error) {
			if items := runtimeCatalogItemsFromCapabilityStore(capabilityStore, capabilityMode, wsServer.GetRuntimeSnapshot()); len(items) > 0 {
				return items, nil
			}
			if !shouldAllowCapabilityLegacyFallback(capabilityMode) {
				return nil, nil
			}
			return buildRuntimeToolCatalogItemsWithSnapshot(context.Background(), cfg, computeClient, wsServer.GetRuntimeSnapshot(), localBridgeProvider.PluginSkillsDir(), activeConfigName, bridgeDynamicRegistry)
		})
	}
	localRuntimeSnapshot := buildLocalRuntimeSnapshot(localComputeRuntime)
	if cfg.Agent.IsSkillEngineEnabled() {
		catalogTools := []middlewareclient.ToolCatalogItem{}
		if shouldAllowCapabilityLegacyFallback(capabilityMode) {
			if localCatalogTools, localCatalogErr := buildRuntimeToolCatalogItemsWithSnapshot(ctx, cfg, computeClient, localRuntimeSnapshot, localBridgeProvider.PluginSkillsDir(), activeConfigName, bridgeDynamicRegistry); localCatalogErr == nil {
				catalogTools = append(catalogTools, localCatalogTools...)
			}
		}
		wsServer.PrimeRuntimeState(localRuntimeSnapshot, catalogTools)
	} else {
		wsServer.PrimeRuntimeState(localRuntimeSnapshot, nil)
	}
	wsServer.SetToolInvokeTimeout(bridgeToolInvokeTimeout)
	localBridgeProvider.SetCatalogChangedHook(func() {
		wsServer.HandleLocalBridgeStateChanged()
	})

	// 传递 agent 重建所需的原始依赖
	wsServer.SetAgentRebuildConfig(&websocket.AgentRebuildConfig{
		ChatModel:          chatModel,
		Tools:              baseTools,
		MaxIteration:       cfg.Agent.LoopMaxIterations,
		BuiltinSkillsDir:   builtinSkillsDir,
		UserSkillsDir:      userSkillsDir,
		PluginSkillsDir:    localBridgeProvider.PluginSkillsDir(),
		BaseConfig:         cfg,
		RegistryGovernance: registryGovernance,
		CapabilityStore:    capabilityStore,
		CapabilityMode:     capabilityMode,
	})
	if buildAgentForServer != nil {
		wsServer.SetRuntimeFactory(buildAgentForServer)
	}

	// 设置管理器到 WebSocket 服务器（agentConfigManager 已在前面初始化）
	wsServer.SetManagers(configService, nil, agentConfigManager)
	wsServer.SetSkillDirs(builtinSkillsDir, userSkillsDir)
	if _, refreshErr := capabilityRefresher.Refresh(ctx, "startup"); refreshErr != nil {
		logger.Warn("初始化 capability snapshot 失败: %v", refreshErr)
	} else if items := runtimeCatalogItemsFromCapabilityStore(capabilityStore, capabilityMode, wsServer.GetRuntimeSnapshot()); len(items) > 0 {
		wsServer.PrimeRuntimeState(wsServer.GetRuntimeSnapshot(), items)
	}

	wsServer.SetMiddlewareToolReloader(func(reloadCtx context.Context) error {
		toolRefreshMu.Lock()
		defer toolRefreshMu.Unlock()

		currentActiveConfigName := ""
		if agentConfigManager != nil {
			currentActiveConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
		}
		baseToolSet, refreshErr := buildRuntimeTools(reloadCtx, cfg, buildOptions, localBridgeProvider.PluginSkillsDir(), builtinSkillsDir, userSkillsDir, currentActiveConfigName, bridgeDynamicRegistry)
		if refreshErr != nil {
			return refreshErr
		}
		newTools := prepareAgentTools(baseToolSet)
		if err := wsServer.UpdateAgentTools(newTools); err != nil {
			return err
		}
		logger.Info("middleware 工具目录已刷新并重建 Agent: total=%d", len(newTools))
		return nil
	})

	// 组装 mux.Router，集成 REST API 和 WebSocket
	router := mux.NewRouter()

	skillCatalog = settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		BuiltinSkillsDir: builtinSkillsDir,
		UserSkillsDir:    userSkillsDir,
		GlobalSkillsDir:  config.ResolveGlobalSkillsDir(cfg.ConfigBaseDir),
		BuiltinToolsDir:  builtinToolsDir,
		UserToolsDir:     settings.InferUserToolsDirFromSkillsDir(userSkillsDir),
		ResolveRuntimeToolDirs: func() []string {
			return config.ResolveRuntimeToolDirsWithPlugin(
				derivePluginToolsDir(localBridgeProvider.PluginSkillsDir()),
				builtinToolsDir,
				settings.InferUserToolsDirFromSkillsDir(userSkillsDir),
				cfg.ConfigBaseDir,
				strings.TrimSpace(agentConfigManager.GetActiveConfigName()),
			)
		},
	}, agentConfigManager)
	runtimeCoordinator := control.NewRuntimeCoordinator(control.RuntimeCoordinatorDeps{
		AgentConfigManager: agentConfigManager,
		RuntimeSource:      wsServer,
		RuntimeDiagnostics: wsServer,
		InstanceRegistryProvider: func() *discovery.InstanceRegistry {
			return localInstanceRegistry
		},
	})
	settingsService := control.NewSettingsMutationService(control.SettingsMutationServiceDeps{
		ConfigService:      configService,
		AgentConfigManager: agentConfigManager,
		RuntimeController:  wsServer,
		SkillCatalog:       skillCatalog,
		ActiveRuns:         wsServer,
		SnapshotRefresher:  capabilityRefresher,
	})
	assetCatalog := control.NewAssetCatalogService(control.AssetCatalogServiceDeps{
		AgentConfigManager:   agentConfigManager,
		SkillCatalog:         skillCatalog,
		RuntimeCoordinator:   runtimeCoordinator,
		RuntimeDiagnostics:   wsServer,
		ToolAggregatorSource: wsServer,
		SnapshotStore:        capabilityStore,
		SnapshotMode:         capabilityMode,
	})
	realtimeHub := control.NewRealtimeHub(wsServer)
	wsServer.SetControlServices(settingsService, assetCatalog, realtimeHub)

	// REST API 路由
	apiHandler := api.NewAPIWithServices(configService, wsServer, localInstanceRegistry, svcRegistry, runtimeCoordinator, settingsService, assetCatalog, realtimeHub)
	apiHandler.SetLocalBridgeProvider(localBridgeProvider)
	apiHandler.SetupRoutes(router)

	// WebSocket 路由
	router.HandleFunc("/ws", wsServer.HandleConnection)

	go func() {
		logger.Info("服务器启动在端口 %d (REST API + WebSocket)", port)
		if err := wsServer.Start(router); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("服务器启动失败: %v", err)
		}
	}()

	logger.Info("系统启动完成，等待客户端连接...")

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	select {
	case <-sigChan:
	case <-parentGone:
		logger.Info("桌面父进程已退出，关闭受管理的后台服务")
	}

	logger.Info("DeepAgents 智能体系统正在关闭...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := wsServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("关闭 WebSocket 服务器失败: %v", err)
	}

	_ = svcRegistry.UnregisterOwned(discovery.ServiceNameAgentQGISBridge, os.Getpid(), port)
	_ = svcRegistry.UnregisterOwned("agent", os.Getpid(), port)

}

func prepareAgentTools(baseTools []tool.BaseTool) []tool.BaseTool {
	if len(baseTools) == 0 {
		return nil
	}

	// Runtime packages decide whether HITL is available. For an already
	// registered request tool, use its typed schema and native interrupt path;
	// the generic YAML adapter cannot describe the nested decision/plan schema.
	prepared := make([]tool.BaseTool, 0, len(baseTools))
	for _, item := range baseTools {
		if item != nil {
			info, err := item.Info(context.Background())
			if err == nil && info != nil && info.Name == runtimecontrol.ToolHITLRequest {
				if requestTool, requestErr := hitl.NewRequestTool(); requestErr == nil {
					item = requestTool
				} else {
					logger.Warn("创建原生 HITL 工具失败，保留已注册工具: %v", requestErr)
				}
			}
		}
		prepared = append(prepared, item)
	}
	wrapped := hitl.WrapHighRiskBaseTools(prepared)
	return dedupeAgentToolsByName(wrapped)
}

func dedupeAgentToolsByName(tools []tool.BaseTool) []tool.BaseTool {
	if len(tools) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tools))
	result := make([]tool.BaseTool, 0, len(tools))
	for _, item := range tools {
		if item == nil {
			continue
		}
		info, err := item.Info(context.Background())
		if err != nil || info == nil {
			result = append(result, item)
			continue
		}
		name := strings.TrimSpace(info.Name)
		if name == "" {
			result = append(result, item)
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, item)
	}
	return result
}

func websocketToolDirs(paths ...string) []string {
	seen := make(map[string]struct{}, len(paths))
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seen[cleaned]; exists {
			continue
		}
		seen[cleaned] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func toolInvokeErrorFromComputeResponse(resp *compute.InvokeResponse) error {
	if resp == nil {
		return fmt.Errorf("tool invoke returned nil response")
	}
	message := strings.TrimSpace(resp.Error)
	if message == "" {
		message = "tool invoke failed"
	}
	retryable := strings.TrimSpace(resp.ErrorCode) != ""
	return &bridgeprovider.ToolInvokeError{
		Code:                strings.TrimSpace(resp.ErrorCode),
		Message:             message,
		Retryable:           retryable,
		ProviderDiagnostics: cloneMap(resp.ProviderDiagnostics),
	}
}

func cloneMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func resolveChatModelFromRuntimeConfig(baseCfg *config.Config, providerName string) (model.ToolCallingChatModel, error) {
	trimmed := strings.TrimSpace(providerName)
	if trimmed == "" {
		return nil, fmt.Errorf("provider 不能为空")
	}
	if baseCfg == nil {
		return nil, fmt.Errorf("配置为空")
	}

	provider := baseCfg.GetProviderByName(trimmed)
	if provider == nil {
		return nil, fmt.Errorf("未找到 provider: %s", trimmed)
	}

	return llm.NewChatModel(config.ProviderToLLMConfig(provider))
}

func resolvePlanChatModelFromRuntimeConfig(baseCfg *config.Config, providerName string) (model.ToolCallingChatModel, error) {
	trimmed := strings.TrimSpace(providerName)
	if baseCfg == nil {
		return nil, fmt.Errorf("配置为空")
	}

	candidates, err := resolvePlanProviderCandidatesFromRuntimeConfig(baseCfg, trimmed)
	if err != nil {
		return nil, err
	}

	var failureMessages []string
	for _, provider := range candidates {
		chatModel, resolveErr := buildPlanChatModelFromRuntimeProvider(provider, trimmed)
		if resolveErr == nil {
			return chatModel, nil
		}
		failureMessages = append(failureMessages, resolveErr.Error())
	}

	return nil, fmt.Errorf("%s", strings.Join(failureMessages, "; "))
}

func resolvePlanProviderCandidatesFromRuntimeConfig(baseCfg *config.Config, providerName string) ([]*config.LLMProviderConfig, error) {
	trimmed := strings.TrimSpace(providerName)
	if baseCfg == nil {
		return nil, fmt.Errorf("配置为空")
	}
	if trimmed != "" {
		provider := baseCfg.GetProviderByName(trimmed)
		if provider == nil {
			return nil, fmt.Errorf("未找到 provider: %s", trimmed)
		}
		return []*config.LLMProviderConfig{provider}, nil
	}

	candidates := make([]*config.LLMProviderConfig, 0, 2)
	seen := make(map[string]struct{}, 2)
	appendCandidate := func(provider *config.LLMProviderConfig) {
		if provider == nil {
			return
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = fmt.Sprintf("%s::%s", strings.TrimSpace(provider.Provider), strings.TrimSpace(provider.Model))
		}
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, provider)
	}

	if quick := strings.TrimSpace(baseCfg.LLM.QuickModel); quick != "" {
		appendCandidate(baseCfg.GetProviderByName(quick))
	}
	appendCandidate(baseCfg.GetActiveProvider())

	if len(candidates) == 0 {
		return nil, fmt.Errorf("未找到激活 provider")
	}

	return candidates, nil
}

func buildPlanChatModelFromRuntimeProvider(provider *config.LLMProviderConfig, providerName string) (model.ToolCallingChatModel, error) {
	if provider == nil {
		return nil, fmt.Errorf("planner model 不满足 planexecute 强制 tool-call 协议（provider=%s）: provider 未初始化", strings.TrimSpace(providerName))
	}

	normalized, err := llm.PrepareConfigForCapability(config.ProviderToLLMConfig(provider), llm.CapabilityPlanExecuteForcedToolChoice)
	if err != nil {
		targetName := strings.TrimSpace(provider.Name)
		if targetName == "" {
			targetName = strings.TrimSpace(providerName)
		}
		return nil, fmt.Errorf("planner model 不满足 planexecute 强制 tool-call 协议（provider=%s backend=%s model=%s）: %w", targetName, provider.Provider, provider.Model, err)
	}

	chatModel, err := llm.NewChatModel(normalized)
	if err != nil {
		targetName := strings.TrimSpace(provider.Name)
		if targetName == "" {
			targetName = strings.TrimSpace(providerName)
		}
		return nil, fmt.Errorf("planner model 不满足 planexecute 强制 tool-call 协议（provider=%s backend=%s model=%s）: %w", targetName, provider.Provider, provider.Model, err)
	}

	return chatModel, nil
}

func cloneProviderConfig(cfg *config.LLMProviderConfig) *config.LLMProviderConfig {
	if cfg == nil {
		return nil
	}
	cp := *cfg
	return &cp
}

func buildRuntimeTools(ctx context.Context, cfg *config.Config, opts skill.BuildOptions, pluginSkillsDir string, builtinSkillsDir string, userSkillsDir string, activeConfigName string, bridgeDynamicRegistry ...*skill.BridgeDynamicRegistry) ([]tool.BaseTool, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	registry, err := loadRuntimeSkillRegistry(builtinSkillsDir, userSkillsDir, pluginSkillsDir, cfg.ConfigBaseDir, activeConfigName, bridgeDynamicRegistry...)
	if err != nil {
		return nil, err
	}
	engine, err := buildRuntimeSkillEngine(registry, opts)
	if err != nil {
		return nil, err
	}
	skillTools, err := skill.BuildToolsFromRegistry(registry, engine)
	if err != nil {
		return nil, err
	}
	return skillTools, nil
}

func loadRuntimeSkillRegistry(builtinSkillsDir string, userSkillsDir string, pluginSkillsDir string, configBaseDir string, activeConfigName string, bridgeDynamicRegistry ...*skill.BridgeDynamicRegistry) (*skill.Registry, error) {
	builtinSkillsDir = strings.TrimSpace(builtinSkillsDir)
	if builtinSkillsDir == "" {
		builtinSkillsDir = config.ResolveBuiltinSkillsDir()
	}
	userSkillsDir = strings.TrimSpace(userSkillsDir)
	if userSkillsDir == "" {
		userSkillsDir = config.ResolveUserSkillsDir()
	}
	builtinToolsDir := settings.InferSiblingToolsDirFromSkillsDir(builtinSkillsDir)
	if builtinToolsDir == "" {
		builtinToolsDir = config.ResolveBuiltinToolsDir()
	}
	userToolsDir := settings.InferUserToolsDirFromSkillsDir(userSkillsDir)
	if userToolsDir == "" {
		userToolsDir = config.ResolveUserToolsDir()
	}
	toolRegistry, err := loadRuntimeToolRegistry(builtinToolsDir, userToolsDir, derivePluginToolsDir(pluginSkillsDir), configBaseDir, activeConfigName)
	if err != nil {
		return nil, err
	}
	if len(bridgeDynamicRegistry) > 0 && bridgeDynamicRegistry[0] != nil {
		bridgeDynamicRegistry[0].ApplyToRegistry(toolRegistry)
	}
	registry := skill.NewRegistry(toolRegistry)
	if err := registry.LoadDirectories(config.ResolveRuntimeSkillDirsWithPlugin(pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)...); err != nil {
		return nil, err
	}
	return registry, nil
}

func loadRuntimeToolRegistry(builtinToolsDir string, userToolsDir string, pluginToolsDir string, configBaseDir string, activeConfigName string) (*tooldef.Registry, error) {
	registry := tooldef.NewRegistry()
	if err := registry.LoadDirectories(resolveRuntimeToolDirs(pluginToolsDir, builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)...); err != nil {
		return nil, err
	}
	return registry, nil
}

func resolveRuntimeToolDirs(pluginToolsDir string, builtinToolsDir string, userToolsDir string, configBaseDir string, activeConfigName string) []string {
	return config.ResolveRuntimeToolDirsWithPlugin(pluginToolsDir, builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
}

func derivePluginToolsDir(pluginSkillsDir string) string {
	trimmedSkillsDir := strings.TrimSpace(pluginSkillsDir)
	if trimmedSkillsDir == "" {
		return ""
	}
	skillsPath := filepath.Clean(trimmedSkillsDir)
	baseName := filepath.Base(skillsPath)
	parentDir := filepath.Dir(skillsPath)
	if (strings.EqualFold(baseName, "system") || strings.EqualFold(baseName, "user")) && strings.EqualFold(filepath.Base(parentDir), "skills") {
		return filepath.Join(filepath.Dir(parentDir), "tools", baseName)
	}
	if strings.EqualFold(baseName, "skills") {
		return filepath.Join(filepath.Dir(skillsPath), "tools", "system")
	}
	return settings.InferSiblingToolsDirFromSkillsDir(pluginSkillsDir)
}

func buildRuntimeSkillConfigProjection(activeConfig *config.AgentConfigFile, pluginSkillsDir string, builtinSkillsDir string, userSkillsDir string, configBaseDir string, activeConfigName string) *subagents.RuntimeSkillConfig {
	return config.ConvertToRuntimeSkillConfigWithPlugin(activeConfig, pluginSkillsDir, builtinSkillsDir, userSkillsDir, configBaseDir, activeConfigName)
}

func buildRuntimeSkillEngine(registry *skill.Registry, opts skill.BuildOptions) (*skill.Engine, error) {
	if registry == nil {
		return nil, fmt.Errorf("skill registry is nil")
	}
	if opts.ComputeClient == nil {
		return nil, fmt.Errorf("compute client is nil")
	}
	computeBackend := skill.NewComputeBackend(opts.ComputeClient)
	backends := []skill.ToolBackend{computeBackend}
	if opts.QGISResponseInvoker != nil || opts.QGISInvoker != nil {
		backends = append(
			backends,
			skill.NewQGISBackend(
				opts.QGISResponseInvoker,
				opts.QGISInvoker,
				opts.QGISRuntimeInspector,
				opts.ArgsMutator,
			),
		)
	}
	if opts.InternalInvoker != nil {
		backends = append(backends, skill.NewInternalBackend(opts.InternalInvoker))
	}
	engine := skill.NewEngine(registry, backends...)
	if opts.AutoLoad != nil {
		engine.SetAutoLoadHandler(func(autoCtx context.Context, params map[string]any, layers []skill.OutputLayer) {
			if len(layers) == 0 {
				return
			}
			converted := make([]compute.OutputLayer, 0, len(layers))
			for _, layer := range layers {
				converted = append(converted, compute.OutputLayer{Path: layer.Path, AutoLoad: layer.AutoLoad})
			}
			opts.AutoLoad(autoCtx, params, converted)
		})
	}
	return engine, nil
}

func buildRuntimeToolCatalogItems(ctx context.Context, cfg *config.Config, computeClient *compute.Client, pluginSkillsDir string, activeConfigName string, bridgeDynamicRegistry ...*skill.BridgeDynamicRegistry) ([]middlewareclient.ToolCatalogItem, error) {
	return buildRuntimeToolCatalogItemsWithSnapshot(ctx, cfg, computeClient, middlewareclient.RuntimeSnapshot{}, pluginSkillsDir, activeConfigName, bridgeDynamicRegistry...)
}

func buildRuntimeToolCatalogItemsWithSnapshot(ctx context.Context, cfg *config.Config, computeClient *compute.Client, snapshot middlewareclient.RuntimeSnapshot, pluginSkillsDir string, activeConfigName string, bridgeDynamicRegistry ...*skill.BridgeDynamicRegistry) ([]middlewareclient.ToolCatalogItem, error) {
	builtinSkillsDir := config.ResolveBuiltinSkillsDir()
	userSkillsDir := config.ResolveUserSkillsDir()
	registry, err := loadRuntimeSkillRegistry(builtinSkillsDir, userSkillsDir, pluginSkillsDir, cfg.ConfigBaseDir, activeConfigName, bridgeDynamicRegistry...)
	if err != nil {
		return nil, err
	}
	items, err := skill.BuildCatalogItemsFromRegistry(registry)
	if err != nil {
		return nil, err
	}
	items = applyLocalComputeToolAvailability(ctx, computeClient, items)
	return skill.ApplyBridgeRuntimeAvailability(items, snapshot), nil
}

func applyLocalComputeToolAvailability(ctx context.Context, computeClient *compute.Client, items []middlewareclient.ToolCatalogItem) []middlewareclient.ToolCatalogItem {
	if computeClient == nil || len(items) == 0 {
		return items
	}
	if ctx == nil {
		ctx = context.Background()
	}
	defs, err := computeClient.ListTools(ctx)
	if err != nil {
		defs = computeClient.CachedTools()
		if len(defs) == 0 {
			logger.Warn("加载本地 compute 工具目录失败，将保留静态技能目录状态: %v", err)
			return items
		}
		logger.Warn("加载本地 compute 工具目录失败，回退到缓存结果: %v", err)
	}
	byName := make(map[string]compute.ToolDefinition, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		byName[name] = def
	}
	if len(byName) == 0 {
		return items
	}
	updated := make([]middlewareclient.ToolCatalogItem, 0, len(items))
	for _, item := range items {
		copied := item
		if !isComputeToolCatalogItem(item) {
			updated = append(updated, copied)
			continue
		}
		def, ok := byName[strings.TrimSpace(item.Name)]
		if !ok {
			updated = append(updated, copied)
			continue
		}
		copied.Available = def.Available
		copied.UnavailableReason = strings.TrimSpace(def.UnavailableReason)
		copied.Backend = skill.RuntimeIDCLI
		if copied.Category == "" {
			copied.Category = strings.TrimSpace(def.Category)
		}
		if copied.Description == "" {
			copied.Description = strings.TrimSpace(def.Description)
		}
		updated = append(updated, copied)
	}
	return updated
}

func isComputeToolCatalogItem(item middlewareclient.ToolCatalogItem) bool {
	runtimeID := skill.NormalizeRuntime(item.Backend)
	if runtimeID != "" {
		return strings.EqualFold(runtimeID, skill.RuntimeIDCLI)
	}
	providerID := strings.TrimSpace(item.ProviderID)
	source := strings.TrimSpace(item.Source)
	return strings.EqualFold(providerID, skill.RuntimeIDCLI) || strings.EqualFold(source, skill.RuntimeIDCLI) || strings.EqualFold(source, "compute")
}

func computeReadyFromRuntimeSnapshot(snapshot middlewareclient.RuntimeSnapshot) bool {
	for _, serviceID := range []string{skill.RuntimeIDCLI} {
		service, ok := snapshot.Services[serviceID]
		if !ok {
			continue
		}
		if service.Ready || strings.EqualFold(strings.TrimSpace(service.Status), "running") {
			return true
		}
	}
	return false
}

func summarizeCapabilityShadowDiff(existing []middlewareclient.ToolCatalogItem, snapshot *capability.CapabilitySnapshot) toolcatalog.ToolDiffResult {
	if snapshot == nil {
		return toolcatalog.ToolDiffResult{}
	}
	return toolcatalog.DiffTools(existing, snapshot.AsRuntimeCatalogItems())
}

func shouldUseCapabilitySnapshot(mode string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(mode))
	return trimmed == "cutover" || trimmed == "primary"
}

func shouldAllowCapabilityLegacyFallback(mode string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(mode))
	return trimmed == "shadow" || trimmed == "cutover"
}

func runtimeCatalogItemsFromCapabilityStore(store *capability.SnapshotStore, mode string, runtimeSnapshot middlewareclient.RuntimeSnapshot) []middlewareclient.ToolCatalogItem {
	if !shouldUseCapabilitySnapshot(mode) || store == nil {
		return nil
	}
	snapshot := store.Get()
	if snapshot == nil {
		return nil
	}
	return skill.ApplyBridgeRuntimeAvailability(snapshot.AsRuntimeCatalogItems(), runtimeSnapshot)
}

func newRuntimeRegistryGovernance(
	cfg *config.Config,
	localBridgeProvider *bridgeprovider.Provider,
	builtinSkillsDir string,
	userSkillsDir string,
	agentConfigManager *config.AgentConfigManager,
	bridgeDynamicRegistry *skill.BridgeDynamicRegistry,
) *control.RegistryGovernance {
	if cfg == nil {
		return nil
	}
	return control.NewRegistryGovernance(control.RegistryGovernanceDeps{
		ResolveToolDirs: func() []string {
			activeConfigName := ""
			if agentConfigManager != nil {
				activeConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
			}
			pluginSkillsDir := ""
			if localBridgeProvider != nil {
				pluginSkillsDir = strings.TrimSpace(localBridgeProvider.PluginSkillsDir())
			}
			return config.ResolveRuntimeToolDirsWithPlugin(
				derivePluginToolsDir(pluginSkillsDir),
				config.ResolveBuiltinToolsDir(),
				settings.InferUserToolsDirFromSkillsDir(userSkillsDir),
				cfg.ConfigBaseDir,
				activeConfigName,
			)
		},
		ResolveSkillDirs: func() []string {
			activeConfigName := ""
			if agentConfigManager != nil {
				activeConfigName = strings.TrimSpace(agentConfigManager.GetActiveConfigName())
			}
			pluginSkillsDir := ""
			if localBridgeProvider != nil {
				pluginSkillsDir = strings.TrimSpace(localBridgeProvider.PluginSkillsDir())
			}
			return config.ResolveRuntimeSkillDirsWithPlugin(
				pluginSkillsDir,
				builtinSkillsDir,
				userSkillsDir,
				cfg.ConfigBaseDir,
				activeConfigName,
			)
		},
		DynamicToolRegistry: bridgeDynamicRegistry,
	})
}

func buildLocalRuntimeSnapshot(localRuntime *compute.LocalRuntime) middlewareclient.RuntimeSnapshot {
	snapshot := middlewareclient.RuntimeSnapshot{
		Timestamp: time.Now(),
		Services:  map[string]middlewareclient.ServiceStatus{},
		Instances: middlewareclient.InstancesSnapshot{Items: []*discovery.QGISInstance{}},
	}
	if localRuntime == nil {
		return snapshot
	}
	computeDiagnostics := map[string]any{
		"ownership":   "agent",
		"transport":   "reverse_websocket",
		"python_path": localRuntime.PythonPath,
		"working_dir": localRuntime.WorkingDir,
	}
	snapshot.Services[skill.RuntimeIDCLI] = middlewareclient.ServiceStatus{
		ID:      skill.RuntimeIDCLI,
		Status:  "running",
		Source:  "sidecar_local_cli",
		Ready:   true,
		Enabled: true,
		Diagnostics: map[string]any{
			"ownership":   computeDiagnostics["ownership"],
			"transport":   computeDiagnostics["transport"],
			"python_path": computeDiagnostics["python_path"],
			"working_dir": computeDiagnostics["working_dir"],
		},
	}
	return snapshot
}

func buildLocalComputeClient(cfg *config.Config, localBridgeProvider *bridgeprovider.Provider, timeout time.Duration) (*compute.Client, *compute.LocalRuntime, error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config is nil")
	}
	pythonPath := resolveAgentComputePythonPath(cfg)
	if strings.TrimSpace(pythonPath) == "" {
		return nil, nil, fmt.Errorf("agent 本地 compute 缺少可用 python_path")
	}
	runtime, err := compute.ResolveLocalRuntime(pythonPath, cfg.Compute.MaxWorkers)
	if err != nil {
		return nil, nil, err
	}
	return compute.NewWSClient(bridgeprovider.NewComputeToolInvoker(localBridgeProvider), "", timeout), runtime, nil
}

func resolveBridgeToolInvokeTimeout(cfg *config.Config) time.Duration {
	if cfg != nil && cfg.Performance.ToolCallTimeout > 0 {
		return time.Duration(cfg.Performance.ToolCallTimeout) * time.Millisecond
	}
	return 120 * time.Second
}

func resolveAgentComputePythonPath(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	if pythonPath := strings.TrimSpace(cfg.Compute.PythonPath); pythonPath != "" {
		return pythonPath
	}
	fallback := ""
	for _, candidate := range []string{"/usr/bin/python3", "/usr/local/bin/python3", "/opt/homebrew/bin/python3"} {
		if isExecutablePython(candidate) && supportsQGISModule(candidate) {
			return candidate
		}
	}
	for _, candidate := range []string{"python3", "python"} {
		found, err := exec.LookPath(candidate)
		if err != nil {
			continue
		}
		found = strings.TrimSpace(found)
		if fallback == "" {
			fallback = found
		}
		if supportsQGISModule(found) {
			return found
		}
	}
	return fallback
}

func isExecutablePython(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}
	info, err := os.Stat(trimmed)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}

func supportsQGISModule(pythonPath string) bool {
	if !isExecutablePython(pythonPath) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, pythonPath, "-c", "import qgis.core")
	return cmd.Run() == nil
}
