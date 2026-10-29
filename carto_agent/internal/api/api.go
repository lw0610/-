package api

import (
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/discovery"
	"github.com/autocarto/carto_agent/internal/flow"
	bridgeprovider "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/settings"
)

// SessionService 会话操作接口（与控制面广播职责解耦）。
type SessionService interface {
	SaveSession(sessionData map[string]any, projectPath string) error
	SaveSessionWithOperation(sessionData map[string]any, projectPath string, operation map[string]any) error
	LoadSession(sessionID, projectPath string) (map[string]any, error)
	ListSessions(projectPath string) ([]map[string]any, error)
	ListProjects() ([]map[string]any, error)
	DeleteSession(sessionID, projectPath string) error
	InitSessionDir(projectPath, sessionID string) (map[string]string, error)
	GetSessionDir(projectPath, sessionID string) (string, error)
	SaveSessionAtomically(sessionID, projectPath string, updater func(map[string]any) error) (map[string]any, error)
}

// SkillService 技能操作接口（用于默认装配 control 服务）。
type SkillService interface {
	ListSkillPackages() ([]settings.SkillPackageSummary, error)
	GetSkillPackage(name string) (settings.SkillPackageDetail, error)
	GetSkillPackageReferences(name string) []config.SkillReference
	SetSkillPackageEnabled(name string, enabled bool) error
	ListSkills() ([]map[string]any, error)
	GetSkillContent(name string) (map[string]any, error)
	SaveSkill(name, description, content string) error
	DeleteSkill(name string) error
	ImportSkill(sourcePath string, overwrite bool) (map[string]any, error)
}

// API REST API 处理器。
type API struct {
	planService             PlanService
	guidePackages           *flow.GuideStore
	flowPackages            *flow.Store
	flowEnvironments        flowEnvironmentCache
	resourceLibrary         *resource.Library
	resourceLibraryMu       sync.Mutex
	configService           *config.ConfigService
	sessionService          SessionService
	router                  *mux.Router
	instanceRegistry        *discovery.InstanceRegistry
	serviceRegistry         *discovery.ServiceRegistry
	localBridgeProvider     *bridgeprovider.Provider
	runtimeCoordinator      control.RuntimeCoordinator
	settingsMutationService control.SettingsMutationService
	assetCatalogService     control.AssetCatalogService
	realtimeHub             control.RealtimeHub
}

// NewAPI 创建 API 实例，并基于原始依赖装配默认 control 服务。
func NewAPI(
	cs *config.ConfigService,
	acm *config.AgentConfigManager,
	ss SessionService,
	ks SkillService,
	skillsDir string,
	ir *discovery.InstanceRegistry,
) *API {
	a := &API{
		configService:    cs,
		sessionService:   ss,
		instanceRegistry: ir,
		realtimeHub:      control.NewNoopRealtimeHub(),
	}
	a.configureDefaultControlServices(acm, ss, ks, skillsDir)
	return a
}

// NewAPIWithServices 使用外部装配好的 control 服务创建 API。
func NewAPIWithServices(
	cs *config.ConfigService,
	ss SessionService,
	ir *discovery.InstanceRegistry,
	sr *discovery.ServiceRegistry,
	runtimeCoordinator control.RuntimeCoordinator,
	settingsService control.SettingsMutationService,
	assetCatalog control.AssetCatalogService,
	realtimeHub control.RealtimeHub,
) *API {
	if realtimeHub == nil {
		realtimeHub = control.NewNoopRealtimeHub()
	}
	return &API{
		configService:           cs,
		sessionService:          ss,
		instanceRegistry:        ir,
		serviceRegistry:         sr,
		runtimeCoordinator:      runtimeCoordinator,
		settingsMutationService: settingsService,
		assetCatalogService:     assetCatalog,
		realtimeHub:             realtimeHub,
	}
}

