package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/compute"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	"github.com/autocarto/carto_agent/internal/middlewareclient"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
	"github.com/autocarto/carto_agent/internal/toolcatalog"
	"github.com/autocarto/carto_agent/internal/toolrouter"
)

type fakeSkillService struct {
	packages map[string]settings.SkillPackageDetail
}

func (f *fakeSkillService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	result := make([]settings.SkillPackageSummary, 0, len(f.packages))
	for _, pkg := range f.packages {
		result = append(result, pkg.Summary())
	}
	return result, nil
}

func (f *fakeSkillService) GetSkillPackage(name string) (settings.SkillPackageDetail, error) {
	pkg, ok := f.packages[name]
	if !ok {
		return settings.SkillPackageDetail{}, &settings.SkillPackageNotFoundError{PackageID: name}
	}
	return pkg, nil
}

func (f *fakeSkillService) GetSkillPackageReferences(name string) []config.SkillReference {
	_ = name
	return nil
}

func (f *fakeSkillService) SetSkillPackageEnabled(name string, enabled bool) error {
	pkg, ok := f.packages[name]
	if !ok {
		return &settings.SkillPackageNotFoundError{PackageID: name}
	}
	pkg.Enabled = enabled
	f.packages[name] = pkg
	return nil
}

func (f *fakeSkillService) ListSkills() ([]map[string]any, error) { return nil, nil }
func (f *fakeSkillService) GetSkillContent(name string) (map[string]any, error) {
	_ = name
	return nil, nil
}
func (f *fakeSkillService) SaveSkill(name, description, content string) error {
	_, _, _ = name, description, content
	return nil
}
func (f *fakeSkillService) DeleteSkill(name string) error { _ = name; return nil }
func (f *fakeSkillService) ImportSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	_, _ = sourcePath, overwrite
	return nil, nil
}

func categoryPackageDetail(packageID, backend, source string, toolNames ...string) settings.SkillPackageDetail {
	tools := make([]skill.SkillDefinition, 0, len(toolNames))
	for _, toolName := range toolNames {
		tools = append(tools, skill.SkillDefinition{
			Metadata: skill.SkillMetadata{Name: toolName, Backend: backend},
			Spec:     skill.SkillSpec{Description: toolName + " desc"},
		})
	}
	return settings.SkillPackageDetail{
		SkillPackageSummary: settings.SkillPackageSummary{
			PackageID: packageID,
			Name:      packageID,
			Kind:      skill.SkillPackageKindCategory,
			Backend:   backend,
			Source:    source,
			Enabled:   true,
			ToolCount: len(tools),
			HasTools:  len(tools) > 0,
		},
		Tools: tools,
	}
}

type fakeToolSessionService struct {
	snapshot        toolrouter.Snapshot
	runtimeSnapshot middlewareclient.RuntimeSnapshot
}

type toolPackageFixture struct {
	packageName string
	backend     string
	toolNames   []string
}

type stubAPIAssetCatalogService struct {
	view       control.ToolCatalogView
	err        error
	blockUntil <-chan struct{}
}

func (s stubAPIAssetCatalogService) SettingsWorkspace() map[string]any { return nil }

func (s stubAPIAssetCatalogService) ListAgentConfigs(string) ([]config.StoredAgentConfig, error) {
	return nil, nil
}

func (s stubAPIAssetCatalogService) GetAgentConfig(string, string) (*config.StoredAgentConfig, error) {
	return nil, nil
}

func (s stubAPIAssetCatalogService) GetAgentProfile(string) (settings.AgentProfileView, error) {
	return settings.AgentProfileView{}, nil
}

func (s stubAPIAssetCatalogService) GetAgent(string, string) (config.AgentDefData, error) {
	return config.AgentDefData{}, nil
}

func (s stubAPIAssetCatalogService) GetToolCatalog(ctx context.Context, _ string) (control.ToolCatalogView, error) {
	if s.blockUntil != nil {
		select {
		case <-s.blockUntil:
		case <-ctx.Done():
			return control.ToolCatalogView{}, ctx.Err()
		}
	}
	return s.view, s.err
}

func (s stubAPIAssetCatalogService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	return nil, nil
}

func (s stubAPIAssetCatalogService) GetSkillPackage(string) (settings.SkillPackageDetail, error) {
	return settings.SkillPackageDetail{}, nil
}

func (s stubAPIAssetCatalogService) GetSkillPackageReferences(string) []config.SkillReference {
	return nil
}

func (s stubAPIAssetCatalogService) ListSkills() ([]map[string]any, error) { return nil, nil }

func (s stubAPIAssetCatalogService) GetSkillContent(string) (map[string]any, error) {
	return nil, nil
}

func (s stubAPIAssetCatalogService) GetSkillReferences(string) []config.SkillReference {
	return nil
}

func (f *fakeToolSessionService) SaveSession(sessionData map[string]any, projectPath string) error {
	_ = sessionData
	_ = projectPath
	return nil
}

func (f *fakeToolSessionService) SaveSessionWithOperation(sessionData map[string]any, projectPath string, operation map[string]any) error {
	_ = sessionData
	_ = projectPath
	_ = operation
	return nil
}

