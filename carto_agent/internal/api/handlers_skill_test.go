package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/control"
	bridgepkg "github.com/autocarto/carto_agent/internal/middleware/bridge"
	"github.com/autocarto/carto_agent/internal/settings"
	"github.com/autocarto/carto_agent/internal/skill"
)

func TestSkillPackageEndpointsExposePackageDataAndToggle(t *testing.T) {
	skillsDir := t.TempDir()
	writeSkillPackageTestFile(t, filepath.Join(skillsDir, "demo-package", "SKILL.md"), `---
name: demo-package
display_name: 演示工具包
domain: 系统
system: false
hidden: true
auto_mount: plan_main
description: demo package
kind: category
backend: compute
enabled: true
tools:
  - demo_buffer
---

# demo
`)
	writeSkillPackageTestFile(t, filepath.Join(skillsDir, "tools", "user", "demo-package", "demo_buffer.yaml"), `apiVersion: tool/v1
kind: Tool
metadata:
  name: demo_buffer
  domain: vector-tools
  runtime: cli
spec:
  description: demo buffer
  parameters:
    - name: INPUT
      type: string
execution:
  backend_tool: demo_buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	userToolsDir := settings.InferUserToolsDirFromSkillsDir(skillsDir)
	isolatedSkillService := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		UserSkillsDir: skillsDir,
		UserToolsDir:  userToolsDir,
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
			return control.NewRegistryGovernance(control.RegistryGovernanceDeps{
				ResolveToolDirs:  func() []string { return []string{userToolsDir} },
				ResolveSkillDirs: func() []string { return []string{skillsDir} },
			})
		},
	}, nil)
	handler := NewAPI(nil, config.NewAgentConfigManager(t.TempDir()), newInMemorySessionService(), isolatedSkillService, skillsDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	listReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages", nil)
	listRR := httptest.NewRecorder()
	router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	listResp := decodeAPIResponse(t, listRR)
	listData := listResp.Data.(map[string]any)
	packages := listData["packages"].([]any)
	if len(packages) == 0 {
		t.Fatalf("expected packages payload to include demo-package, got %#v", listData["packages"])
	}
	foundDemo := false
	for _, raw := range packages {
		pkg := raw.(map[string]any)
		if pkg["package_id"] == "demo-package" {
			foundDemo = true
			if pkg["display_name"] != "演示工具包" || pkg["domain"] != "系统" || pkg["system"] != false || pkg["hidden"] != true || pkg["auto_mount"] != "plan_main" {
				t.Fatalf("v2 package metadata lost in public response: %#v", pkg)
			}
			break
		}
	}
	if !foundDemo {
		t.Fatalf("expected demo-package in overlay package list, got %#v", listData["packages"])
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages/demo-package", nil)
	detailRR := httptest.NewRecorder()
	router.ServeHTTP(detailRR, detailReq)
	if detailRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages/demo-package status=%d body=%s", detailRR.Code, detailRR.Body.String())
	}
	detailResp := decodeAPIResponse(t, detailRR)
	detailData := detailResp.Data.(map[string]any)
	if detailData["kind"] != "category" {
		t.Fatalf("unexpected package kind: %#v", detailData)
	}
	toolRefs, ok := detailData["tool_refs"].([]any)
	if !ok || len(toolRefs) != 1 || toolRefs[0] != "demo_buffer" {
		t.Fatalf("unexpected tool_refs payload: %#v", detailData["tool_refs"])
	}
	if tools, ok := detailData["tools"]; ok && tools != nil {
		if typed, castOK := tools.([]any); !castOK || len(typed) != 0 {
			t.Fatalf("expected refs-only package detail to omit or empty legacy tools payload, got %#v", tools)
		}
	}

	toggleReq := httptest.NewRequest(http.MethodPost, "/api/skill-packages/demo-package/toggle", bytes.NewBufferString(`{"enabled":false}`))
	toggleReq.Header.Set("Content-Type", "application/json")
	toggleRR := httptest.NewRecorder()
	router.ServeHTTP(toggleRR, toggleReq)
	if toggleRR.Code != http.StatusOK {
		t.Fatalf("POST /api/skill-packages/demo-package/toggle status=%d body=%s", toggleRR.Code, toggleRR.Body.String())
	}
	toggleResp := decodeAPIResponse(t, toggleRR)
	toggleData := toggleResp.Data.(map[string]any)
	mutation := toggleData["mutation"].(map[string]any)
	if mutation["required_action"] != "runtime_rebuild" {
		t.Fatalf("unexpected mutation payload: %#v", mutation)
	}
	pkg := toggleData["package"].(map[string]any)
	if enabled, ok := pkg["enabled"].(bool); !ok || enabled {
		t.Fatalf("expected package disabled after toggle, got %#v", pkg)
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/skills/demo-package", nil)
	legacyRR := httptest.NewRecorder()
	router.ServeHTTP(legacyRR, legacyReq)
	if legacyRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skills/demo-package status=%d body=%s", legacyRR.Code, legacyRR.Body.String())
	}
	legacyResp := decodeAPIResponse(t, legacyRR)
	legacyData := legacyResp.Data.(map[string]any)
	if legacyData["kind"] != "category" {
		t.Fatalf("unexpected legacy package detail: %#v", legacyData)
	}
	if legacyData["tool_ref_count"] != float64(1) {
		t.Fatalf("unexpected legacy tool_ref_count: %#v", legacyData)
	}
	if _, ok := legacyData["tools"]; ok {
		t.Fatalf("expected legacy skill detail to omit embedded tools payload, got %#v", legacyData["tools"])
	}
}

func TestLegacySkillImportRejectsManagedPackageDirectory(t *testing.T) {
	skillsDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "source-package")
	writeSkillPackageTestFile(t, filepath.Join(sourceDir, "SKILL.md"), `---
name: managed-package
description: managed package
kind: category
---

# managed
`)
	writeSkillPackageTestFile(t, filepath.Join(sourceDir, "tools", "buffer.yaml"), `apiVersion: skill/v1
kind: AtomicSkill
metadata:
  name: buffer
  backend: compute
spec:
  description: buffer
execution:
  backend_tool: buffer
  param_mapping:
    INPUT: '{{ INPUT }}'
`)

	handler := NewAPI(nil, config.NewAgentConfigManager(t.TempDir()), newInMemorySessionService(), nil, skillsDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/skills/import", bytes.NewBufferString(`{"source_path":"`+sourceDir+`","overwrite":false}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("POST /api/skills/import status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	if resp.Code != "SKILL_PACKAGE_OPERATION_UNSUPPORTED" {
		t.Fatalf("unexpected response code: %#v", resp)
	}
}

func TestSkillPackageScopeEndpointsExposeExclusivePackages(t *testing.T) {
	skillsDir := t.TempDir()
	writeSkillPackageTestFile(t, filepath.Join(skillsDir, "global-package", "SKILL.md"), `---
name: global-package
description: global package
kind: workflow
---

# global
`)

	acm := config.NewAgentConfigManager(t.TempDir())
	exclusiveSkillPath := filepath.Join(acm.ExclusiveSkillsDir("default"), "exclusive-package", "SKILL.md")
	writeSkillPackageTestFile(t, exclusiveSkillPath, `---
name: exclusive-package
description: exclusive package
kind: workflow
---

# exclusive
`)

	handler := NewAPI(nil, acm, newInMemorySessionService(), nil, skillsDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	globalReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages?scope=global", nil)
	globalRR := httptest.NewRecorder()
	router.ServeHTTP(globalRR, globalReq)
	if globalRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages?scope=global status=%d body=%s", globalRR.Code, globalRR.Body.String())
	}
	globalResp := decodeAPIResponse(t, globalRR)
	globalPackages := globalResp.Data.(map[string]any)["packages"].([]any)
	foundUserPackage := false
	for _, item := range globalPackages {
		pkg := item.(map[string]any)
		if pkg["package_id"] == "global-package" && pkg["scope"] == "user" {
			foundUserPackage = true
			break
		}
	}
	if !foundUserPackage {
		t.Fatalf("expected global alias to include user package, got %#v", globalPackages)
	}

	exclusiveReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages?scope=exclusive&config_name=default", nil)
	exclusiveRR := httptest.NewRecorder()
	router.ServeHTTP(exclusiveRR, exclusiveReq)
	if exclusiveRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages?scope=exclusive status=%d body=%s", exclusiveRR.Code, exclusiveRR.Body.String())
	}
	exclusiveResp := decodeAPIResponse(t, exclusiveRR)
	exclusivePackages := exclusiveResp.Data.(map[string]any)["packages"].([]any)
	if len(exclusivePackages) != 1 {
		t.Fatalf("unexpected exclusive packages: %#v", exclusivePackages)
	}
	exclusivePackage := exclusivePackages[0].(map[string]any)
	if exclusivePackage["package_id"] != "exclusive-package" || exclusivePackage["scope"] != "exclusive" || exclusivePackage["owner_config"] != "default" {
		t.Fatalf("unexpected exclusive package payload: %#v", exclusivePackage)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages/exclusive-package?scope=exclusive&config_name=default", nil)
	detailRR := httptest.NewRecorder()
	router.ServeHTTP(detailRR, detailReq)
	if detailRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages/exclusive-package status=%d body=%s", detailRR.Code, detailRR.Body.String())
	}
	detailResp := decodeAPIResponse(t, detailRR)
	detail := detailResp.Data.(map[string]any)
	if detail["scope"] != "exclusive" || detail["owner_config"] != "default" {
		t.Fatalf("unexpected scoped detail payload: %#v", detail)
	}

	allReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages?scope=all&config_name=default", nil)
	allRR := httptest.NewRecorder()
	router.ServeHTTP(allRR, allReq)
	if allRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages?scope=all status=%d body=%s", allRR.Code, allRR.Body.String())
	}
	allResp := decodeAPIResponse(t, allRR)
	allPackages := allResp.Data.(map[string]any)["packages"].([]any)
	foundGlobalPackage := false
	foundExclusivePackage := false
	for _, item := range allPackages {
		pkg := item.(map[string]any)
		if pkg["package_id"] == "global-package" && pkg["scope"] == "user" {
			foundGlobalPackage = true
		}
		if pkg["package_id"] == "exclusive-package" && pkg["scope"] == "exclusive" {
			foundExclusivePackage = true
		}
	}
	if !foundGlobalPackage || !foundExclusivePackage {
		t.Fatalf("unexpected scoped all packages: %#v", allPackages)
	}
}

