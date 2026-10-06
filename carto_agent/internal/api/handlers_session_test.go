package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/settings"
)

type inMemorySessionService struct {
	byProject map[string]map[string]map[string]any
}

func newInMemorySessionService() *inMemorySessionService {
	return &inMemorySessionService{
		byProject: make(map[string]map[string]map[string]any),
	}
}

func sessionProjectKey(projectPath string) string {
	if projectPath == "" {
		return "__no_project__"
	}
	return projectPath
}

func cloneSessionMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func (s *inMemorySessionService) SaveSession(sessionData map[string]any, projectPath string) error {
	projectKey := sessionProjectKey(projectPath)
	sessionID, _ := sessionData["session_id"].(string)
	if sessionID == "" {
		return fmt.Errorf("missing session_id")
	}
	if _, ok := s.byProject[projectKey]; !ok {
		s.byProject[projectKey] = make(map[string]map[string]any)
	}
	s.byProject[projectKey][sessionID] = cloneSessionMap(sessionData)
	return nil
}

func (s *inMemorySessionService) SaveSessionWithOperation(sessionData map[string]any, projectPath string, operation map[string]any) error {
	return s.SaveSession(sessionData, projectPath)
}

func (s *inMemorySessionService) LoadSession(sessionID, projectPath string) (map[string]any, error) {
	projectKey := sessionProjectKey(projectPath)
	byID := s.byProject[projectKey]
	if byID == nil {
		return nil, fmt.Errorf("not found")
	}
	sessionData, ok := byID[sessionID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return cloneSessionMap(sessionData), nil
}

func (s *inMemorySessionService) ListSessions(projectPath string) ([]map[string]any, error) {
	projectKey := sessionProjectKey(projectPath)
	byID := s.byProject[projectKey]
	out := make([]map[string]any, 0, len(byID))
	for _, sessionData := range byID {
		out = append(out, cloneSessionMap(sessionData))
	}
	return out, nil
}

func (s *inMemorySessionService) ListProjects() ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (s *inMemorySessionService) DeleteSession(sessionID, projectPath string) error {
	projectKey := sessionProjectKey(projectPath)
	byID := s.byProject[projectKey]
	if byID == nil {
		return fmt.Errorf("not found")
	}
	delete(byID, sessionID)
	return nil
}

func (s *inMemorySessionService) NotifySessionChange(changeType, sessionID string, extra map[string]any) {
}

func (s *inMemorySessionService) InitSessionDir(projectPath, sessionID string) (map[string]string, error) {
	return map[string]string{"root": "/tmp/mock-session"}, nil
}

func (s *inMemorySessionService) GetSessionDir(projectPath, sessionID string) (string, error) {
	return "/tmp/mock-session", nil
}

func (s *inMemorySessionService) SaveSessionAtomically(sessionID, projectPath string, updater func(map[string]any) error) (map[string]any, error) {
	sessionData, err := s.LoadSession(sessionID, projectPath)
	if err != nil {
		return nil, err
	}
	if updater != nil {
		if err := updater(sessionData); err != nil {
			return nil, err
		}
	}
	if err := s.SaveSession(sessionData, projectPath); err != nil {
		return nil, err
	}
	return cloneSessionMap(sessionData), nil
}

func (s *inMemorySessionService) ListSkills() ([]map[string]any, error) {
	return []map[string]any{}, nil
}

func (s *inMemorySessionService) ListSkillPackages() ([]settings.SkillPackageSummary, error) {
	return []settings.SkillPackageSummary{}, nil
}

func (s *inMemorySessionService) GetSkillPackage(name string) (settings.SkillPackageDetail, error) {
	return settings.SkillPackageDetail{}, fmt.Errorf("not implemented")
}

func (s *inMemorySessionService) GetSkillPackageReferences(name string) []config.SkillReference {
	return nil
}

func (s *inMemorySessionService) SetSkillPackageEnabled(name string, enabled bool) error {
	return nil
}

func (s *inMemorySessionService) GetSkillContent(name string) (map[string]any, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *inMemorySessionService) SaveSkill(name, description, content string) error {
	return nil
}

func (s *inMemorySessionService) DeleteSkill(name string) error {
	return nil
}

func (s *inMemorySessionService) ImportSkill(sourcePath string, overwrite bool) (map[string]any, error) {
	return map[string]any{"imported": []map[string]any{}}, nil
}

func buildSessionTestRouter(t *testing.T) *mux.Router {
	t.Helper()

	router, _ := buildSessionTestContext(t)
	return router
}

func buildSessionTestContext(t *testing.T) (*mux.Router, *inMemorySessionService) {
	t.Helper()

	service := newInMemorySessionService()
	handler := NewAPI(nil, nil, service, service, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router, service
}

func TestSessionCreateAndPatchEndpoints(t *testing.T) {
	router := buildSessionTestRouter(t)
	projectPath := "/tmp/demo.qgz"

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"name":"测试会话"}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	createResp := decodeAPIResponse(t, createRR)
	if !createResp.Success {
		t.Fatalf("create session success=false: %+v", createResp)
	}

	created, ok := createResp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected create data type: %T", createResp.Data)
	}
	if got, _ := created["title_source"].(string); got != "manual" {
		t.Fatalf("expected manual title_source for named session, got=%q payload=%#v", got, created)
	}
	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("missing session_id in create response: %#v", created)
	}

	patchReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/sessions/"+sessionID+"?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"name":"已重命名","archived":true}`),
	)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRR := httptest.NewRecorder()
	router.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusOK {
		t.Fatalf("PATCH /api/sessions/{id} status=%d body=%s", patchRR.Code, patchRR.Body.String())
	}

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/api/sessions/"+sessionID+"?project_path=/tmp/demo.qgz",
		nil,
	)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/{id} status=%d body=%s", getRR.Code, getRR.Body.String())
	}

	getResp := decodeAPIResponse(t, getRR)
	sessionData, ok := getResp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected get data type: %T", getResp.Data)
	}
	if got, _ := sessionData["name"].(string); got != "已重命名" {
		t.Fatalf("unexpected patched name: %#v", sessionData["name"])
	}
	if got, _ := sessionData["archived"].(bool); !got {
		t.Fatalf("expected archived=true, got=%#v", sessionData["archived"])
	}
	if got, _ := sessionData["title_source"].(string); got != "manual" {
		t.Fatalf("expected renamed session title_source=manual, got=%q payload=%#v", got, sessionData)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions?project_path="+projectPath, nil)
	listRR := httptest.NewRecorder()
	router.ServeHTTP(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions status=%d body=%s", listRR.Code, listRR.Body.String())
	}

	listResp := decodeAPIResponse(t, listRR)
	listData := listResp.Data.(map[string]any)
	sessionsRaw, ok := listData["sessions"].([]any)
	if !ok || len(sessionsRaw) != 1 {
		t.Fatalf("unexpected sessions payload: %#v", listData["sessions"])
	}
}

func TestSessionOperationEndpoint(t *testing.T) {
	router := buildSessionTestRouter(t)
	projectPath := "/tmp/demo.qgz"

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"name":"测试会话"}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	createResp := decodeAPIResponse(t, createRR)
	created := createResp.Data.(map[string]any)
	sessionID := created["session_id"].(string)

	opReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions/"+sessionID+"/operations?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"operation":{"type":"clear_history"}}`),
	)
	opReq.Header.Set("Content-Type", "application/json")
	opRR := httptest.NewRecorder()
	router.ServeHTTP(opRR, opReq)
	if opRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/{id}/operations status=%d body=%s", opRR.Code, opRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"?project_path="+projectPath, nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/{id} status=%d body=%s", getRR.Code, getRR.Body.String())
	}
}