func (f *fakeToolSessionService) LoadSession(sessionID, projectPath string) (map[string]any, error) {
	_ = sessionID
	_ = projectPath
	return map[string]any{}, nil
}

func (f *fakeToolSessionService) ListSessions(projectPath string) ([]map[string]any, error) {
	_ = projectPath
	return []map[string]any{}, nil
}

func (f *fakeToolSessionService) ListProjects() ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (f *fakeToolSessionService) DeleteSession(sessionID, projectPath string) error {
	_ = sessionID
	_ = projectPath
	return nil
}

func (f *fakeToolSessionService) NotifySessionChange(changeType, sessionID string, extra map[string]any) {
	_ = changeType
	_ = sessionID
	_ = extra
}

func (f *fakeToolSessionService) InitSessionDir(projectPath, sessionID string) (map[string]string, error) {
	_ = projectPath
	_ = sessionID
	return map[string]string{}, nil
}

func (f *fakeToolSessionService) GetSessionDir(projectPath, sessionID string) (string, error) {
	_ = projectPath
	_ = sessionID
	return "", nil
}

func (f *fakeToolSessionService) SaveSessionAtomically(
	sessionID,
	projectPath string,
	updater func(map[string]any) error,
) (map[string]any, error) {
	_ = sessionID
	_ = projectPath
	state := map[string]any{}
	if updater != nil {
		if err := updater(state); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (f *fakeToolSessionService) GetToolAggregatorSnapshot() toolrouter.Snapshot {
	if f == nil {
		return toolrouter.Snapshot{}
	}
	return f.snapshot
}

func (f *fakeToolSessionService) GetRuntimeSnapshot() middlewareclient.RuntimeSnapshot {
	if f == nil {
		return middlewareclient.RuntimeSnapshot{}
	}
	return f.runtimeSnapshot
}

func (f *fakeToolSessionService) GetRuntimeState() middlewareclient.RuntimeState {
	if f == nil {
		return middlewareclient.RuntimeState{}
	}
	return middlewareclient.RuntimeState{Snapshot: &f.runtimeSnapshot}
}

func (f *fakeToolSessionService) GetRuntimeRevision() string {
	return "runtime-rev-test"
}

func (f *fakeToolSessionService) GetRuntimeActiveConfigName() string {
	return "default"
}

func (f *fakeToolSessionService) GetRuntimeModeIssues() map[string]SettingsModeIssue {
	return nil
}

func (f *fakeToolSessionService) GetToolSchemaRevision() string {
	return "schema-rev-test"
}

func (f *fakeToolSessionService) GetToolAvailabilityRevision() string {
	return "availability-rev-test"
}

func (f *fakeToolSessionService) GetLastRuntimeRebuildReason() string {
	return "tool_schema_changed:schema-rev-test"
}

func (f *fakeToolSessionService) GetLastRuntimeRebuildAt() time.Time {
	return time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)
}

func (f *fakeToolSessionService) GetPlannerReadyReason() string {
	return "planner key missing"
}

func (f *fakeToolSessionService) GetActiveInstanceID() string {
	return "instance-1"
}

func buildToolTestRouterWithPackages(t *testing.T, ss SessionService, packages []toolPackageFixture) (*mux.Router, *config.AgentConfigManager) {
	t.Helper()

	rootDir := t.TempDir()
	t.Setenv("AUTOCARTO_PROJECT_ROOT", rootDir)
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	skillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	// Read the canonical tool definitions into this isolated asset tree. Empty
	// trees intentionally no longer fall back to stale embedded contracts.
	toolsDir := filepath.Join(rootDir, "autocarto", "tools", "system")
	if err := os.MkdirAll(toolsDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"project-state/open_project.yaml", "project-state/get_project_info.yaml", "project-state/get_layers_info.yaml", "pyqgis-scripting/pyqgis_get_context.yaml"} {
		source := filepath.Join(originalWD, "..", "..", "..", "autocarto", "tools", "system", filepath.FromSlash(relative))
		raw, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(toolsDir, filepath.Base(relative)), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	writeTestSkillPackage := func(packageName string, backend string, toolNames ...string) {
		t.Helper()
		packageDir := filepath.Join(skillsDir, packageName)
		if err := os.MkdirAll(packageDir, 0o755); err != nil {
			t.Fatalf("mkdir package dir: %v", err)
		}
		toolRefs := strings.Join(toolNames, "\n  - ")
		systemFlag := "false"
		if packageName == "project-state" {
			systemFlag = "true"
		}
		skillMarkdown := strings.TrimSpace(`---
name: `+packageName+`
description: `+packageName+` package
kind: category
backend: `+backend+`
system: `+systemFlag+`
enabled: true
tools:
  - `+toolRefs+`
---

`+packageName+` package`) + "\n"
		if err := os.WriteFile(filepath.Join(packageDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}

	packageNames := make([]string, 0, len(packages))
	for _, pkg := range packages {
		packageNames = append(packageNames, "\""+pkg.packageName+"\"")
		writeTestSkillPackage(pkg.packageName, pkg.backend, pkg.toolNames...)
	}
	skillPackagesYAML := "[]"
	if len(packageNames) > 0 {
		skillPackagesYAML = "[" + strings.Join(packageNames, ", ") + "]"
	}

	defaultConfig := `version: "2.0"
runtime:
  shared_tool_packages: ` + skillPackagesYAML + `
  agents:
    orchestrator:
      display_name: 编排智能体
      description: tool test orchestrator
      is_main: true
      enabled: true
      skill_policy:
        private: {}
        share: {}
      prompts:
        static_instructions: test
        user_prompt_template: "{{ user_input }}"
plan:
  max_iterations: 100
  global:
	skill_packages: []
  agents:
    planner:
      display_name: Planner
      is_main: true
      mode: readonly_plus
      skill_policy:
        private: {}
        share: {}
      prompts:
        static_instructions: test
        runtime_mode_instruction: runtime
        user_prompt_template: "{{ user_input }}"
`
	if err := os.MkdirAll(filepath.Join(configDir, "tooltest"), 0o755); err != nil {
		t.Fatalf("mkdir tooltest config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "tooltest", "agent.yaml"), []byte(defaultConfig), 0o644); err != nil {
		t.Fatalf("write tooltest config: %v", err)
	}

	acm := config.NewAgentConfigManager(configDir)
	handler := NewAPI(nil, acm, ss, nil, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router, acm
}

func buildToolTestRouter(t *testing.T, ss SessionService) (*mux.Router, *config.AgentConfigManager) {
	t.Helper()
	return buildToolTestRouterWithPackages(t, ss, []toolPackageFixture{
		{packageName: "canvas-navigation", backend: "bridge", toolNames: []string{"open_project", "pyqgis_get_context"}},
		{packageName: "project-state", backend: "bridge", toolNames: []string{"get_project_info", "get_layers_info"}},
	})
}

func buildToolTestRouterWithAssetCatalog(t *testing.T, assetCatalog control.AssetCatalogService) *mux.Router {
	t.Helper()
	handler := NewAPIWithServices(nil, nil, nil, nil, nil, nil, assetCatalog, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}

func decodeToolCatalogContract(t *testing.T, rr *httptest.ResponseRecorder) (Response, map[string]any, map[string]any) {
	t.Helper()
	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool catalog data type: %T", resp.Data)
	}
	catalog, ok := data["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("missing catalog contract: %#v", data)
	}
	return resp, data, catalog
}

func requireCatalogItems(t *testing.T, catalog map[string]any) []map[string]any {
	t.Helper()
	rawItems, ok := catalog["items"].([]any)
	if !ok {
		t.Fatalf("missing catalog items: %#v", catalog["items"])
	}
	items := make([]map[string]any, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, item)
	}
	return items
}

func requireErrorCatalogContract(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	resp := decodeAPIResponse(t, rr)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected tool catalog error data type: %T", resp.Data)
	}
	catalog, ok := data["catalog"].(map[string]any)
	if !ok {
		t.Fatalf("missing catalog contract in error payload: %#v", data)
	}
	return catalog
}

func decodeRawAPIEnvelope(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal api response failed: %v body=%s", err, rr.Body.String())
	}
	return envelope
}

func requireSharedErrorDictionary(t *testing.T, container map[string]any) map[string]any {
	t.Helper()
	sharedError, ok := container["shared_error"].(map[string]any)
	if !ok {
		t.Fatalf("missing shared_error dictionary: %#v", container)
	}
	return sharedError
}

func requireAPIErrorEnvelope(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	errorEnvelope, ok := envelope["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing api error envelope: %#v", envelope)
	}
	return errorEnvelope
}

func TestCatalogContract(t *testing.T) {
	service := &fakeToolSessionService{
		snapshot: toolrouter.Snapshot{
			Interactive: []toolrouter.InteractiveToolDefinition{{
				Name:        "open_project",
				Description: "打开工程",
				Category:    "project",
			}, {
				Name:        "pyqgis_get_context",
				Description: "交互上下文工具",
				Category:    "project",
			}},
			Compute: []compute.ToolDefinition{{
				Name:        "get_project_info",
				Description: "工程信息",
				Category:    "project",
			}, {
				Name:        "get_layers_info",
				Description: "图层信息",
				Category:    "project",
			}},
		},
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			RuntimeRevision:     "runtime-rev-test",
			ToolCatalogRevision: "rev-tools",
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:     "instance-1",
				RunningCount: 1,
			},
		},
	}

	router, _ := buildToolTestRouter(t, service)
	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp, data, catalog := decodeToolCatalogContract(t, rr)
	if !resp.Success {
		t.Fatalf("tool catalog success=false: %+v", resp)
	}
	if _, exists := data["tools"]; exists {
		t.Fatalf("did not expect legacy tools in response: %#v", data)
	}
	if _, exists := data["tool_infos"]; exists {
		t.Fatalf("did not expect legacy tool_infos in response: %#v", data)
	}
	if _, exists := data["available_count"]; exists {
		t.Fatalf("did not expect legacy available_count in response: %#v", data)
	}
	if _, exists := data["configured_count"]; exists {
		t.Fatalf("did not expect legacy configured_count in response: %#v", data)
	}
	if _, exists := data["tool_catalog_status"]; exists {
		t.Fatalf("did not expect legacy tool_catalog_status in response: %#v", data)
	}
	if got, _ := catalog["id"].(string); got != "tool_catalog/global" {
		t.Fatalf("unexpected catalog id: %q", got)
	}
	if got, _ := catalog["version"].(string); got != "1" {
		t.Fatalf("unexpected catalog version: %q", got)
	}
	if got, _ := catalog["capability_type"].(string); got != "tool_catalog" {
		t.Fatalf("unexpected catalog capability_type: %q", got)
	}
	if got, _ := catalog["availability"].(string); got != "available" {
		t.Fatalf("unexpected catalog availability: %q body=%s", got, rr.Body.String())
	}
	if got, _ := catalog["degradation_status"].(string); got != "none" {
		t.Fatalf("unexpected catalog degradation_status: %q", got)
	}
	items := requireCatalogItems(t, catalog)
	if len(items) != 4 {
		t.Fatalf("expected 4 catalog items, got %d: %#v", len(items), items)
	}
	foundContractShape := false
	for _, item := range items {
		if item["id"] == "get_project_info" {
			foundContractShape = true
			if got, _ := item["runtime_target"].(string); got != "cli" {
				t.Fatalf("unexpected runtime_target: %q", got)
			}
			if got, _ := item["availability"].(string); got != "available" {
				t.Fatalf("unexpected item availability: %q", got)
			}
			if _, ok := item["name"]; ok {
				t.Fatalf("did not expect legacy item name field in contract item: %#v", item)
			}
		}
	}
	if !foundContractShape {
		t.Fatalf("expected get_project_info contract item, got %#v", items)
	}
}