func TestDefaultAPIControlServicesReadFullRuntimeOverlay(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(rootDir, "autocarto"))
	userDir := filepath.Join(rootDir, ".autocarto", "skills", "user")
	defaultsSkillPath := filepath.Join(rootDir, "autocarto", "skills", "system", "defaults-package", "SKILL.md")
	writeSkillPackageTestFile(t, defaultsSkillPath, `---
name: defaults-package
description: defaults package
kind: workflow
---

# defaults
`)
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

	acm := config.NewAgentConfigManager(filepath.Join(rootDir, ".autocarto", "agent_configs"))
	handler := NewAPI(nil, acm, newInMemorySessionService(), nil, userDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/api/skill-packages", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	packages := resp.Data.(map[string]any)["packages"].([]any)
	foundDefaultsPackage := false
	for _, item := range packages {
		pkg := item.(map[string]any)
		if pkg["package_id"] == "defaults-package" {
			foundDefaultsPackage = true
			break
		}
	}
	if !foundDefaultsPackage {
		t.Fatalf("expected defaults package visible through default API overlay, got %#v", packages)
	}
}

func TestSkillPackageEndpointsDoNotExposeBridgeDynamicToolsAsVirtualPackage(t *testing.T) {
	bridgeDynamicRegistry := skill.NewBridgeDynamicRegistry()
	bridgeDynamicRegistry.RegisterInstanceTools("instance-a", []bridgepkg.ToolDefinition{{
		Name:        "flash_feature",
		Description: "flash feature",
		Category:    "vector-tools",
		InputSchema: map[string]any{
			"type": "object",
		},
	}})

	svc := settings.NewFileSkillServiceWithConfig(settings.FileSkillServiceConfig{
		UserSkillsDir: t.TempDir(),
		UserToolsDir:  t.TempDir(),
		ResolveRuntimeRegistryGovernance: func() settings.RuntimeRegistryGovernance {
			return control.NewRegistryGovernance(control.RegistryGovernanceDeps{
				ResolveSkillDirs:    func() []string { return nil },
				DynamicToolRegistry: bridgeDynamicRegistry,
			})
		},
	}, nil)

	handler := NewAPI(nil, config.NewAgentConfigManager(t.TempDir()), newInMemorySessionService(), svc, t.TempDir(), nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	listReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages", nil)
	listRR := httptest.NewRecorder()
	router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("GET /api/skill-packages status=%d body=%s", listRR.Code, listRR.Body.String())
	}
	listResp := decodeAPIResponse(t, listRR)
	packages := listResp.Data.(map[string]any)["packages"].([]any)
	for _, raw := range packages {
		pkg := raw.(map[string]any)
		if pkg["package_id"] == "_bridge-dynamic" {
			t.Fatalf("expected bridge dynamic tools hidden from package list, got %#v", packages)
		}
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/skill-packages/_bridge-dynamic", nil)
	detailReq = mux.SetURLVars(detailReq, map[string]string{"name": "_bridge-dynamic"})
	detailRR := httptest.NewRecorder()
	router.ServeHTTP(detailRR, detailReq)
	if detailRR.Code != http.StatusNotFound {
		t.Fatalf("GET /api/skill-packages/_bridge-dynamic status=%d body=%s", detailRR.Code, detailRR.Body.String())
	}

	legacyReq := httptest.NewRequest(http.MethodGet, "/api/skills/_bridge-dynamic", nil)
	legacyReq = mux.SetURLVars(legacyReq, map[string]string{"name": "_bridge-dynamic"})
	legacyRR := httptest.NewRecorder()
	router.ServeHTTP(legacyRR, legacyReq)
	if legacyRR.Code != http.StatusNotFound {
		t.Fatalf("GET /api/skills/_bridge-dynamic status=%d body=%s", legacyRR.Code, legacyRR.Body.String())
	}
}

func TestSkillPackageMigrateEndpointMovesSingleOwnerPackage(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(rootDir, "autocarto"))
	configDir := filepath.Join(rootDir, ".autocarto", "config", "agent_configs")
	skillsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir failed: %v", err)
	}
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir failed: %v", err)
	}
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
	packageID := "migratable-package"
	writeSkillPackageTestFile(t, filepath.Join(skillsDir, packageID, "SKILL.md"), `---
name: migratable-package
description: migratable package
kind: workflow
---

# migratable
`)

	acm := config.NewAgentConfigManager(configDir)
	data, err := acm.GetConfigData("default")
	if err != nil {
		t.Fatalf("GetConfigData(default) failed: %v", err)
	}
	data.Runtime.Global.SkillPackages = append(data.Runtime.Global.SkillPackages, packageID)
	if err := acm.SaveConfig("", "default", data); err != nil {
		t.Fatalf("SaveConfig(default) failed: %v", err)
	}

	handler := NewAPI(nil, acm, newInMemorySessionService(), nil, skillsDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/skill-packages/migrate", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/skill-packages/migrate status=%d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeAPIResponse(t, rr)
	dataPayload := resp.Data.(map[string]any)
	migrated := dataPayload["migrated"].([]any)
	if len(migrated) != 1 {
		t.Fatalf("unexpected migrated payload: %#v", dataPayload)
	}
	targetPath := filepath.Join(acm.ExclusiveSkillsDir("default"), packageID, "SKILL.md")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected migrated package at %s: %v", targetPath, err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, packageID)); !os.IsNotExist(err) {
		t.Fatalf("expected global package removed after migrate, err=%v", err)
	}
	mutation := dataPayload["mutation"].(map[string]any)
	if mutation["required_action"] != "runtime_rebuild" {
		t.Fatalf("unexpected migration mutation: %#v", mutation)
	}
}