func TestSessionExportAndImportEndpoints(t *testing.T) {
	router := buildSessionTestRouter(t)
	projectPath := "/tmp/demo.qgz"

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"name":"导出测试会话"}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	createResp := decodeAPIResponse(t, createRR)
	created := createResp.Data.(map[string]any)
	sessionID := created["session_id"].(string)

	created["messages"] = []map[string]any{{"role": "user", "content": "hello"}}
	created["tags"] = []string{"import", "export"}
	created["execution_mode"] = "build"
	saveBody, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal save body failed: %v", err)
	}

	saveReq := httptest.NewRequest(
		http.MethodPut,
		"/api/sessions/"+sessionID+"?project_path=/tmp/demo.qgz",
		bytes.NewReader(saveBody),
	)
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	router.ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("PUT /api/sessions/{id} status=%d body=%s", saveRR.Code, saveRR.Body.String())
	}

	exportReq := httptest.NewRequest(
		http.MethodGet,
		"/api/sessions/"+sessionID+"/export?project_path=/tmp/demo.qgz",
		nil,
	)
	exportRR := httptest.NewRecorder()
	router.ServeHTTP(exportRR, exportReq)
	if exportRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/{id}/export status=%d body=%s", exportRR.Code, exportRR.Body.String())
	}

	exportResp := decodeAPIResponse(t, exportRR)
	exported := exportResp.Data.(map[string]any)
	if got, _ := exported["session_id"].(string); got != sessionID {
		t.Fatalf("unexpected exported session_id: %q", got)
	}
	if got, _ := exported["title_source"].(string); got != "manual" {
		t.Fatalf("expected exported title_source=manual, got=%q payload=%#v", got, exported)
	}

	importPayload, err := json.Marshal(map[string]any{"session": exported})
	if err != nil {
		t.Fatalf("marshal import payload failed: %v", err)
	}

	importReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions/import?project_path=/tmp/demo.qgz",
		bytes.NewReader(importPayload),
	)
	importReq.Header.Set("Content-Type", "application/json")
	importRR := httptest.NewRecorder()
	router.ServeHTTP(importRR, importReq)
	if importRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions/import status=%d body=%s", importRR.Code, importRR.Body.String())
	}

	importResp := decodeAPIResponse(t, importRR)
	imported := importResp.Data.(map[string]any)
	importedID, _ := imported["session_id"].(string)
	if importedID == "" || importedID == sessionID {
		t.Fatalf("unexpected imported session_id: %q", importedID)
	}
	if got, _ := imported["title_source"].(string); got != "manual" {
		t.Fatalf("expected imported title_source=manual, got=%q payload=%#v", got, imported)
	}

	importedMessages, ok := imported["messages"].([]any)
	if !ok || len(importedMessages) != 1 {
		t.Fatalf("unexpected imported messages: %#v", imported["messages"])
	}
	message0, ok := importedMessages[0].(map[string]any)
	if !ok || message0["content"] != "hello" {
		t.Fatalf("unexpected imported message payload: %#v", importedMessages[0])
	}

	importedGetReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+importedID+"?project_path="+projectPath, nil)
	importedGetRR := httptest.NewRecorder()
	router.ServeHTTP(importedGetRR, importedGetReq)
	if importedGetRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/{imported-id} status=%d body=%s", importedGetRR.Code, importedGetRR.Body.String())
	}
}