func TestEmptyCatalogSemantics(t *testing.T) {
	t.Run("valid empty catalog", func(t *testing.T) {
		service := &fakeToolSessionService{}
		router, _ := buildToolTestRouterWithPackages(t, service, nil)
		req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
		}

		_, _, catalog := decodeToolCatalogContract(t, rr)
		if got, _ := catalog["availability"].(string); got != "valid_empty" {
			t.Fatalf("unexpected valid empty availability: %q", got)
		}
		if got, _ := catalog["degradation_status"].(string); got != "none" {
			t.Fatalf("unexpected valid empty degradation_status: %q", got)
		}
		if items := requireCatalogItems(t, catalog); len(items) != 0 {
			t.Fatalf("expected empty catalog items, got %#v", items)
		}
	})

	t.Run("abnormal empty catalog", func(t *testing.T) {
		router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{view: control.ToolCatalogView{
			Scope:           "global",
			ConfigName:      "default",
			RuntimeRevision: "runtime-rev-test",
			Catalog: toolcatalog.Contract{
				ID:                      "tool_catalog/global",
				Version:                 "1",
				Source:                  "go_control_plane",
				CapabilityType:          "tool_catalog",
				RuntimeTarget:           "mixed",
				Availability:            "unavailable",
				MissingDependencyReason: "abnormal_empty",
				Policy:                  &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
				DegradationStatus:       "abnormal_empty",
				Items:                   []toolcatalog.Capability{},
			},
		}})
		req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("expected abnormal empty status=500, got=%d body=%s", rr.Code, rr.Body.String())
		}

		catalog := requireErrorCatalogContract(t, rr)
		if got, _ := catalog["availability"].(string); got != "unavailable" {
			t.Fatalf("unexpected abnormal empty availability: %q", got)
		}
		if got, _ := catalog["degradation_status"].(string); got != "abnormal_empty" {
			t.Fatalf("unexpected abnormal empty degradation_status: %q", got)
		}
		envelope := decodeRawAPIEnvelope(t, rr)
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected abnormal empty data payload: %#v", envelope)
		}
		sharedError := requireSharedErrorDictionary(t, data)
		if got, _ := sharedError["code"].(string); got != "abnormal_empty" {
			t.Fatalf("unexpected abnormal empty shared_error code: %q payload=%#v", got, envelope)
		}
		if got, _ := sharedError["message"].(string); got != "工具目录异常为空，当前能力已降级。" {
			t.Fatalf("unexpected abnormal empty shared_error message: %q payload=%#v", got, envelope)
		}
		if got, _ := sharedError["user_action"].(string); got != "请刷新工具目录或检查运行态注册是否完成。" {
			t.Fatalf("unexpected abnormal empty shared_error user_action: %q payload=%#v", got, envelope)
		}
		if got, _ := sharedError["retryable"].(bool); !got {
			t.Fatalf("expected abnormal empty shared_error retryable=true, payload=%#v", envelope)
		}
		t.Logf("abnormal_empty http body: %s", rr.Body.String())
		errorEnvelope := requireAPIErrorEnvelope(t, envelope)
		if got, _ := errorEnvelope["message"].(string); got != "工具目录异常为空，当前能力已降级。" {
			t.Fatalf("unexpected abnormal empty error message: %q payload=%#v", got, envelope)
		}
		if got, _ := errorEnvelope["user_action"].(string); got != "请刷新工具目录或检查运行态注册是否完成。" {
			t.Fatalf("unexpected abnormal empty error user_action: %q payload=%#v", got, envelope)
		}
		if got, _ := errorEnvelope["retryable"].(bool); !got {
			t.Fatalf("expected abnormal empty error retryable=true, payload=%#v", envelope)
		}
		if got, _ := errorEnvelope["message"].(string); got != sharedError["message"] {
			t.Fatalf("abnormal empty error/shared_error message mismatch: error=%q shared=%q payload=%#v", got, sharedError["message"], envelope)
		}
		if got, _ := errorEnvelope["user_action"].(string); got != sharedError["user_action"] {
			t.Fatalf("abnormal empty error/shared_error user_action mismatch: error=%q shared=%q payload=%#v", got, sharedError["user_action"], envelope)
		}
		if got, _ := errorEnvelope["retryable"].(bool); got != sharedError["retryable"] {
			t.Fatalf("abnormal empty error/shared_error retryable mismatch: error=%v shared=%v payload=%#v", got, sharedError["retryable"], envelope)
		}
	})
}

