package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/testutil"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestV4BuiltinZttztRoleMatrix(t *testing.T) {
	a, manager, _, _ := realFlowAPI(t)
	var data config.AgentConfigData
	if err := yaml.Unmarshal([]byte(testutil.V4ZttztProfileYAML), &data); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveConfig("", "zttzt-fixture", &data); err != nil {
		t.Fatal(err)
	}
	compiler, err := a.FlowCompiler()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"marching-route", "statistical-choropleth", "traffic-route", "data-to-map"} {
		p, err := compiler.PlanReadOnly(context.Background(), id, "zttzt-fixture")
		if err != nil {
			t.Fatal(err)
		}
		errors, visionWarnings := 0, 0
		for _, d := range p.Diagnostics {
			if d.Level == "error" {
				errors++
			}
			if d.Level == "warning" && d.Code == "FLOW_ROLE_VISION_UNAVAILABLE" {
				visionWarnings++
			}
		}
		expectedVision := 0
		if id == "marching-route" {
			expectedVision = 2
		}
		if errors != 0 || visionWarnings != expectedVision {
			t.Fatalf("%s: %+v", id, p.Diagnostics)
		}
		for _, review := range []bool{false, true} {
			issues := flow.RoleIssues(p, map[string]any{"质量审查": review}, nil)
			blocked := 0
			for _, d := range issues {
				if d.Level == "error" {
					blocked++
				}
			}
			expected := 0
			if id == "marching-route" && review {
				expected = 2
			}
			if blocked != expected {
				t.Fatalf("%s review=%v: %+v", id, review, issues)
			}
		}
	}
}

func TestV4RoleMappingAPIChangesOnlyProfile(t *testing.T) {
	a, manager, _, _ := realFlowAPI(t)
	store, err := a.flowStore()
	if err != nil {
		t.Fatal(err)
	}
	raw := "---\n名称: 对应测试\n---\n## 参与角色\n### 工程师\n- 工具包：data-loading\n## 步骤\n### 加载\n- 负责：工程师\n- 做法：加载\n- 完成标准：完成\n"
	item, err := store.Create(raw, "v4-role-api")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(item.Dir, "FLOW.md"))
	if err != nil {
		t.Fatal(err)
	}
	compiler, _ := a.FlowCompiler()
	initial, err := compiler.PlanReadOnly(context.Background(), item.Meta.ID, "compile-scheme")
	if err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	a.SetupRoutes(router)
	for i, body := range []string{`{"mapping":{"工程师":"data-renamed"}}`, `{"mapping":{}}`} {
		req := httptest.NewRequest("PUT", "/api/flows/"+item.Meta.ID+"/roles?scheme=compile-scheme", bytes.NewBufferString(body))
		req.Header.Set("X-AutoCarto-Contract-Version", "v1")
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatal(rr.Code, rr.Body.String())
		}
		var response struct {
			Data struct {
				Slots []flow.RoleSlotPlan `json:"role_slots"`
			}
		}
		if err = json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		source := response.Data.Slots[0].Resolution.Source
		if i == 0 && source != "mapping" {
			t.Fatal(source)
		}
		if i == 1 && source == "mapping" {
			t.Fatal("automatic mapping not restored")
		}
		plan, err := compiler.PlanReadOnly(context.Background(), item.Meta.ID, "compile-scheme")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && (plan.SchemeRevision == initial.SchemeRevision || plan.CompileKey == initial.CompileKey) {
			t.Fatal("mapping did not invalidate plan")
		}
	}
	after, _ := os.ReadFile(filepath.Join(item.Dir, "FLOW.md"))
	if !bytes.Equal(before, after) {
		t.Fatal("mapping changed FLOW.md")
	}
	profile, err := manager.GetConfigData("compile-scheme")
	if err != nil || profile.Flows != nil {
		t.Fatal("policy disabled flows", err)
	}
}