func TestSessionCreateDefaultsTitleSourceWhenNameMissing(t *testing.T) {
	router := buildSessionTestRouter(t)

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	createResp := decodeAPIResponse(t, createRR)
	created := createResp.Data.(map[string]any)
	if got, _ := created["title_source"].(string); got != "default" {
		t.Fatalf("expected default title_source for unnamed session, got=%q payload=%#v", got, created)
	}
	if got, _ := created["name_auto_generated"].(bool); !got {
		t.Fatalf("expected name_auto_generated=true for unnamed session, got=%#v", created["name_auto_generated"])
	}
}

func TestSessionGetNormalizesMissingTitleSource(t *testing.T) {
	router, service := buildSessionTestContext(t)
	projectPath := "/tmp/demo.qgz"

	if err := service.SaveSession(map[string]any{
		"session_id":          "session-legacy-title",
		"name":                "会话 03-12 15:00",
		"name_auto_generated": true,
		"messages":            []any{},
		"execution_mode":      "build",
		"run_intent":          "execute",
		"created_at":          time.Now().Format(time.RFC3339),
		"updated_at":          time.Now().Format(time.RFC3339),
	}, projectPath); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/session-legacy-title?project_path="+projectPath, nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions/{id} status=%d body=%s", getRR.Code, getRR.Body.String())
	}

	getResp := decodeAPIResponse(t, getRR)
	sessionData := getResp.Data.(map[string]any)
	if got, _ := sessionData["title_source"].(string); got != "default" {
		t.Fatalf("expected normalized title_source=default, got=%q payload=%#v", got, sessionData)
	}
}

func TestSessionImportRejectsDeprecatedAutoMode(t *testing.T) {
	router := buildSessionTestRouter(t)

	importReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions/import?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"session":{"name":"旧会话","messages":[{"role":"user","content":"hello"}],"execution_mode":"auto"}}`),
	)
	importReq.Header.Set("Content-Type", "application/json")
	importRR := httptest.NewRecorder()
	router.ServeHTTP(importRR, importReq)
	if importRR.Code != http.StatusBadRequest {
		t.Fatalf("expected import deprecated auto mode status=400, got=%d body=%s", importRR.Code, importRR.Body.String())
	}
}

func TestSessionPatchRejectsDeprecatedAutoMode(t *testing.T) {
	router := buildSessionTestRouter(t)

	createReq := httptest.NewRequest(
		http.MethodPost,
		"/api/sessions?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"name":"测试会话"}`),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/sessions status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	createResp := decodeAPIResponse(t, createRR)
	created := createResp.Data.(map[string]any)
	sessionID := created["session_id"].(string)

	patchReq := httptest.NewRequest(
		http.MethodPatch,
		"/api/sessions/"+sessionID+"?project_path=/tmp/demo.qgz",
		bytes.NewBufferString(`{"execution_mode":"auto"}`),
	)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRR := httptest.NewRecorder()
	router.ServeHTTP(patchRR, patchReq)
	if patchRR.Code != http.StatusBadRequest {
		t.Fatalf("expected patch deprecated auto mode status=400, got=%d body=%s", patchRR.Code, patchRR.Body.String())
	}
}

