package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/datasource"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/gorilla/mux"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type rejectRequestTransport struct{ calls *atomic.Int32 }

func (t rejectRequestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, fmt.Errorf("request guard: network forbidden")
}
func TestV4ReadAndCheckEndpointsNeverCallNetworkOrModel(t *testing.T) {
	a, manager, _, home := realFlowAPI(t)
	cs := config.NewConfigService(nil, &config.Config{}, "", filepath.Join(home, "config", "config.yaml"))
	a.configService = cs
	a.settingsMutationService = control.NewSettingsMutationService(control.SettingsMutationServiceDeps{ConfigService: cs, AgentConfigManager: manager})
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	library, err := a.resourceStore()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\n名称: 离线请求守卫\n---\n## 需要准备\n- 铁路（必填，图层）\n- 范围（必填，文本）\n## 参与角色\n### 工程师\n- 工具包：data-loading\n## 数据需求\n### 铁路\n- 类别：transport.railway\n- 范围：「范围」\n## 步骤\n### 加载\n- 负责：工程师\n- 做法：读取「铁路」，范围「范围」\n- 完成标准：完成\n"
	item, err := store.Create(raw, "request-guard")
	if err != nil {
		t.Fatal(err)
	}
	var networkCalls, modelCalls atomic.Int32
	original := http.DefaultTransport
	http.DefaultTransport = rejectRequestTransport{&networkCalls}
	t.Cleanup(func() { http.DefaultTransport = original })
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(home, "cache"), LoadEnvironment: func(ctx context.Context, scheme string) (flow.Environment, error) {
		env, err := a.loadFlowEnvironment(ctx, scheme)
		env.Model = apiFlowModelFunc(func(context.Context, flow.ModelRequest) (string, error) {
			modelCalls.Add(1)
			return "", fmt.Errorf("request guard: model forbidden")
		})
		env.ModelName = "forbidden-fixture"
		return env, err
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	profilePath := filepath.Join(home, "config", "agent_configs", "compile-scheme", "agent.yaml")
	before, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "rail.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	dataInfo, err := os.Stat(filepath.Join(data, "rail.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/flows/" + item.Meta.ID + "?scheme=compile-scheme", nil},
		{"POST", "/api/flows/" + item.Meta.ID + "/inputs/check", map[string]any{"scheme": "compile-scheme", "values": map[string]any{"铁路": map[string]any{"$acquire": true}, "范围": "北京市"}}},
		{"GET", "/api/flows/" + item.Meta.ID + "/roles?scheme=compile-scheme", nil},
		{"GET", "/api/data-sources", nil}, {"POST", "/api/data-sources/scan", map[string]any{"path": data}}, {"GET", "/api/data-store", nil}, {"GET", "/api/acquisition/config", nil},
	} {
		rr := acquisitionRequest(t, router, request.method, request.path, request.body)
		if rr.Code != 200 {
			t.Fatalf("%s %s: %d %s", request.method, request.path, rr.Code, rr.Body.String())
		}
	}
	after, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("planning wrote role overlay to stored scheme")
	}
	freshData, err := os.Stat(filepath.Join(data, "rail.geojson"))
	if err != nil {
		t.Fatal(err)
	}
	if !freshData.ModTime().Equal(dataInfo.ModTime()) {
		t.Fatal("scanning modified user data")
	}
	rr := acquisitionRequest(t, router, "PUT", "/api/flows/"+item.Meta.ID+"/roles?scheme=compile-scheme", map[string]any{"mapping": map[string]string{"工程师": "flow_role"}})
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	// Settings mutations are local too, including confirmation-protected deletes.
	source := datasource.Source{Name: "guard-source", Kind: "local_folder", Root: data, Provides: []datasource.Provide{{Category: "transport.railway", Path: "rail.geojson"}}}
	rr = acquisitionRequest(t, router, "POST", "/api/data-sources", source)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var saved struct {
		Data struct {
			Source datasource.Source `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	source.Name = "changed-locally"
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{"PUT", "/api/data-sources/" + saved.Data.Source.ID, source},
		{"DELETE", "/api/data-sources/" + saved.Data.Source.ID + "?confirm=true", nil},
		{"PUT", "/api/acquisition/config", config.AcquisitionConfig{}.WithDefaults()},
		{"POST", "/api/data-store/clean-expired?confirm=true", nil},
	} {
		rr = acquisitionRequest(t, router, request.method, request.path, request.body)
		if rr.Code != 200 {
			t.Fatalf("%s %s: %d %s", request.method, request.path, rr.Code, rr.Body.String())
		}
	}
	cache, _, err := a.dataStore()
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := cache.Materialize(context.Background(), acquire.CacheSpec{SourceID: "source.guard", Category: "transport.railway"}, 1, func(dir string) (acquire.Manifest, error) {
		if err := os.WriteFile(filepath.Join(dir, "out", "fixture.geojson"), []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
			return acquire.Manifest{}, err
		}
		return acquire.Manifest{OutputFiles: []string{"out/fixture.geojson"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rr = acquisitionRequest(t, router, "DELETE", "/api/data-store/"+record.Key+"?confirm=true", nil)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if networkCalls.Load() != 0 || modelCalls.Load() != 0 {
		t.Fatalf("request path invoked network=%d model=%d", networkCalls.Load(), modelCalls.Load())
	}
}
