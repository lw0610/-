package websocket

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/dataservice"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/resource"
)

func TestV7PrecheckRejectsUnresolvedStaticRegion(t *testing.T) {
	for _, upstream := range []bool{false, true} {
		t.Run(map[bool]string{false: "unresolved region", true: "valid upstream dependency"}[upstream], func(t *testing.T) {
			s, factory, run, p := cartoRuntimeFixture(t)
			s.configService = config.NewConfigService(nil, &config.Config{Integrations: config.IntegrationsConfig{OSM: config.OSMConfig{Enabled: true}}}, "", filepath.Join(t.TempDir(), "config.yaml"))
			extent := &plan.Extent{Region: "河南省、湖北省、陕西省、甘肃省、宁夏回族自治区"}
			p.Steps[0].Inputs = []string{"river"}
			if upstream {
				extent = &plan.Extent{From: "route_bounds", Expand: .2}
				p.Steps = append([]plan.Step{{ID: "prepare", Title: "生成范围", Capabilities: []string{"read"}, Produces: []plan.Product{{ID: "route_bounds", Kind: "file", Format: "geojson"}}}}, p.Steps...)
			}
			p.Data = []plan.Data{{ID: "river", Name: "沿线河流", Source: "retrieve", Kind: "layer", Geometry: "line", Need: &plan.Need{Category: "hydro.river_line"}, Extent: extent}}
			prepared, err := s.prepareCartographicPlan(context.Background(), run, p, nil)
			if err != nil {
				t.Fatal(err)
			}
			check := prepared.Snapshot.Precheck[0]
			if upstream {
				if check.Status != "available" || !strings.Contains(check.Reason, "上游") {
					t.Fatalf("valid delayed extent rejected: %+v", check)
				}
			} else if prepared.Snapshot.Status != "needs_data" || check.Status != "unavailable" || !strings.Contains(check.Reason, "无法解析指定地区") {
				t.Fatalf("unresolved extent falsely available: %+v", prepared.Snapshot)
			}
			if len(factory.roles) != 0 {
				t.Fatal("precheck invoked a model")
			}
		})
	}
}

func TestV7PrecheckAndExecutionUseLinkedMainMapWidth(t *testing.T) {
	s, _, run, p := cartoRuntimeFixture(t)
	s.configService = config.NewConfigService(nil, &config.Config{Integrations: config.IntegrationsConfig{OSM: config.OSMConfig{Enabled: true}}}, "", filepath.Join(t.TempDir(), "config.yaml"))
	p.Guide = &plan.GuideRef{ID: "marching-route"}
	p.Facts = []plan.Fact{{Name: "纸张", Value: "A4 横向", From: "用户"}}
	p.Data = []plan.Data{{ID: "rail", Name: "区域铁路", Source: "retrieve", Kind: "layer", Geometry: "line", Need: &plan.Need{Category: "transport.railway"}, Extent: &plan.Extent{BBox: []float64{110, 25, 114, 29}}}}
	p.Steps[0].Inputs = []string{"rail"}
	prepared, err := s.prepareCartographicPlan(context.Background(), run, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	var resources []*resource.ResolvedResource
	for _, r := range prepared.Executable.Resources {
		resources = append(resources, r.ResolvedResource())
	}
	_, runtimeWidth, err := planMapFrame(p, resources)
	if err != nil || math.Abs(runtimeWidth-252.45) > .0001 {
		t.Fatalf("execution frame=%f err=%v", runtimeWidth, err)
	}
	dir, err := config.ResolveSessionDir(run.ProjectPath, run.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "data", "finds", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("precheck did not perform one local lookup: %v %v", paths, err)
	}
	var saved struct {
		Request dataservice.FindRequest `json:"request"`
		Result  dataservice.FindResult  `json:"result"`
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil || json.Unmarshal(raw, &saved) != nil {
		t.Fatal(err)
	}
	if math.Abs(saved.Request.Need.MapFrameWidthMM-runtimeWidth) > .0001 {
		t.Fatalf("preview and execution disagree: preview=%f runtime=%f", saved.Request.Need.MapFrameWidthMM, runtimeWidth)
	}
	wantScale := acquire.EstimateScale(&datasource.ExtentPlan{BBox: p.Data[0].Extent.BBox}, map[string]any{"map_frame_width_mm": runtimeWidth})
	if saved.Result.Resolved.ScaleDenominator != wantScale || wantScale <= 500000 {
		t.Fatalf("wrong real-frame scale: actual=%d want=%d", saved.Result.Resolved.ScaleDenominator, wantScale)
	}
}