func TestSessionGetNormalizesStoredDeprecatedAutoMode(t *testing.T) {
	service := newInMemorySessionService()
	if err := service.SaveSession(map[string]any{
		"session_id":          "legacy-auto-session",
		"name":                "旧会话",
		"messages":            []any{},
		"execution_mode":      "auto",
		"run_intent":          "auto",
		"project_path":        "/tmp/demo.qgz",
		"created_at":          time.Now().Format(time.RFC3339),
		"updated_at":          time.Now().Format(time.RFC3339),
		"agent_config":        "default",
		"llm_config":          map[string]any{},
		"tool_calls":          []any{},
		"snapshots":           []any{},
		"context_stats":       map[string]any{"total_tokens": 0, "compressed_count": 0},
		"tags":                []any{},
		"archived":            false,
		"description":         "",
		"compression_history": []any{},
	}, "/tmp/demo.qgz"); err != nil {
		t.Fatalf("seed legacy session failed: %v", err)
	}

	handler := NewAPI(nil, nil, service, service, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)

	getReq := httptest.NewRequest(http.MethodGet, "/api/sessions/legacy-auto-session?project_path=/tmp/demo.qgz", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("expected stored deprecated auto mode status=200, got=%d body=%s", getRR.Code, getRR.Body.String())
	}

	resp := decodeAPIResponse(t, getRR)
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data payload: %#v", resp.Data)
	}
	if data["execution_mode"] != "build" {
		t.Fatalf("expected normalized execution_mode=build, got=%v", data["execution_mode"])
	}
	if data["run_intent"] != "execute" {
		t.Fatalf("expected normalized run_intent=execute, got=%v", data["run_intent"])
	}
}

func TestListProjectsEndpoint(t *testing.T) {
	router := buildSessionTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/projects status=%d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeAPIResponse(t, rr)
	if !resp.Success {
		t.Fatalf("projects response should be success: %+v", resp)
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected projects payload type: %T", resp.Data)
	}
	if _, ok := data["projects"].([]any); !ok {
		t.Fatalf("projects should be array: %#v", data["projects"])
	}
}

func TestSessionSavePreservesRuntimeOwnedFields(t *testing.T) {
	router, service := buildSessionTestContext(t)
	projectPath := "/tmp/demo.qgz"
	sessionID := "session-runtime-owned"
	createdAt := time.Now().Add(-time.Minute).Format(time.RFC3339)
	updatedAt := time.Now().Format(time.RFC3339)

	if err := service.SaveSession(map[string]any{
		"session_id":             sessionID,
		"name":                   "运行态会话",
		"messages":               []any{},
		"execution_mode":         "build",
		"run_intent":             "execute",
		"workflow_blocks":        []any{map[string]any{"id": "wf-live", "summary": "backend-owned", "status": "running", "entries": []any{}}},
		"runtime_status":         "running",
		"runtime_summary":        "backend-owned",
		"runtime_failure_reason": "",
		"created_at":             createdAt,
		"updated_at":             updatedAt,
	}, projectPath); err != nil {
		t.Fatalf("seed session failed: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		"/api/sessions/"+sessionID+"?project_path="+projectPath,
		bytes.NewBufferString(`{"session_id":"session-runtime-owned","name":"用户修改名称","messages":[],"execution_mode":"build","run_intent":"execute","workflow_blocks":[{"id":"wf-client","summary":"client-overwrite","status":"completed","entries":[]}],"runtime_status":"failed","runtime_summary":"client-overwrite"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT /api/sessions/{id} status=%d body=%s", rr.Code, rr.Body.String())
	}

	stored, err := service.LoadSession(sessionID, projectPath)
	if err != nil {
		t.Fatalf("load stored session failed: %v", err)
	}
	blocks, ok := stored["workflow_blocks"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("unexpected stored workflow_blocks: %#v", stored["workflow_blocks"])
	}
	block, _ := blocks[0].(map[string]any)
	if block["id"] != "wf-live" {
		t.Fatalf("expected backend-owned workflow block to be preserved, got %#v", block)
	}
	if stored["runtime_status"] != "running" {
		t.Fatalf("expected runtime_status to be preserved, got %#v", stored["runtime_status"])
	}
	if stored["runtime_summary"] != "backend-owned" {
		t.Fatalf("expected runtime_summary to be preserved, got %#v", stored["runtime_summary"])
	}
	if stored["name"] != "用户修改名称" {
		t.Fatalf("expected editable session fields to remain writable, got %#v", stored["name"])
	}
}