func TestSkillMutationEndpointsSupportExplicitExclusiveScope(t *testing.T) {
	globalDir := t.TempDir()
	acm := config.NewAgentConfigManager(t.TempDir())
	exclusiveDir := acm.ExclusiveSkillsDir("default")
	handler := NewAPI(nil, acm, newInMemorySessionService(), nil, globalDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	saveReq := httptest.NewRequest(http.MethodPut, "/api/skills/scoped-package?scope=exclusive&config_name=default", bytes.NewBufferString(`{"description":"exclusive package","content":"# exclusive"}`))
	saveReq = mux.SetURLVars(saveReq, map[string]string{"name": "scoped-package"})
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	router.ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("PUT /api/skills/scoped-package?scope=exclusive status=%d body=%s", saveRR.Code, saveRR.Body.String())
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "scoped-package", "SKILL.md")); err != nil {
		t.Fatalf("expected scoped save in exclusive dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "scoped-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected global dir untouched after scoped save, stat err=%v", err)
	}

	importPath := filepath.Join(t.TempDir(), "import-me.md")
	writeSkillPackageTestFile(t, importPath, `---
name: imported-scoped-package
description: imported scoped package
kind: category
tools: [fixture_tool]
---

# imported
`)
	importReq := httptest.NewRequest(http.MethodPost, "/api/skills/import?scope=exclusive&config_name=default", bytes.NewBufferString(`{"source_path":"`+importPath+`","overwrite":false}`))
	importReq.Header.Set("Content-Type", "application/json")
	importRR := httptest.NewRecorder()
	router.ServeHTTP(importRR, importReq)
	if importRR.Code != http.StatusOK {
		t.Fatalf("POST /api/skills/import?scope=exclusive status=%d body=%s", importRR.Code, importRR.Body.String())
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "imported-scoped-package", "SKILL.md")); err != nil {
		t.Fatalf("expected scoped import in exclusive dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "imported-scoped-package", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("expected global dir untouched after scoped import, stat err=%v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/skills/scoped-package?scope=exclusive&config_name=default", nil)
	deleteReq = mux.SetURLVars(deleteReq, map[string]string{"name": "scoped-package"})
	deleteRR := httptest.NewRecorder()
	router.ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("DELETE /api/skills/scoped-package?scope=exclusive status=%d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
	if _, err := os.Stat(filepath.Join(exclusiveDir, "scoped-package")); !os.IsNotExist(err) {
		t.Fatalf("expected scoped delete to remove exclusive package, stat err=%v", err)
	}
}

func TestSkillMutationEndpointExplicitScopedEditPreservesLowerLayerTrackedFiles(t *testing.T) {
	rootDir := t.TempDir()
	t.Setenv("AUTOCARTO_ASSETS_ROOT", filepath.Join(rootDir, "autocarto"))
	userDir := filepath.Join(rootDir, ".autocarto", "skills", "user")
	defaultsDir := filepath.Join(rootDir, "autocarto", "skills", "system")
	writeSkillPackageTestFile(t, filepath.Join(defaultsDir, "shared-package", "SKILL.md"), `---
name: shared-package
description: defaults package
kind: category
runtime: qgis
tools:
  - buffer
---

# defaults package
`)
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

	acm := config.NewAgentConfigManager(filepath.Join(rootDir, ".autocarto", "agent_configs"))
	exclusiveDir := acm.ExclusiveSkillsDir("default")
	handler := NewAPI(nil, acm, newInMemorySessionService(), nil, userDir, nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	req := httptest.NewRequest(http.MethodPut, "/api/skills/shared-package?scope=exclusive&config_name=default", bytes.NewBufferString(`{"description":"exclusive package","content":"# exclusive"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/skills/shared-package?scope=exclusive status=%d body=%s", rr.Code, rr.Body.String())
	}
	content, err := os.ReadFile(filepath.Join(exclusiveDir, "shared-package", "SKILL.md"))
	if err != nil {
		t.Fatalf("expected explicit scoped edit to preserve override package: %v", err)
	}
	if !strings.Contains(string(content), "tools:") || !strings.Contains(string(content), "- buffer") {
		t.Fatalf("expected explicit scoped edit to preserve tool refs, got %s", string(content))
	}
}

func writeSkillPackageTestFile(t *testing.T, path string, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir error: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("write error: %v", err)
	}
}
