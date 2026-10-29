package websocket

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/osmdata"
)

// Installed before SetPlanRuntime; background acquisition follows application
// shutdown, while Controller.Cancel cancels the individual flow immediately.
func (s *DeepServer) SetFlowLifecycleContext(ctx context.Context) { s.flowLifecycle = ctx }
func (binding flowRuntimeBinding) acquisitionEngine(ctx context.Context, run *flowrun.RunState) (*acquire.Engine, error) {
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, err
	}
	catalog, err := datasource.Load(filepath.Join(assets, "tools", "system", "data-acquisition", "assets"), filepath.Join(home, "sources"))
	if err != nil {
		return nil, err
	}
	binding.server.mu.Lock()
	service := binding.server.configService
	binding.server.mu.Unlock()
	if service == nil {
		return nil, fmt.Errorf("数据获取配置服务不可用")
	}
	cfg := service.GetAcquisitionConfig()
	integrations, err := service.GetIntegrationsConfig()
	if err != nil {
		return nil, err
	}
	invoke := func(ctx context.Context, name string, args map[string]any) (any, error) {
		ctx = binding.server.withOSMProgress(ctx, nil, run.SessionID, modeStatePhaseBuilding)
		if session, ok := binding.server.flowContext(run.SessionID); ok {
			ctx = withToolSessionContext(ctx, run.SessionID, session.ProjectPath, session.InstanceID)
		}
		builder, _, err := binding.server.flowBuilder()
		if err != nil {
			return nil, err
		}
		invoker, ok := builder.(interface {
			InvokeAcquisitionTool(context.Context, string, map[string]any) (any, error)
		})
		if !ok {
			return nil, fmt.Errorf("数据获取工具通道不可用")
		}
		return invoker.InvokeAcquisitionTool(ctx, name, args)
	}
	options := osmdata.Options{Enabled: integrations.OSM.Enabled, Authorized: true, Endpoints: integrations.OSM.Endpoints, NominatimURL: integrations.OSM.NominatimURL, ProxyURL: integrations.OSM.ProxyURL, TimeoutSeconds: integrations.OSM.TimeoutSeconds, TotalTimeoutSeconds: integrations.OSM.TotalTimeoutSeconds, MaxAttempts: integrations.OSM.MaxAttempts, MaxResponseMB: integrations.OSM.MaxResponseMB, ProjectDir: run.ProjectRoot}
	if cfg.OverpassEndpoint != "" {
		options.Endpoints = []string{cfg.OverpassEndpoint}
	}
	osmInvoke := func(ctx context.Context, name string, args map[string]any, options osmdata.Options) (*osmdata.Result, error) {
		ctx = binding.server.withOSMProgress(ctx, nil, run.SessionID, modeStatePhaseBuilding)
		return osmdata.Invoke(ctx, name, args, options)
	}
	adapters := []acquire.Adapter{acquire.LocalAdapter{}, acquire.OfflineAdapter{Invoke: invoke}, acquire.HTTPAdapter{MaxBytes: int64(cfg.MaxDownloadMB) * 1024 * 1024, ProxyURL: integrations.OSM.ProxyURL}, acquire.OverpassAdapter{Options: options, Catalog: catalog, Invoke: osmInvoke}, acquire.TiandituAdapter{Invoke: invoke, ConfigDir: filepath.Join(home, "config")}, acquire.GEEAdapter{Invoke: invoke, AssetsRoot: assets}}
	engine := &acquire.Engine{Catalog: catalog, Store: acquire.NewStore(filepath.Join(home, "data", "acquired"), int64(cfg.StoreMaxGB*1024*1024*1024)), Invoke: invoke, Config: cfg, Adapters: map[string]acquire.Adapter{}}
	for _, adapter := range adapters {
		engine.Adapters[adapter.Kind()] = adapter
	}
	return engine, nil
}