func (a *API) configureDefaultControlServices(acm *config.AgentConfigManager, ss SessionService, ks SkillService, skillsDir string) {
	skillCatalog := ks
	if skillCatalog == nil && strings.TrimSpace(skillsDir) != "" {
		builtinSkillsDir := config.ResolveBuiltinSkillsDir()
		builtinToolsDir := config.ResolveBuiltinToolsDir()
		userToolsDir := settings.InferUserToolsDirFromSkillsDir(skillsDir)
		configBaseDir := ""
		activeConfigName := ""
		globalSkillsDir := ""
		if acm != nil {
			configBaseDir = acm.ConfigBaseDir()
			activeConfigName = acm.GetActiveConfigName()
			globalSkillsDir = config.ResolveGlobalSkillsDir(configBaseDir)
		}
		registryGovernance := control.NewRegistryGovernance(control.RegistryGovernanceDeps{
			ResolveToolDirs: func() []string {
				return config.ResolveRuntimeToolDirs(builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
			},
			ResolveSkillDirs: func() []string {
				return config.ResolveRuntimeSkillDirs(builtinSkillsDir, skillsDir, configBaseDir, activeConfigName)
			},
		})
		skillCatalog = settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
			BuiltinSkillsDir: builtinSkillsDir,
			UserSkillsDir:    skillsDir,
			GlobalSkillsDir:  globalSkillsDir,
			BuiltinToolsDir:  builtinToolsDir,
			UserToolsDir:     userToolsDir,
			ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
				return registryGovernance
			},
			ResolveRuntimeToolDirs: func() []string {
				return config.ResolveRuntimeToolDirs(builtinToolsDir, userToolsDir, configBaseDir, activeConfigName)
			},
			ResolveRuntimeSkillDirs: func() []string {
				return config.ResolveRuntimeSkillDirs(builtinSkillsDir, skillsDir, configBaseDir, activeConfigName)
			},
			ResolveActiveConfigName: func() string {
				if acm == nil {
					return ""
				}
				return acm.GetActiveConfigName()
			},
			ResolveExclusiveSkillDir: func(configName string) string {
				if acm == nil {
					return ""
				}
				return acm.ExclusiveSkillsDir(configName)
			},
		}, acm)
	}

	var runtimeSource control.RuntimeSource
	if provider, ok := any(ss).(control.RuntimeSource); ok {
		runtimeSource = provider
	}
	var runtimeController settings.RuntimeController
	if provider, ok := any(ss).(settings.RuntimeController); ok {
		runtimeController = provider
	}
	var runtimeDiagnostics settings.RuntimeDiagnosticsService
	if provider, ok := any(ss).(settings.RuntimeDiagnosticsService); ok {
		runtimeDiagnostics = provider
	} else if provider, ok := any(runtimeController).(settings.RuntimeDiagnosticsService); ok {
		runtimeDiagnostics = provider
	}
	var toolAggregatorSource control.ToolAggregatorSource
	if provider, ok := any(ss).(control.ToolAggregatorSource); ok {
		toolAggregatorSource = provider
	}
	var activeRuns control.ActiveRunInspector
	if provider, ok := any(ss).(control.ActiveRunInspector); ok {
		activeRuns = provider
	}

	a.runtimeCoordinator = control.NewRuntimeCoordinator(control.RuntimeCoordinatorDeps{
		AgentConfigManager: acm,
		RuntimeSource:      runtimeSource,
		RuntimeDiagnostics: runtimeDiagnostics,
		InstanceRegistryProvider: func() *discovery.InstanceRegistry {
			return a.instanceRegistry
		},
	})
	a.settingsMutationService = control.NewSettingsMutationService(control.SettingsMutationServiceDeps{
		ConfigService:      a.configService,
		AgentConfigManager: acm,
		RuntimeController:  runtimeController,
		SkillCatalog:       skillCatalog,
		ActiveRuns:         activeRuns,
	})
	a.assetCatalogService = control.NewAssetCatalogService(control.AssetCatalogServiceDeps{
		AgentConfigManager:   acm,
		SkillCatalog:         skillCatalog,
		RuntimeCoordinator:   a.runtimeCoordinator,
		RuntimeDiagnostics:   runtimeDiagnostics,
		ToolAggregatorSource: toolAggregatorSource,
	})
}

func (a *API) SetLocalBridgeProvider(provider *bridgeprovider.Provider) {
	a.localBridgeProvider = provider
	a.resourceLibraryMu.Lock()
	library := a.resourceLibrary
	a.resourceLibraryMu.Unlock()
	if library != nil {
		a.queueResourcePreviews(library)
	}
}

// SetRealtimeHub 设置实时广播服务。
func (a *API) SetRealtimeHub(hub control.RealtimeHub) {
	if hub == nil {
		a.realtimeHub = control.NewNoopRealtimeHub()
		return
	}
	a.realtimeHub = hub
}

// corsMiddleware CORS 中间件。
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+httpContractVersionHeader)
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAllowedOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == "tauri://localhost" {
		return true
	}
	if origin == "https://tauri.localhost" {
		return true
	}
	if strings.HasPrefix(origin, "http://localhost") {
		return true
	}
	if strings.HasPrefix(origin, "http://127.0.0.1") {
		return true
	}
	return false
}
