package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"github.com/autocarto/carto_agent/internal/config"
)

func buildAgentTestRouter(t *testing.T) *mux.Router {
	t.Helper()

	acm := config.NewAgentConfigManager(t.TempDir())
	handler := NewAPI(nil, acm, nil, nil, "", nil)
	router := mux.NewRouter()
	handler.SetupRoutes(router)
	return router
}

func TestAgentConfigCreateCloneAndGetDataEndpoints(t *testing.T) {
	router := buildAgentTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/agent-configs", bytes.NewBufferString(`{"name":"custom"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("POST /api/agent-configs status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	dataReq := httptest.NewRequest(http.MethodGet, "/api/agent-configs/custom/data", nil)
	dataRR := httptest.NewRecorder()
	router.ServeHTTP(dataRR, dataReq)
	if dataRR.Code != http.StatusOK {
		t.Fatalf("GET /api/agent-configs/{name}/data status=%d body=%s", dataRR.Code, dataRR.Body.String())
	}

	dataResp := decodeAPIResponse(t, dataRR)
	if !dataResp.Success {
		t.Fatalf("GET config data success=false: %+v", dataResp)
	}
	dataMap, ok := dataResp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type: %T", dataResp.Data)
	}
	runtimeShared, ok := dataMap["runtime_shared"].(map[string]any)
	if !ok {
		t.Fatalf("missing runtime_shared in profile data: %#v", dataResp.Data)
	}
	if _, exists := runtimeShared["common_tools"]; exists {
		t.Fatalf("runtime_shared should not expose common_tools anymore: %#v", runtimeShared)
	}
	if skills, ok := runtimeShared["skill_packages"].([]any); !ok || len(skills) == 0 {
		t.Fatalf("expected runtime_shared.skill_packages in profile data: %#v", runtimeShared)
	}
	runtimeAgents, ok := dataMap["runtime_agents"].([]any)
	if !ok {
		t.Fatalf("missing runtime_agents in profile data: %#v", dataResp.Data)
	}
	if len(runtimeAgents) == 0 {
		t.Fatalf("expected default runtime_agents in profile data: %#v", dataResp.Data)
	}

	cloneReq := httptest.NewRequest(http.MethodPost, "/api/agent-configs/custom/clone", bytes.NewBufferString(`{"target_name":"custom-copy"}`))
	cloneReq.Header.Set("Content-Type", "application/json")
	cloneRR := httptest.NewRecorder()
	router.ServeHTTP(cloneRR, cloneReq)
	if cloneRR.Code != http.StatusOK {
		t.Fatalf("POST /api/agent-configs/{name}/clone status=%d body=%s", cloneRR.Code, cloneRR.Body.String())
	}

	cloneDataReq := httptest.NewRequest(http.MethodGet, "/api/agent-configs/custom-copy/data", nil)
	cloneDataRR := httptest.NewRecorder()
	router.ServeHTTP(cloneDataRR, cloneDataReq)
	if cloneDataRR.Code != http.StatusOK {
		t.Fatalf("GET cloned config data status=%d body=%s", cloneDataRR.Code, cloneDataRR.Body.String())
	}
}

func TestLegacyAgentMemberEndpointsRemoved(t *testing.T) {
	router := buildAgentTestRouter(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/agent-configs", bytes.NewBufferString(`{"name":"custom"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	router.ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusOK {
		t.Fatalf("create config failed: status=%d body=%s", createRR.Code, createRR.Body.String())
	}

	addBody := `{"agent_name":"vector_helper","agent":{"display_name":"矢量助手","description":"负责矢量处理","enabled":true}}`
	addReq := httptest.NewRequest(http.MethodPost, "/api/agent-configs/custom/agents", bytes.NewBufferString(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addRR := httptest.NewRecorder()
	router.ServeHTTP(addRR, addReq)
	if addRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed agent add endpoint status=404, got=%d body=%s", addRR.Code, addRR.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/agent-configs/custom/agents/vector_helper", nil)
	getRR := httptest.NewRecorder()
	router.ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed agent get endpoint status=404, got=%d body=%s", getRR.Code, getRR.Body.String())
	}

	updateBody := `{"display_name":"矢量助手","description":"更新后的描述","enabled":true}`
	updateReq := httptest.NewRequest(http.MethodPut, "/api/agent-configs/custom/agents/vector_helper", bytes.NewBufferString(updateBody))
	updateReq.Header.Set("Content-Type", "application/json")
	updateRR := httptest.NewRecorder()
	router.ServeHTTP(updateRR, updateReq)
	if updateRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed agent update endpoint status=404, got=%d body=%s", updateRR.Code, updateRR.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/agent-configs/custom/agents/vector_helper", nil)
	deleteRR := httptest.NewRecorder()
	router.ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed agent delete endpoint status=404, got=%d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func TestLegacyAgentMainDeleteEndpointRemoved(t *testing.T) {
	router := buildAgentTestRouter(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/agent-configs/default/agents/orchestrator", nil)
	deleteRR := httptest.NewRecorder()
	router.ServeHTTP(deleteRR, deleteReq)
	if deleteRR.Code != http.StatusNotFound {
		t.Fatalf("expected removed main delete endpoint status=404, got=%d body=%s", deleteRR.Code, deleteRR.Body.String())
	}
}

func TestAgentConfigSaveAcceptsRuntimeOnlyPayload(t *testing.T) {
	router := buildAgentTestRouter(t)

	saveBody := `{
			"meta": {
				"default_execution_mode": "build",
				"description": "runtime only",
				"plan_enabled": false
			},
			"has_plan": false,
			"runtime_shared": {
				"common_skills": [],
				"common_category_skills": ["project-state"],
				"common_workflow_skills": []
			},
			"runtime_agents": [
				{
					"name": "orchestrator",
					"agent": {
						"display_name": "统一运行智能体",
						"description": "runtime payload",
						"is_main": true,
						"enabled": true,
						"prompts": {
							"static_instructions": "test"
						}
					}
				}
			]
		}`
	saveReq := httptest.NewRequest(http.MethodPut, "/api/agent-configs/default", bytes.NewBufferString(saveBody))
	saveReq.Header.Set("Content-Type", "application/json")
	saveRR := httptest.NewRecorder()
	router.ServeHTTP(saveRR, saveReq)
	if saveRR.Code != http.StatusOK {
		t.Fatalf("save runtime-only payload failed: status=%d body=%s", saveRR.Code, saveRR.Body.String())
	}

	dataReq := httptest.NewRequest(http.MethodGet, "/api/agent-configs/default/data", nil)
	dataRR := httptest.NewRecorder()
	router.ServeHTTP(dataRR, dataReq)
	if dataRR.Code != http.StatusOK {
		t.Fatalf("GET /api/agent-configs/default/data status=%d body=%s", dataRR.Code, dataRR.Body.String())
	}

	dataResp := decodeAPIResponse(t, dataRR)
	dataMap, ok := dataResp.Data.(map[string]any)
	if !ok {
		t.Fatalf("unexpected data type after runtime-only save: %T", dataResp.Data)
	}
	runtimeAgents, ok := dataMap["runtime_agents"].([]any)
	if !ok {
		t.Fatalf("missing runtime_agents after runtime-only save: %#v", dataResp.Data)
	}
	found := false
	for _, item := range runtimeAgents {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if entry["name"] == "orchestrator" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected runtime_agents to include orchestrator after runtime-only save: %#v", runtimeAgents)
	}
}