func TestProviderMissingSemantics(t *testing.T) {
	router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-test",
		Catalog: toolcatalog.Contract{
			ID:                      "tool_catalog/global",
			Version:                 "1",
			Source:                  "go_control_plane",
			CapabilityType:          "tool_catalog",
			RuntimeTarget:           "cli",
			Availability:            "degraded",
			MissingDependencyReason: "provider_missing",
			Policy:                  &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
			DegradationStatus:       "provider_missing",
			Items: []toolcatalog.Capability{{
				ID:                      "output_init_dir",
				Source:                  "raster-analysis",
				CapabilityType:          "tool",
				RuntimeTarget:           "cli",
				Availability:            "unavailable",
				MissingDependencyReason: "provider_missing",
				Policy:                  &toolcatalog.Policy{Scope: "global"},
				DegradationStatus:       "provider_missing",
			}},
		},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected provider missing status=200, got=%d body=%s", rr.Code, rr.Body.String())
	}
	t.Logf("provider_missing http body: %s", rr.Body.String())

	_, _, catalog := decodeToolCatalogContract(t, rr)
	if got, _ := catalog["availability"].(string); got != "degraded" {
		t.Fatalf("unexpected provider missing availability: %q", got)
	}
	if got, _ := catalog["missing_dependency_reason"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider missing reason: %q", got)
	}
	if got, _ := catalog["degradation_status"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider missing degradation_status: %q", got)
	}
	items := requireCatalogItems(t, catalog)
	if len(items) != 1 {
		t.Fatalf("expected 1 provider-missing item, got %#v", items)
	}
	if got, _ := items[0]["missing_dependency_reason"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider-missing item reason: %q", got)
	}
	envelope := decodeRawAPIEnvelope(t, rr)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected provider missing data payload: %#v", envelope)
	}
	sharedError := requireSharedErrorDictionary(t, data)
	if got, _ := sharedError["code"].(string); got != "provider_missing" {
		t.Fatalf("unexpected provider missing shared_error code: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["message"].(string); got != "Provider 未配置，当前能力已降级。" {
		t.Fatalf("unexpected provider missing shared_error message: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["user_action"].(string); got != "请先配置至少一个 Provider 后重试。" {
		t.Fatalf("unexpected provider missing shared_error user_action: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["retryable"].(bool); !got {
		t.Fatalf("expected provider missing shared_error retryable=true, payload=%#v", envelope)
	}
}

func TestUnsupportedRuntimeSemantics(t *testing.T) {
	router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-rev-test",
		Catalog: toolcatalog.Contract{
			ID:                      "tool_catalog/global",
			Version:                 "1",
			Source:                  "go_control_plane",
			CapabilityType:          "tool_catalog",
			RuntimeTarget:           "mixed",
			Availability:            "unsupported",
			MissingDependencyReason: "unsupported_runtime",
			Policy:                  &toolcatalog.Policy{Scope: "global", ConfigName: "default"},
			DegradationStatus:       "unsupported_runtime",
			Items: []toolcatalog.Capability{{
				ID:                      "wasm_render_map",
				Source:                  "wasm-provider",
				CapabilityType:          "tool",
				RuntimeTarget:           "wasm",
				Availability:            "unsupported",
				MissingDependencyReason: "unsupported_runtime",
				Policy:                  &toolcatalog.Policy{Scope: "global"},
				DegradationStatus:       "unsupported_runtime",
			}},
		},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected unsupported runtime status=503, got=%d body=%s", rr.Code, rr.Body.String())
	}

	catalog := requireErrorCatalogContract(t, rr)
	if got, _ := catalog["availability"].(string); got != "unsupported" {
		t.Fatalf("unexpected unsupported runtime availability: %q", got)
	}
	if got, _ := catalog["missing_dependency_reason"].(string); got != "unsupported_runtime" {
		t.Fatalf("unexpected unsupported runtime reason: %q", got)
	}
	if got, _ := catalog["degradation_status"].(string); got != "unsupported_runtime" {
		t.Fatalf("unexpected unsupported runtime degradation_status: %q", got)
	}
	items := requireCatalogItems(t, catalog)
	if len(items) != 1 {
		t.Fatalf("expected 1 unsupported runtime item, got %#v", items)
	}
	if got, _ := items[0]["runtime_target"].(string); got != "wasm" {
		t.Fatalf("unexpected unsupported runtime target: %q", got)
	}
	envelope := decodeRawAPIEnvelope(t, rr)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected unsupported runtime data payload: %#v", envelope)
	}
	sharedError := requireSharedErrorDictionary(t, data)
	if got, _ := sharedError["code"].(string); got != "unsupported_runtime" {
		t.Fatalf("unexpected unsupported runtime shared_error code: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["message"].(string); got != "当前运行时不支持该能力。" {
		t.Fatalf("unexpected unsupported runtime shared_error message: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["user_action"].(string); got != "请切换到受支持的运行时或启动对应服务。" {
		t.Fatalf("unexpected unsupported runtime shared_error user_action: %q payload=%#v", got, envelope)
	}
	if got, _ := sharedError["retryable"].(bool); got {
		t.Fatalf("expected unsupported runtime shared_error retryable=false, payload=%#v", envelope)
	}
	t.Logf("unsupported_runtime http body: %s", rr.Body.String())
	errorEnvelope := requireAPIErrorEnvelope(t, envelope)
	if got, _ := errorEnvelope["message"].(string); got != "当前运行时不支持该能力。" {
		t.Fatalf("unexpected unsupported runtime error message: %q payload=%#v", got, envelope)
	}
	if got, _ := errorEnvelope["user_action"].(string); got != "请切换到受支持的运行时或启动对应服务。" {
		t.Fatalf("unexpected unsupported runtime error user_action: %q payload=%#v", got, envelope)
	}
	if got, _ := errorEnvelope["retryable"].(bool); got {
		t.Fatalf("expected unsupported runtime error retryable=false, payload=%#v", envelope)
	}
	if got, _ := errorEnvelope["message"].(string); got != sharedError["message"] {
		t.Fatalf("unsupported runtime error/shared_error message mismatch: error=%q shared=%q payload=%#v", got, sharedError["message"], envelope)
	}
	if got, _ := errorEnvelope["user_action"].(string); got != sharedError["user_action"] {
		t.Fatalf("unsupported runtime error/shared_error user_action mismatch: error=%q shared=%q payload=%#v", got, sharedError["user_action"], envelope)
	}
	if got, _ := errorEnvelope["retryable"].(bool); got != sharedError["retryable"] {
		t.Fatalf("unsupported runtime error/shared_error retryable mismatch: error=%v shared=%v payload=%#v", got, sharedError["retryable"], envelope)
	}
}

func TestGetToolCatalogUsesAuthoritativeContractItems(t *testing.T) {
	service := &fakeToolSessionService{
		snapshot: toolrouter.Snapshot{
			Interactive: []toolrouter.InteractiveToolDefinition{
				{
					Name:        "open_project",
					Description: "打开工程",
					Category:    "project",
				},
			},
			Compute: []compute.ToolDefinition{
				{
					Name:        "get_project_info",
					Description: "工程信息",
					Category:    "project",
				},
			},
		},
		runtimeSnapshot: middlewareclient.RuntimeSnapshot{
			ToolCatalogRevision: "rev-tools",
			Instances: middlewareclient.InstancesSnapshot{
				ActiveID:     "instance-1",
				RunningCount: 1,
			},
		},
	}

	router, _ := buildToolTestRouter(t, service)

	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp, data, catalog := decodeToolCatalogContract(t, rr)
	if !resp.Success {
		t.Fatalf("tool catalog success=false: %+v", resp)
	}
	items := requireCatalogItems(t, catalog)
	itemMap := make(map[string]map[string]any)
	for _, item := range items {
		name, _ := item["id"].(string)
		if name != "" {
			itemMap[name] = item
		}
	}

	interactiveInfo, ok := itemMap["open_project"]
	if !ok {
		t.Fatalf("missing interactive tool contract in %#v", itemMap)
	}
	if got, _ := interactiveInfo["source"].(string); got != "qgis" {
		t.Fatalf("unexpected interactive source: %q", got)
	}
	if got, _ := interactiveInfo["runtime_target"].(string); got != "qgis" {
		t.Fatalf("unexpected interactive runtime_target: %q", got)
	}
	if got, _ := interactiveInfo["availability"].(string); got != "available" {
		t.Fatalf("unexpected interactive availability: %q", got)
	}
	if got, _ := interactiveInfo["capability_type"].(string); got != "tool" {
		t.Fatalf("unexpected interactive capability_type: %q", got)
	}

	computeInfo, ok := itemMap["get_project_info"]
	if !ok {
		t.Fatalf("missing compute tool contract in %#v", itemMap)
	}
	if got, _ := computeInfo["source"].(string); got != "cli" {
		t.Fatalf("unexpected compute source: %q", got)
	}
	if got, _ := computeInfo["runtime_target"].(string); got != "cli" {
		t.Fatalf("unexpected compute runtime_target: %q", got)
	}
	if got, _ := computeInfo["availability"].(string); got != "available" {
		t.Fatalf("unexpected compute availability: %q", got)
	}
	if got, _ := data["runtime_revision"].(string); got != "runtime-rev-test" {
		t.Fatalf("unexpected runtime_revision: %q", got)
	}
}

func TestGetToolCatalogReturnsChineseDiscoveryMetadata(t *testing.T) {
	contract := toolcatalog.BuildContract("global", "default", []toolcatalog.Item{{
		Name: "sample", DisplayName: "创建要素缓冲区", Description: "按距离生成缓冲区", Keywords: []string{"缓冲", "邻近分析"}, PackageID: "vector-analysis", Source: "qgis", Available: true,
	}}, []string{"sample"}, nil)
	router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{view: control.ToolCatalogView{Catalog: contract}})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	_, _, catalog := decodeToolCatalogContract(t, rr)
	items := requireCatalogItems(t, catalog)
	if len(items) != 1 {
		t.Fatalf("unexpected catalog: %#v", items)
	}
	item := items[0]
	if item["display_name"] != "创建要素缓冲区" || item["description"] != "按距离生成缓冲区" || item["package_id"] != "vector-analysis" {
		t.Fatalf("missing public discovery metadata: %#v", item)
	}
	keywords, ok := item["keywords"].([]any)
	if !ok || len(keywords) != 2 || keywords[0] != "缓冲" || keywords[1] != "邻近分析" {
		t.Fatalf("keywords must be a string array: %#v", item)
	}
}

func TestGetToolCatalogMarksUnavailableContractItems(t *testing.T) {
	service := &fakeToolSessionService{
		snapshot: toolrouter.Snapshot{},
	}

	router, _ := buildToolTestRouter(t, service)

	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	_, _, catalog := decodeToolCatalogContract(t, rr)
	items := requireCatalogItems(t, catalog)
	itemMap := make(map[string]map[string]any)
	for _, item := range items {
		name, _ := item["id"].(string)
		if name != "" {
			itemMap[name] = item
		}
	}

	interactiveMissing, ok := itemMap["pyqgis_get_context"]
	if !ok {
		t.Fatalf("missing pyqgis_get_context contract in %#v", itemMap)
	}
	if got, _ := interactiveMissing["availability"].(string); got != "unavailable" {
		t.Fatalf("unexpected pyqgis_get_context availability: %q", got)
	}
	if reason, _ := interactiveMissing["missing_dependency_reason"].(string); reason != "runtime_catalog_missing" {
		t.Fatalf("unexpected pyqgis_get_context missing_dependency_reason: %q", reason)
	}
	if got, _ := interactiveMissing["source"].(string); got != "go_control_plane" {
		t.Fatalf("unexpected pyqgis_get_context source: %q", got)
	}

	computeMissing, ok := itemMap["get_project_info"]
	if !ok {
		t.Fatalf("missing get_project_info contract in %#v", itemMap)
	}
	if got, _ := computeMissing["availability"].(string); got != "unavailable" {
		t.Fatalf("unexpected get_project_info availability: %q", got)
	}
	if reason, _ := computeMissing["missing_dependency_reason"].(string); reason != "runtime_catalog_missing" {
		t.Fatalf("unexpected get_project_info missing_dependency_reason: %q", reason)
	}
	if got, _ := computeMissing["source"].(string); got != "qgis" {
		t.Fatalf("unexpected get_project_info source: %q", got)
	}
}

func TestGetToolCatalogOmitsLegacyPackagePolicyFields(t *testing.T) {
	service := &fakeToolSessionService{
		snapshot: toolrouter.Snapshot{
			Interactive: []toolrouter.InteractiveToolDefinition{{
				Name:        "test_interactive_tool",
				Description: "交互工具描述",
				Category:    "interactive",
			}},
			Compute: []compute.ToolDefinition{{
				Name:        "test_compute_tool",
				Description: "计算工具描述",
				Category:    "compute",
			}},
		},
	}
	rootDir := t.TempDir()
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	skillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	toolsDir := filepath.Join(rootDir, "tools", "user")
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(rootDir); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
	})
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "canvas-navigation"), 0o755); err != nil {
		t.Fatalf("mkdir interactive skill dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "raster-analysis"), 0o755); err != nil {
		t.Fatalf("mkdir compute skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "canvas-navigation", "SKILL.md"), []byte("---\nname: canvas-navigation\ndescription: canvas-navigation\nkind: category\nbackend: bridge\nenabled: true\ntools:\n  - test_interactive_tool\n---\n"), 0o644); err != nil {
		t.Fatalf("write interactive SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "raster-analysis", "SKILL.md"), []byte("---\nname: raster-analysis\ndescription: raster-analysis\nkind: category\nbackend: compute\nenabled: true\ntools:\n  - test_compute_tool\n---\n"), 0o644); err != nil {
		t.Fatalf("write compute SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(toolsDir, "canvas-navigation"), 0o755); err != nil {
		t.Fatalf("mkdir interactive tools dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(toolsDir, "raster-analysis"), 0o755); err != nil {
		t.Fatalf("mkdir compute tools dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "canvas-navigation", "test_interactive_tool.yaml"), []byte("apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: test_interactive_tool\n  domain: canvas-navigation\n  runtime: qgis\nspec:\n  description: test_interactive_tool desc\n  parameters:\n    - name: INPUT\n      type: string\nexecution:\n  backend_tool: test_interactive_tool\n  param_mapping:\n    INPUT: '{{ layer }}'\n"), 0o644); err != nil {
		t.Fatalf("write interactive tool yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "raster-analysis", "test_compute_tool.yaml"), []byte("apiVersion: tool/v1\nkind: Tool\nmetadata:\n  name: test_compute_tool\n  domain: raster-analysis\n  runtime: cli\nspec:\n  description: test_compute_tool desc\n  parameters:\n    - name: INPUT\n      type: string\nexecution:\n  backend_tool: test_compute_tool\n  param_mapping:\n    INPUT: '{{ layer }}'\n"), 0o644); err != nil {
		t.Fatalf("write compute tool yaml: %v", err)
	}
	defaultConfig := `version: "1.0"
runtime:
  global:
	skill_packages: [canvas-navigation, raster-analysis]
  agents:
    orchestrator:
      display_name: 编排智能体
      description: tool test orchestrator
      is_main: true
      enabled: true
      skill_policy:
        private: {}
        share: {}
      prompts:
        static_instructions: test
        user_prompt_template: "{{ user_input }}"
`
	if err := os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(defaultConfig), 0o644); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	acm := config.NewAgentConfigManager(configDir)
	skillService := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		BuiltinSkillsDir: skillsDir,
		BuiltinToolsDir:  toolsDir,
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
			return control.NewRegistryGovernance(control.RegistryGovernanceDeps{
				ResolveToolDirs:  func() []string { return []string{toolsDir} },
				ResolveSkillDirs: func() []string { return []string{skillsDir} },
			})
		},
	}, nil)
	handler := NewAPI(nil, acm, service, skillService, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	_, _, catalog := decodeToolCatalogContract(t, rr)
	items := requireCatalogItems(t, catalog)
	for _, item := range items {
		if _, exists := item["kind"]; exists {
			t.Fatalf("unexpected legacy kind in authoritative contract: %#v", item)
		}
		if _, exists := item["domain"]; exists {
			t.Fatalf("unexpected legacy domain in authoritative contract: %#v", item)
		}
		if _, exists := item["enabled"]; exists {
			t.Fatalf("unexpected legacy enabled flag in authoritative contract: %#v", item)
		}
	}
}

func TestGetToolCatalogPublicPayloadDoesNotLeakHiddenToolNames(t *testing.T) {
	router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{view: control.ToolCatalogView{
		Scope:           "global",
		ConfigName:      "default",
		RuntimeRevision: "runtime-1",
		Catalog: toolcatalog.BuildContract(
			"global",
			"default",
			[]toolcatalog.Item{{Name: "open_project", Source: toolcatalog.SourceInteractive, Available: true}},
			[]string{"open_project"},
			nil,
		),
		ToolInfos: []toolcatalog.Item{
			{Name: "open_project", Source: toolcatalog.SourceInteractive, Available: true},
			{Name: "open_project_debug", Source: toolcatalog.SourceInteractive, Available: true},
		},
		Tools: []string{"open_project", "open_project_debug"},
	}})

	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	_, data, catalog := decodeToolCatalogContract(t, rr)
	if _, exists := data["tool_infos"]; exists {
		t.Fatalf("expected tool_infos to remain internal-only, payload=%#v", data)
	}
	if _, exists := data["tools"]; exists {
		t.Fatalf("expected tools to remain internal-only, payload=%#v", data)
	}
	items := requireCatalogItems(t, catalog)
	if len(items) != 1 {
		t.Fatalf("expected only public catalog item in payload, got %#v", items)
	}
	if got, _ := items[0]["id"].(string); got != "open_project" {
		t.Fatalf("unexpected public catalog item id: %q payload=%#v", got, items)
	}
}

func TestGetToolCatalogReturnsDegradedPayloadWhenAssetCatalogTimesOut(t *testing.T) {
	prevTimeout := assetCatalogReadTimeout
	assetCatalogReadTimeout = 10 * time.Millisecond
	defer func() {
		assetCatalogReadTimeout = prevTimeout
	}()

	router := buildToolTestRouterWithAssetCatalog(t, stubAPIAssetCatalogService{
		blockUntil: make(chan struct{}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/tools/catalog?scope=global", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/tools/catalog status=%d body=%s", rr.Code, rr.Body.String())
	}

	_, data, catalog := decodeToolCatalogContract(t, rr)
	sharedError, ok := data["shared_error"].(map[string]any)
	if !ok {
		t.Fatalf("expected degraded shared_error payload, got %#v", data["shared_error"])
	}
	if got := sharedError["code"]; got != "catalog_degraded" {
		t.Fatalf("unexpected shared_error.code: %#v", got)
	}
	if got := catalog["degradation_status"]; got != toolcatalog.DegradationStatusMissingDependency {
		t.Fatalf("unexpected degradation_status: %#v", got)
	}
	if got := catalog["missing_dependency_reason"]; got != toolcatalog.MissingReasonRuntimeCatalogMissing {
		t.Fatalf("unexpected missing_dependency_reason: %#v", got)
	}
}
