package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/cloudwego/eino/schema"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func (s *DeepServer) dataService(ctx context.Context, sessionID string) (*dataservice.Service, error) {
	session, ok := s.flowContext(sessionID)
	if !ok {
		return nil, fmt.Errorf("当前会话上下文不可用")
	}
	ctx = withToolSessionContext(ctx, sessionID, session.ProjectPath, session.InstanceID)
	dir, err := config.ResolveSessionDir(session.ProjectPath, sessionID)
	if err != nil {
		return nil, err
	}
	binding := flowRuntimeBinding{server: s}
	engine, err := binding.acquisitionEngine(ctx, &flowrun.RunState{SessionID: sessionID, ProjectRoot: session.ProjectPath})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	configuration := s.configService
	s.mu.Unlock()
	if configuration == nil {
		return nil, fmt.Errorf("数据获取设置不可用")
	}
	cfg := configuration.GetAcquisitionConfig()
	integrations, err := configuration.GetIntegrationsConfig()
	if err != nil {
		return nil, err
	}
	env := datasource.PlanningEnvironment{Catalog: engine.Catalog, AutoAcquire: "allow", Integrations: map[string]bool{"osm": integrations.OSM.Enabled, "gee": integrations.GEE.Enabled, "browser": integrations.Browser.Enabled, "web": integrations.Web.Enabled}, Secrets: map[string]bool{}, WebSearch: cfg.WebSearchEnabled, MaxDownloadMB: cfg.MaxDownloadMB}
	for _, name := range configuration.ConfiguredSecretNames() {
		env.Secrets[name] = true
	}
	memory, err := flow.DefaultDataMemory()
	if err != nil {
		return nil, err
	}
	provided, err := dataservice.LoadBindings(dir)
	if err != nil {
		return nil, err
	}
	if session.Session != nil {
		for _, message := range session.Session.GetMessages() {
			if message == nil || message.Role != schema.User {
				continue
			}
			raw, _ := json.Marshal(message.Extra["attachments"])
			var attachments []struct {
				Path     string `json:"path"`
				Name     string `json:"name"`
				Category string `json:"category"`
			}
			if json.Unmarshal(raw, &attachments) == nil {
				for _, attachment := range attachments {
					if attachment.Path != "" {
						name := attachment.Name
						if name == "" {
							name = filepath.Base(attachment.Path)
						}
						provided = append(provided, dataservice.Binding{Name: name, Path: attachment.Path, Category: attachment.Category})
					}
				}
			}
		}
	}
	selectedLayers := map[string]bool{}
	if session.Session != nil {
		messages := session.Session.GetMessages()
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			if m == nil || m.Role != schema.User {
				continue
			}
			raw, _ := json.Marshal(m.Extra["layers"])
			var refs []struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(raw, &refs)
			for _, r := range refs {
				selectedLayers[r.ID] = true
			}
			break
		}
	}
	options := dataservice.Options{SessionID: sessionID, SessionDir: dir, Environment: env, Engine: engine, Memory: memory, UserData: provided, Config: cfg}
	if session.InstanceID != "" {
		options.Layers = func(callCtx context.Context) ([]dataservice.Binding, error) {
			callCtx = withToolSessionContext(callCtx, sessionID, session.ProjectPath, session.InstanceID)
			value, err := binding.Invoke(callCtx, "get_layers_info", map[string]any{"include_invisible": true})
			if err != nil {
				return nil, err
			}
			layers, err := dataLayerBindings(value)
			for i := range layers {
				layers[i].Explicit = selectedLayers[layers[i].LayerID]
			}
			return layers, err
		}
		options.ResolveExtent = func(callCtx context.Context, value string) (*flow.ExtentPlan, error) {
			layers, err := options.Layers(callCtx)
			if err != nil {
				return nil, err
			}
			for _, layer := range layers {
				if layer.Name == value || layer.LayerID == value {
					if layer.Path != "" {
						return acquire.ResolveExtent(callCtx, layer.Path, engine.Catalog, nil)
					}
				}
			}
			return nil, fmt.Errorf("图层范围无法在本地解析：%s", value)
		}
	}
	return dataservice.New(options), nil
}

func (s *DeepServer) dataServiceForPlan(ctx context.Context, run *flowrun.RunState) (*dataservice.Service, error) {
	service, err := s.dataService(ctx, run.SessionID)
	if err != nil {
		return nil, err
	}
	type sample struct {
		bytes int64
		at    time.Time
		stage string
	}
	var mu sync.Mutex
	samples := map[string]sample{}
	return service.WithExecution(true, false, func(row string, p acquire.Progress) {
		now := time.Now()
		speed := p.BytesPerSecond
		mu.Lock()
		old, exists := samples[row]
		if speed == 0 && exists && old.stage == p.Stage && p.Bytes >= old.bytes && now.After(old.at) {
			speed = float64(p.Bytes-old.bytes) / now.Sub(old.at).Seconds()
		}
		samples[row] = sample{p.Bytes, now, p.Stage}
		mu.Unlock()
		percent := p.Percent
		if percent == nil && p.Total > 0 && p.Bytes >= 0 && p.Bytes <= p.Total {
			v := 100 * float64(p.Bytes) / float64(p.Total)
			percent = &v
		}
		event := flowrun.Event{Event: "flow.acquisition_progress", SessionID: run.SessionID, FlowRunID: run.ID, Payload: map[string]any{"row": row, "stage": p.Stage, "bytes": p.Bytes, "total": p.Total, "percent": percent, "bytes_per_second": speed, "message": p.Message}}
		if run.CartoPlan != nil {
			event.PlanID = run.CartoPlan.ID
			event.PlanVersion = run.CartoPlan.Version
			event.Payload["plan_id"] = event.PlanID
			event.Payload["plan_version"] = event.PlanVersion
		}
		(flowRuntimeBinding{server: s}).Emit(ctx, event)
	}), nil
}
func (s *DeepServer) withDataService(ctx context.Context, sessionID string) context.Context {
	service, err := s.dataService(ctx, sessionID)
	if err != nil {
		return ctx
	}
	return dataservice.WithService(ctx, service)
}
func dataLayerBindings(value any) ([]dataservice.Binding, error) {
	payload, err := acquire.UnwrapToolResult(value)
	if err != nil {
		return nil, err
	}
	if info, ok := payload["info"].(map[string]any); ok {
		payload = info
	}
	raw, _ := json.Marshal(payload["layers"])
	var layers []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Source   string `json:"source"`
		Type     string `json:"type"`
		Geometry string `json:"geometry_type"`
		Fields   []struct {
			Name string `json:"name"`
		} `json:"fields"`
	}
	if err = json.Unmarshal(raw, &layers); err != nil {
		return nil, err
	}
	out := []dataservice.Binding{}
	for _, layer := range layers {
		geometry := strings.ToLower(layer.Geometry)
		switch {
		case strings.Contains(geometry, "point"):
			geometry = "point"
		case strings.Contains(geometry, "line"):
			geometry = "line"
		case strings.Contains(geometry, "polygon"):
			geometry = "polygon"
		case layer.Type == "raster":
			geometry = "raster"
		}
		b := dataservice.Binding{Name: layer.Name, LayerID: layer.ID, Geometry: geometry}
		path := strings.Split(layer.Source, "|")[0]
		if strings.HasPrefix(path, "/") {
			b.Path = path
		}
		for _, field := range layer.Fields {
			b.Fields = append(b.Fields, field.Name)
		}
		out = append(out, b)
	}
	return out, nil
}
